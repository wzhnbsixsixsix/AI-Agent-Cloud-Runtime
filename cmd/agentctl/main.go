package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agentctl",
	Short: "AgentForge CLI",
	Long:  "agentctl 是 AgentForge 运行时的命令行客户端，支持 gRPC 与自研 ACP 协议。",
}

func main() {
	rootCmd.AddCommand(newRunCmd(), newResumeCmd(), newToolCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
