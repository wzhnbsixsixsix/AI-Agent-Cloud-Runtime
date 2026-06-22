package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

func newRunAgentCmd() *cobra.Command {
	var (
		grpcAddr    string
		total       int
		concurrency int
		prompt      string
		model       string
		timeout     time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run-agent",
		Short: "并发压测 RunAgent 双向流，适合 W9 mock LLM 基准",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if total <= 0 {
				total = 1
			}
			if concurrency <= 0 {
				concurrency = 1
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			conn, err := dialGRPC(ctx, grpcAddr)
			if err != nil {
				return fmt.Errorf("grpc dial: %w", err)
			}
			defer conn.Close()
			stats := &stats{}
			var ok atomic.Int64
			var failed atomic.Int64
			start := time.Now()
			if err := runRunAgentWorkers(ctx, conn, total, concurrency, prompt, model, stats, &ok, &failed); err != nil {
				return err
			}
			p50, p95, p99, avg, n := stats.Summary()
			elapsed := time.Since(start)
			fmt.Printf("\nScenario: RunAgent (n=%d, c=%d)\n", total, concurrency)
			fmt.Printf("ok=%d failed=%d duration=%s throughput=%.2f runs/sec\n", ok.Load(), failed.Load(), elapsed, float64(total)/elapsed.Seconds())
			fmt.Println("              p50         p95         p99         avg")
			fmt.Printf("%-13s %-11s %-11s %-11s %-11s samples=%d\n", "RunAgent", p50, p95, p99, avg, n)
			if failed.Load() > 0 {
				return fmt.Errorf("run-agent benchmark had %d failures", failed.Load())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&grpcAddr, "grpc", "localhost:8080", "gRPC gateway 地址")
	cmd.Flags().IntVarP(&total, "num", "n", 100, "总 RunAgent 请求数")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "c", 8, "并发请求数")
	cmd.Flags().StringVar(&prompt, "prompt", "用一句话介绍 AgentForge", "每次 RunAgent 的 prompt")
	cmd.Flags().StringVar(&model, "model", "", "覆盖模型；mock LLM 下可为空")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "压测总超时")
	return cmd
}

func runRunAgentWorkers(ctx context.Context, conn *grpc.ClientConn, total, concurrency int, prompt, model string, stats *stats, ok, failed *atomic.Int64) error {
	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			cli := pb.NewAgentServiceClient(conn)
			for id := range jobs {
				start := time.Now()
				err := runAgentOnce(ctx, cli, fmt.Sprintf("bench-%d-%d", workerID, id), prompt, model)
				stats.Add(time.Since(start))
				if err != nil {
					failed.Add(1)
					firstErrOnce.Do(func() { firstErr = err })
					continue
				}
				ok.Add(1)
			}
		}(i)
	}
	for i := 0; i < total; i++ {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func runAgentOnce(ctx context.Context, cli pb.AgentServiceClient, userID, prompt, model string) error {
	stream, err := cli.RunAgent(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.RunRequest{UserId: userID, Prompt: prompt, Model: model}); err != nil {
		return err
	}
	if err := stream.CloseSend(); err != nil {
		return err
	}
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch ev.GetPayload().(type) {
		case *pb.RunEvent_Done:
			return nil
		case *pb.RunEvent_Error:
			return fmt.Errorf("run error: %s", ev.GetError().GetMessage())
		}
	}
}
