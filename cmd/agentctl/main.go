package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agentctl",
	Short: "AgentForge CLI",
	Long:  "agentctl 是 AgentForge 运行时的命令行客户端，通过 gRPC 双向流与 gateway 交互。",
}

func main() {
	rootCmd.AddCommand(newRunCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
