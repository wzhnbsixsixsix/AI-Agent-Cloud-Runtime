package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	internalacp "github.com/wzhnbsixsixsix/agentforge/internal/acp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// stats 持有一组耗时样本，输出 P50/P95/P99/avg。
type stats struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (s *stats) Add(d time.Duration) {
	s.mu.Lock()
	s.samples = append(s.samples, d)
	s.mu.Unlock()
}

func (s *stats) Summary() (p50, p95, p99, avg time.Duration, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n = len(s.samples)
	if n == 0 {
		return
	}
	sort.Slice(s.samples, func(i, j int) bool { return s.samples[i] < s.samples[j] })
	p50 = s.samples[n*50/100]
	p95 = s.samples[min(n*95/100, n-1)]
	p99 = s.samples[min(n*99/100, n-1)]
	var sum time.Duration
	for _, v := range s.samples {
		sum += v
	}
	avg = sum / time.Duration(n)
	return
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// printCompare 打印 ACP vs gRPC 对比表格 + speedup。
func printCompare(scenario string, n, c int, acpStats, grpcStats *stats) {
	ap50, ap95, ap99, aavg, _ := acpStats.Summary()
	gp50, gp95, gp99, gavg, _ := grpcStats.Summary()
	fmt.Printf("\nScenario: %s (n=%d, c=%d)\n", scenario, n, c)
	fmt.Println("              p50         p95         p99         avg")
	fmt.Printf("%-13s %-11s %-11s %-11s %-11s\n", "ACP", ap50, ap95, ap99, aavg)
	fmt.Printf("%-13s %-11s %-11s %-11s %-11s\n", "gRPC", gp50, gp95, gp99, gavg)
	fmt.Printf("%-13s %-11s %-11s %-11s %-11s\n", "speedup",
		ratio(gp50, ap50), ratio(gp95, ap95), ratio(gp99, ap99), ratio(gavg, aavg))
}

func ratio(g, a time.Duration) string {
	if a <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2fx", float64(g)/float64(a))
}

// printThroughput 输出 ops/sec 对比。
func printThroughput(scenario string, n, c int, acpDur, grpcDur time.Duration) {
	acpQPS := float64(n) / acpDur.Seconds()
	grpcQPS := float64(n) / grpcDur.Seconds()
	fmt.Printf("\nScenario: %s (n=%d, c=%d)\n", scenario, n, c)
	fmt.Printf("%-13s %-15s %-15s\n", "", "duration", "ops/sec")
	fmt.Printf("%-13s %-15s %-15.0f\n", "ACP", acpDur, acpQPS)
	fmt.Printf("%-13s %-15s %-15.0f\n", "gRPC", grpcDur, grpcQPS)
	if grpcQPS > 0 {
		fmt.Printf("%-13s %.2fx\n", "speedup", acpQPS/grpcQPS)
	}
}

// dialACP 建立一个 ACP 连接（含 HELLO 握手）。
func dialACP(ctx context.Context, addr string) (*internalacp.Client, error) {
	return internalacp.Dial(ctx, internalacp.DialOptions{Addr: addr, UserID: "bench"})
}

// dialGRPC 建立一个 gRPC 连接。
func dialGRPC(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

// runWorkers 已移至 scenarios.go 的 runEqualWorkers，避免重复。

// healthCheck 调用 gRPC health.Check，作为 gRPC 侧的 ping。
func healthCheck(ctx context.Context, conn *grpc.ClientConn) error {
	cli := healthpb.NewHealthClient(conn)
	_, err := cli.Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}
