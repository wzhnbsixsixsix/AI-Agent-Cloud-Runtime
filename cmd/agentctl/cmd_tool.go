package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// newToolCmd 提供 W3 阶段直接调用 sandbox tool 的 CLI 入口。
//
//	agentctl tool list
//	agentctl tool exec bash --args '{"command":"echo hi"}'
//
// 一律走 gRPC（gateway 的 ExecTool / ListTools）。ACP 不暴露 tool 接口。
func newToolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool",
		Short: "调用 worker 侧 sandbox 内置 tool（W3 调试入口）",
	}
	cmd.AddCommand(newToolListCmd(), newToolExecCmd())
	return cmd
}

// dialGateway 复用 cmd_run.go 同款 gRPC 拨号逻辑。
func dialGateway(ctx context.Context, addr string) (*grpc.ClientConn, string, error) {
	cfg, _ := config.LoadAgentCtl()
	if addr == "" {
		if cfg != nil && cfg.GatewayDial != "" {
			addr = cfg.GatewayDial
		} else {
			addr = "localhost:8080"
		}
	}
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, addr, fmt.Errorf("dial gateway %s: %w", addr, err)
	}
	return conn, addr, nil
}

func newToolListCmd() *cobra.Command {
	var (
		dialTo  string
		timeout time.Duration
		showSch bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出 worker 已注册的全部 tool（含 JSON Schema）",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			conn, _, err := dialGateway(ctx, dialTo)
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := pb.NewAgentServiceClient(conn).ListTools(ctx, &pb.ListToolsRequest{})
			if err != nil {
				return fmt.Errorf("list tools: %w", err)
			}
			tools := append([]*pb.ToolDescriptor(nil), resp.GetTools()...)
			sort.Slice(tools, func(i, j int) bool { return tools[i].GetName() < tools[j].GetName() })

			if showSch {
				for _, t := range tools {
					fmt.Printf("# %s\n%s\n\nparameters:\n%s\n\n",
						t.GetName(), t.GetDescription(), prettyJSON(t.GetParametersJson()))
				}
				return nil
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tDESCRIPTION")
			for _, t := range tools {
				desc := strings.ReplaceAll(t.GetDescription(), "\n", " ")
				if len(desc) > 80 {
					desc = desc[:77] + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\n", t.GetName(), desc)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway gRPC 地址（默认 GATEWAY_DIAL_ADDR）")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "超时")
	cmd.Flags().BoolVar(&showSch, "schema", false, "同时打印每个 tool 的 JSON Schema")
	return cmd
}

func newToolExecCmd() *cobra.Command {
	var (
		argsJSON  string
		argsFile  string
		userID    string
		dialTo    string
		toolWait  time.Duration
		dialWait  time.Duration
		rawOutput bool
	)
	cmd := &cobra.Command{
		Use:   "exec <tool>",
		Short: "执行一次 tool 调用（同步等待结果）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := args[0]

			argsBytes, err := loadArgsJSON(argsJSON, argsFile)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// 给 dial 单独的短超时；execTool 自身用 toolWait。
			dialCtx, dialCancel := context.WithTimeout(ctx, dialWait)
			defer dialCancel()
			conn, _, err := dialGateway(dialCtx, dialTo)
			if err != nil {
				return err
			}
			defer conn.Close()

			callCtx, callCancel := context.WithTimeout(ctx, toolWait)
			defer callCancel()

			start := time.Now()
			resp, err := pb.NewAgentServiceClient(conn).ExecTool(callCtx, &pb.ExecToolRequest{
				UserId:    userID,
				Tool:      toolName,
				ArgsJson:  argsBytes,
				TimeoutMs: uint32(toolWait / time.Millisecond),
			})
			if err != nil {
				return fmt.Errorf("exec tool: %w", err)
			}

			if rawOutput {
				// 仅打印 content；返回值用于 shell pipeline。失败仍非零退出。
				fmt.Print(resp.GetContent())
				if resp.GetIsError() || resp.GetErrorCode() != "" {
					os.Exit(1)
				}
				return nil
			}

			fmt.Fprintf(os.Stderr, "[tool] name=%s call_id=%s container=%s elapsed=%s\n",
				toolName, resp.GetCallId(), resp.GetContainerId(), time.Since(start).Round(time.Millisecond))
			fmt.Fprintf(os.Stderr, "[tool] exit_code=%d is_error=%v\n",
				resp.GetExitCode(), resp.GetIsError())
			if ec := resp.GetErrorCode(); ec != "" {
				fmt.Fprintf(os.Stderr, "[tool] error: %s %s\n", ec, resp.GetErrorMessage())
			}
			if meta := resp.GetMetadataJson(); len(meta) > 0 && string(meta) != "{}" {
				fmt.Fprintf(os.Stderr, "[tool] metadata: %s\n", prettyJSON(string(meta)))
			}
			fmt.Fprintln(os.Stderr, "----- content -----")
			fmt.Println(resp.GetContent())

			if resp.GetIsError() || resp.GetErrorCode() != "" {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&argsJSON, "args", "", "tool 参数 JSON（与 --args-file 二选一）")
	cmd.Flags().StringVar(&argsFile, "args-file", "", "从文件读取 tool 参数 JSON；'-' 表示 stdin")
	cmd.Flags().StringVar(&userID, "user", "agentctl", "调用者标识（落到日志/审计）")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway gRPC 地址（默认 GATEWAY_DIAL_ADDR）")
	cmd.Flags().DurationVar(&toolWait, "timeout", 60*time.Second, "tool 调用整体超时")
	cmd.Flags().DurationVar(&dialWait, "dial-timeout", 5*time.Second, "建连超时")
	cmd.Flags().BoolVar(&rawOutput, "raw", false, "只把 content 写到 stdout，便于管道处理")
	return cmd
}

// loadArgsJSON 解析 --args / --args-file，返回压缩后的 JSON 字节。
// 校验是否能解析为 JSON 对象，避免把明显错的字符串发到 worker 才报错。
func loadArgsJSON(inline, file string) ([]byte, error) {
	switch {
	case inline != "" && file != "":
		return nil, errors.New("--args and --args-file are mutually exclusive")
	case inline == "" && file == "":
		// 允许无参（部分 tool 可能没有必填参数），但传一个 {} 占位更稳。
		return []byte("{}"), nil
	}

	var raw []byte
	if file != "" {
		if file == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("read stdin: %w", err)
			}
			raw = b
		} else {
			b, err := os.ReadFile(file)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", file, err)
			}
			raw = b
		}
	} else {
		raw = []byte(inline)
	}

	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("args is not a valid JSON object: %w", err)
	}
	// 重新 marshal 以去掉换行/注释干扰。
	out, err := json.Marshal(probe)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// prettyJSON 尝试把 src pretty-print，失败则原样返回。
func prettyJSON(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(src), &v); err != nil {
		return src
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return src
	}
	return string(b)
}
