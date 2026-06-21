package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "hook", Short: "W8 Hook 服务调试"}
	cmd.AddCommand(newHookListCmd(), newHookRunCmd())
	return cmd
}

func newHookListCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出 hookd 已加载 hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			conn, err := dialHook(ctx, addr)
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := pb.NewHookServiceClient(conn).ListHooks(ctx, &pb.ListHooksRequest{})
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tEVENT\tENABLED\tSOURCE\tDESCRIPTION")
			for _, h := range resp.GetHooks() {
				fmt.Fprintf(tw, "%s\t%s\t%v\t%s\t%s\n", h.GetId(), h.GetEvent().String(), h.GetEnabled(), h.GetSource(), h.GetDescription())
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "hookd 地址（默认 HOOK_SERVICE_ADDR）")
	return cmd
}

func newHookRunCmd() *cobra.Command {
	var event, file, addr string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "执行一次 hook",
		RunE: func(cmd *cobra.Command, args []string) error {
			if event == "" {
				return errors.New("--event is required")
			}
			raw := []byte("{}")
			if file != "" {
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				raw = b
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			conn, err := dialHook(ctx, addr)
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := pb.NewHookServiceClient(conn).ExecuteHook(ctx, &pb.ExecuteHookRequest{Event: parseHookEvent(event), PayloadJson: raw})
			if err != nil {
				return err
			}
			fmt.Printf("allowed=%v reason=%q matched=%v\n%s\n", resp.GetAllowed(), resp.GetReason(), resp.GetMatchedHooks(), string(resp.GetPayloadJson()))
			return nil
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "PreLLM | PostLLM | PreToolUse | PostToolUse")
	cmd.Flags().StringVar(&file, "file", "", "payload JSON 文件")
	cmd.Flags().StringVar(&addr, "addr", "", "hookd 地址（默认 HOOK_SERVICE_ADDR）")
	return cmd
}

func dialHook(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	cfg, _ := config.LoadAgentCtl()
	if addr == "" && cfg != nil {
		addr = cfg.HookServiceAddr
	}
	if addr == "" {
		addr = "localhost:8083"
	}
	return grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
}

func parseHookEvent(s string) pb.HookEvent {
	switch s {
	case "PreLLM":
		return pb.HookEvent_HOOK_EVENT_PRE_LLM
	case "PostLLM":
		return pb.HookEvent_HOOK_EVENT_POST_LLM
	case "PreToolUse":
		return pb.HookEvent_HOOK_EVENT_PRE_TOOL_USE
	case "PostToolUse":
		return pb.HookEvent_HOOK_EVENT_POST_TOOL_USE
	default:
		return pb.HookEvent_HOOK_EVENT_UNSPECIFIED
	}
}
