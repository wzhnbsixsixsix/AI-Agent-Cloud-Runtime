package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	internalacp "github.com/wzhnbsixsixsix/agentforge/internal/acp"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

func newRTTCmd() *cobra.Command {
	var (
		grpcAddr, acpAddr string
		n                 int
		warmup            int
	)
	cmd := &cobra.Command{
		Use:   "rtt",
		Short: "单连接 N 次 ping 的 P50/P95/P99 延迟",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			// ACP
			acpCli, err := dialACP(ctx, acpAddr)
			if err != nil {
				return fmt.Errorf("acp dial: %w", err)
			}
			defer acpCli.Close()
			acpStats := &stats{}
			for i := 0; i < warmup; i++ {
				_, _ = acpCli.Ping(ctx)
			}
			for i := 0; i < n; i++ {
				rtt, err := acpCli.Ping(ctx)
				if err != nil {
					return fmt.Errorf("acp ping[%d]: %w", i, err)
				}
				acpStats.Add(rtt)
			}

			// gRPC
			conn, err := dialGRPC(ctx, grpcAddr)
			if err != nil {
				return fmt.Errorf("grpc dial: %w", err)
			}
			defer conn.Close()
			grpcStats := &stats{}
			for i := 0; i < warmup; i++ {
				_ = healthCheck(ctx, conn)
			}
			for i := 0; i < n; i++ {
				start := time.Now()
				if err := healthCheck(ctx, conn); err != nil {
					return fmt.Errorf("grpc check[%d]: %w", i, err)
				}
				grpcStats.Add(time.Since(start))
			}

			printCompare("RTT", n, 1, acpStats, grpcStats)
			return nil
		},
	}
	cmd.Flags().StringVar(&grpcAddr, "grpc", "localhost:8080", "gRPC gateway 地址")
	cmd.Flags().StringVar(&acpAddr, "acp", "localhost:8090", "ACP gateway 地址")
	cmd.Flags().IntVarP(&n, "num", "n", 5000, "ping 次数")
	cmd.Flags().IntVar(&warmup, "warmup", 100, "预热轮数（不计入统计）")
	return cmd
}

func newThroughputCmd() *cobra.Command {
	var (
		grpcAddr, acpAddr string
		n, c              int
	)
	cmd := &cobra.Command{
		Use:   "throughput",
		Short: "c 个连接并发 ping，测 ops/sec",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()
			if c <= 0 {
				c = 1
			}

			// ACP: 预先建立 c 条连接
			acpClis := make([]*internalacp.Client, c)
			for i := 0; i < c; i++ {
				cli, err := dialACP(ctx, acpAddr)
				if err != nil {
					return fmt.Errorf("acp dial[%d]: %w", i, err)
				}
				acpClis[i] = cli
			}
			defer func() {
				for _, x := range acpClis {
					if x != nil {
						_ = x.Close()
					}
				}
			}()
			acpDur, err := runEqualWorkers(c, n, func(workerID int) error {
				_, err := acpClis[workerID].Ping(ctx)
				return err
			})
			if err != nil {
				return fmt.Errorf("acp run: %w", err)
			}

			// gRPC: 同样预先 c 条连接
			grpcConns := make([]*grpc.ClientConn, c)
			for i := 0; i < c; i++ {
				conn, err := dialGRPC(ctx, grpcAddr)
				if err != nil {
					return fmt.Errorf("grpc dial[%d]: %w", i, err)
				}
				grpcConns[i] = conn
			}
			defer func() {
				for _, x := range grpcConns {
					if x != nil {
						_ = x.Close()
					}
				}
			}()
			grpcDur, err := runEqualWorkers(c, n, func(workerID int) error {
				return healthCheck(ctx, grpcConns[workerID])
			})
			if err != nil {
				return fmt.Errorf("grpc run: %w", err)
			}

			printThroughput("Throughput", n, c, acpDur, grpcDur)
			return nil
		},
	}
	cmd.Flags().StringVar(&grpcAddr, "grpc", "localhost:8080", "gRPC gateway 地址")
	cmd.Flags().StringVar(&acpAddr, "acp", "localhost:8090", "ACP gateway 地址")
	cmd.Flags().IntVarP(&n, "num", "n", 50000, "总 ping 数")
	cmd.Flags().IntVarP(&c, "concurrency", "c", 64, "并发连接数")
	return cmd
}

func newConnectCmd() *cobra.Command {
	var (
		grpcAddr, acpAddr string
		n, c              int
	)
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "并发建连耗时（ACP HELLO 握手 vs gRPC HTTP/2 握手）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			if c <= 0 {
				c = 1
			}

			acpStats := &stats{}
			grpcStats := &stats{}

			acpDur, err := runEqualWorkers(c, n, func(_ int) error {
				start := time.Now()
				cli, err := dialACP(ctx, acpAddr)
				if err != nil {
					return err
				}
				acpStats.Add(time.Since(start))
				return cli.Close()
			})
			if err != nil {
				return fmt.Errorf("acp connect: %w", err)
			}

			grpcDur, err := runEqualWorkers(c, n, func(_ int) error {
				start := time.Now()
				conn, err := dialGRPC(ctx, grpcAddr)
				if err != nil {
					return err
				}
				if err := healthCheck(ctx, conn); err != nil {
					_ = conn.Close()
					return err
				}
				grpcStats.Add(time.Since(start))
				return conn.Close()
			})
			if err != nil {
				return fmt.Errorf("grpc connect: %w", err)
			}

			printCompare("Connect Setup", n, c, acpStats, grpcStats)
			fmt.Printf("(total wallclock: ACP=%s gRPC=%s)\n", acpDur, grpcDur)
			return nil
		},
	}
	cmd.Flags().StringVar(&grpcAddr, "grpc", "localhost:8080", "gRPC gateway 地址")
	cmd.Flags().StringVar(&acpAddr, "acp", "localhost:8090", "ACP gateway 地址")
	cmd.Flags().IntVarP(&n, "num", "n", 1000, "总建连次数")
	cmd.Flags().IntVarP(&c, "concurrency", "c", 50, "并发度")
	return cmd
}

// runEqualWorkers 起 c 个 worker，每个执行 n/c 次 op；返回总 wallclock 与首个错误。
func runEqualWorkers(c, n int, op func(workerID int) error) (time.Duration, error) {
	per := n / c
	if per == 0 {
		per = 1
	}
	start := time.Now()
	var wg sync.WaitGroup
	var firstErr atomic.Value
	for i := 0; i < c; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for k := 0; k < per; k++ {
				if err := op(id); err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if v := firstErr.Load(); v != nil {
		return 0, v.(error)
	}
	return time.Since(start), nil
}
