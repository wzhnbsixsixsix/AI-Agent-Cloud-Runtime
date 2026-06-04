package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	internalacp "github.com/wzhnbsixsixsix/agentforge/internal/acp"
	"github.com/wzhnbsixsixsix/agentforge/internal/config"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newRunCmd() *cobra.Command {
	var (
		prompt  string
		userID  string
		model   string
		dialTo  string
		proto   string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "运行一次 agent，流式打印 token（支持 gRPC 与 ACP）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" {
				return errors.New("--prompt is required")
			}
			cfg, _ := config.LoadAgentCtl()
			if proto == "" {
				if cfg != nil {
					proto = cfg.Proto
				} else {
					proto = "grpc"
				}
			}
			if dialTo == "" && cfg != nil {
				switch proto {
				case "acp":
					dialTo = cfg.GatewayACPDial
				default:
					dialTo = cfg.GatewayDial
				}
			}
			if dialTo == "" {
				if proto == "acp" {
					dialTo = "localhost:8090"
				} else {
					dialTo = "localhost:8080"
				}
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if timeout > 0 {
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, timeout)
				defer c()
			}

			switch proto {
			case "grpc":
				return runViaGRPC(ctx, dialTo, userID, model, prompt)
			case "acp":
				return runViaACP(ctx, dialTo, userID, model, prompt)
			default:
				return fmt.Errorf("unknown --proto %q (want grpc|acp)", proto)
			}
		},
	}
	cmd.Flags().StringVar(&prompt, "prompt", "", "用户 prompt（必填）")
	cmd.Flags().StringVar(&userID, "user", "anonymous", "用户标识")
	cmd.Flags().StringVar(&model, "model", "", "覆盖默认模型")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway 地址；默认按 --proto 选 GATEWAY_DIAL_ADDR / GATEWAY_ACP_DIAL_ADDR")
	cmd.Flags().StringVar(&proto, "proto", "", "传输协议：grpc | acp（默认读 AGENTCTL_PROTO，未设则 grpc）")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "整体超时")
	return cmd
}

func runViaGRPC(ctx context.Context, addr, userID, model, prompt string) error {
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return fmt.Errorf("dial gateway %s: %w", addr, err)
	}
	defer conn.Close()

	cli := pb.NewAgentServiceClient(conn)
	stream, err := cli.RunAgent(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	if err := stream.Send(&pb.RunRequest{UserId: userID, Prompt: prompt, Model: model}); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("close send: %w", err)
	}

	var runID, traceID string
	for {
		ev, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("recv: %w", err)
		}
		if runID == "" {
			runID = ev.GetRunId()
			traceID = ev.GetTraceId()
		}
		if done, err := printEvent(ev, runID, traceID); err != nil || done {
			return err
		}
	}
	return nil
}

func runViaACP(ctx context.Context, addr, userID, model, prompt string) error {
	cli, err := internalacp.Dial(ctx, internalacp.DialOptions{Addr: addr, UserID: userID})
	if err != nil {
		return fmt.Errorf("dial acp %s: %w", addr, err)
	}
	defer cli.Close()

	if err := cli.SendRun(&pb.RunRequest{UserId: userID, Prompt: prompt, Model: model}); err != nil {
		return fmt.Errorf("send run: %w", err)
	}
	for {
		ev, end, err := cli.NextEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("recv: %w", err)
		}
		if ev != nil {
			if done, err := printEvent(ev, cli.RunID(), cli.TraceID()); err != nil || done {
				return err
			}
		}
		if end {
			break
		}
	}
	return nil
}

// printEvent 打印一条 RunEvent，返回 (终态, err)。
func printEvent(ev *pb.RunEvent, runID, traceID string) (bool, error) {
	switch p := ev.Payload.(type) {
	case *pb.RunEvent_Token:
		fmt.Print(p.Token.GetText())
	case *pb.RunEvent_StateChanged:
		// 静默
	case *pb.RunEvent_Done:
		fmt.Printf("\n[DONE] run_id=%s trace_id=%s tokens=%d\n", runID, traceID, p.Done.GetTotalTokens())
		return true, nil
	case *pb.RunEvent_Error:
		fmt.Fprintf(os.Stderr, "\n[ERROR] %s: %s (run=%s)\n", p.Error.GetCode(), p.Error.GetMessage(), runID)
		os.Exit(1)
	}
	return false, nil
}
