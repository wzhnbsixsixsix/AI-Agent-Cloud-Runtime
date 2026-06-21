package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"github.com/spf13/cobra"
)

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "运行 W7 multi-agent pipeline",
	}
	cmd.AddCommand(newPipelineRunCmd())
	return cmd
}

func newPipelineRunCmd() *cobra.Command {
	var (
		file    string
		tenant  string
		userID  string
		model   string
		dialTo  string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "运行 pipeline YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return errors.New("--file is required")
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("read pipeline file: %w", err)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			conn, _, err := dialGateway(ctx, dialTo)
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := pb.NewAgentServiceClient(conn).RunPipeline(ctx, &pb.PipelineRequest{
				TenantId: tenant,
				UserId:   userID,
				Model:    model,
				SpecYaml: string(raw),
			})
			if err != nil {
				return fmt.Errorf("run pipeline: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[pipeline] name=%s status=%s\n", resp.GetName(), resp.GetStatus())
			if resp.GetError() != "" {
				fmt.Fprintf(os.Stderr, "[pipeline] error=%s\n", resp.GetError())
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "STEP\tROLE\tSTATUS\tRUN_ID\tSUMMARY")
			for _, r := range resp.GetResults() {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.GetStepId(), r.GetRole(), r.GetStatus(), r.GetChildRunId(), oneLine(r.GetSummary(), 120))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			if resp.GetStatus() == "error" {
				if resp.GetError() == "" {
					return errors.New("pipeline failed")
				}
				return errors.New(resp.GetError())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "pipeline YAML 文件")
	cmd.Flags().StringVar(&tenant, "tenant", "default", "tenant id")
	cmd.Flags().StringVar(&userID, "user", "pipeline", "用户标识")
	cmd.Flags().StringVar(&model, "model", "", "覆盖默认模型")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway gRPC 地址（默认 GATEWAY_DIAL_ADDR）")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "整体超时")
	return cmd
}

func oneLine(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	rs := []rune(out)
	if max > 0 && len(rs) > max {
		if max <= 3 {
			return string(rs[:max])
		}
		return string(rs[:max-3]) + "..."
	}
	return out
}
