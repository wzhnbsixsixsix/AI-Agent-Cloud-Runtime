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
)

// newResumeCmd 演示 ACP 断线续传：用 run_id + last_seq 重新连接 gateway，
// 把缓存里 seq>last_seq 的事件流式回放到 stdout。
//
// 仅支持 ACP（gRPC 没有原生协议级续传）。
func newResumeCmd() *cobra.Command {
	var (
		runID   string
		lastSeq uint64
		dialTo  string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "ACP 断线续传：从缓存中拉取 seq>last_seq 的事件",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runID == "" {
				return errors.New("--run-id is required")
			}
			cfg, _ := config.LoadAgentCtl()
			if dialTo == "" {
				if cfg != nil && cfg.GatewayACPDial != "" {
					dialTo = cfg.GatewayACPDial
				} else {
					dialTo = "localhost:8090"
				}
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if timeout > 0 {
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, timeout)
				defer c()
			}

			cli, err := internalacp.Dial(ctx, internalacp.DialOptions{Addr: dialTo, UserID: "resume"})
			if err != nil {
				return fmt.Errorf("dial acp %s: %w", dialTo, err)
			}
			defer cli.Close()

			if err := cli.SendResume(runID, lastSeq); err != nil {
				return fmt.Errorf("send resume: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[resume] run_id=%s last_seq=%d\n", runID, lastSeq)

			var got int
			for {
				ev, end, err := cli.NextEvent()
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					return fmt.Errorf("recv: %w", err)
				}
				if ev != nil {
					got++
					switch p := ev.Payload.(type) {
					case *pb.RunEvent_Token:
						fmt.Print(p.Token.GetText())
					case *pb.RunEvent_Done:
						fmt.Printf("\n[DONE] tokens=%d (replayed=%d)\n", p.Done.GetTotalTokens(), got)
					case *pb.RunEvent_Error:
						fmt.Fprintf(os.Stderr, "\n[ERROR] %s: %s\n", p.Error.GetCode(), p.Error.GetMessage())
					}
				}
				if end {
					break
				}
			}
			fmt.Fprintf(os.Stderr, "[resume done] events_replayed=%d\n", got)
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "需要续传的 run_id（必填，从原 run 的输出里抄）")
	cmd.Flags().Uint64Var(&lastSeq, "last-seq", 0, "已收到的最大 seq；填 0 表示从头回放")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway ACP 地址（默认 GATEWAY_ACP_DIAL_ADDR）")
	cmd.Flags().DurationVar(&timeout, "timeout", time.Minute, "整体超时")
	return cmd
}
