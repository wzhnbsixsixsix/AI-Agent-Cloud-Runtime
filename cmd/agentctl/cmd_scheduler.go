package main

import (
	"context"
	"fmt"
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

func newSchedulerCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "scheduler", Short: "W8 scheduler 调试"}
	cmd.AddCommand(newSchedulerLeaderCmd(), newSchedulerPickCmd())
	return cmd
}

func newSchedulerLeaderCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "leader",
		Short: "查询 scheduler leader",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			conn, err := dialScheduler(ctx, addr)
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := pb.NewSchedulerServiceClient(conn).Leader(ctx, &pb.LeaderRequest{})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "leader_id=%s leader_addr=%s is_leader=%v\n", resp.GetLeaderId(), resp.GetLeaderAddr(), resp.GetIsLeader())
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "scheduler 地址（默认 SCHEDULER_DIAL_ADDR）")
	return cmd
}

func newSchedulerPickCmd() *cobra.Command {
	var addr, runID string
	cmd := &cobra.Command{
		Use:   "pick",
		Short: "选择一个当前最低负载 worker",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runID == "" {
				runID = "demo"
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			conn, err := dialScheduler(ctx, addr)
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := pb.NewSchedulerServiceClient(conn).Pick(ctx, &pb.PickRequest{RunId: runID})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "worker_id=%s addr=%s reason=%s leader=%s\n", resp.GetWorkerId(), resp.GetAddr(), resp.GetReason(), resp.GetLeaderId())
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "scheduler 地址（默认 SCHEDULER_DIAL_ADDR）")
	cmd.Flags().StringVar(&runID, "run-id", "demo", "run id")
	return cmd
}

func dialScheduler(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	cfg, _ := config.LoadAgentCtl()
	if addr == "" && cfg != nil {
		addr = cfg.SchedulerDial
	}
	if addr == "" {
		addr = "localhost:8081"
	}
	return grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
}
