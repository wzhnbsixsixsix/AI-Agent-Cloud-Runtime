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
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "运行一次 agent，流式打印 token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" {
				return errors.New("--prompt is required")
			}
			cfg, _ := config.LoadAgentCtl()
			if dialTo == "" {
				if cfg != nil {
					dialTo = cfg.GatewayDial
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

			conn, err := grpc.DialContext(ctx, dialTo,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithBlock(),
			)
			if err != nil {
				return fmt.Errorf("dial gateway %s: %w", dialTo, err)
			}
			defer conn.Close()

			cli := pb.NewAgentServiceClient(conn)
			stream, err := cli.RunAgent(ctx)
			if err != nil {
				return fmt.Errorf("open stream: %w", err)
			}
			if err := stream.Send(&pb.RunRequest{
				UserId: userID,
				Prompt: prompt,
				Model:  model,
			}); err != nil {
				return fmt.Errorf("send: %w", err)
			}
			if err := stream.CloseSend(); err != nil {
				return fmt.Errorf("close send: %w", err)
			}

			var (
				runID   string
				traceID string
			)
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
				switch p := ev.Payload.(type) {
				case *pb.RunEvent_Token:
					fmt.Print(p.Token.GetText())
				case *pb.RunEvent_StateChanged:
					// 静默；如要可视化加 -v
				case *pb.RunEvent_Done:
					fmt.Printf("\n[DONE] run_id=%s trace_id=%s tokens=%d\n", runID, traceID, p.Done.GetTotalTokens())
					return nil
				case *pb.RunEvent_Error:
					fmt.Fprintf(os.Stderr, "\n[ERROR] %s: %s (run=%s)\n", p.Error.GetCode(), p.Error.GetMessage(), runID)
					os.Exit(1)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&prompt, "prompt", "", "用户 prompt（必填）")
	cmd.Flags().StringVar(&userID, "user", "anonymous", "用户标识")
	cmd.Flags().StringVar(&model, "model", "", "覆盖默认模型")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway 地址（默认读 GATEWAY_DIAL_ADDR）")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "整体超时")
	return cmd
}
