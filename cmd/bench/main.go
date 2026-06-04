// agentforge-bench: ACP vs gRPC 对比 benchmark 工具。
//
//	bench rtt        --grpc=:8080 --acp=:8090 -n 5000 -c 1
//	bench throughput --grpc=:8080 --acp=:8090 -n 50000 -c 64
//	bench connect    --grpc=:8080 --acp=:8090 -n 1000 -c 50
//
// 4 个面试卖点场景中，rtt / throughput / connect 在此实现；
// resume 见 agentctl resume 子命令演示。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "bench",
		Short: "AgentForge ACP vs gRPC benchmark",
	}
	root.AddCommand(newRTTCmd(), newThroughputCmd(), newConnectCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
