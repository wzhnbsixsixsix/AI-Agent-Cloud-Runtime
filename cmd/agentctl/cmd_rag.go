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

func newRAGCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rag",
		Short: "RAG 文档索引与检索（W6 demo）",
	}
	cmd.AddCommand(newRAGIngestCmd(), newRAGQueryCmd())
	return cmd
}

func newRAGIngestCmd() *cobra.Command {
	var (
		path    string
		source  string
		tenant  string
		dialTo  string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "读取本地文本文件并写入 RAG store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return errors.New("--path is required")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			if source == "" {
				source = path
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
			resp, err := pb.NewAgentServiceClient(conn).IngestRAG(ctx, &pb.IngestRAGRequest{
				TenantId: tenant,
				Source:   source,
				Content:  string(raw),
			})
			if err != nil {
				return fmt.Errorf("ingest rag: %w", err)
			}
			fmt.Printf("[rag] tenant=%s source=%s chunks=%d\n", resp.GetTenantId(), resp.GetSource(), resp.GetChunks())
			for _, id := range resp.GetChunkIds() {
				fmt.Println(id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "本地文本/Markdown/代码文件路径")
	cmd.Flags().StringVar(&source, "source", "", "RAG source 标识；默认等于 --path")
	cmd.Flags().StringVar(&tenant, "tenant", "default", "tenant id")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway gRPC 地址（默认 GATEWAY_DIAL_ADDR）")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "超时")
	return cmd
}

func newRAGQueryCmd() *cobra.Command {
	var (
		query    string
		tenant   string
		dialTo   string
		topK     uint32
		minScore float64
		timeout  time.Duration
		raw      bool
	)
	cmd := &cobra.Command{
		Use:   "query",
		Short: "查询 RAG store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(query) == "" {
				return errors.New("--query is required")
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
			resp, err := pb.NewAgentServiceClient(conn).QueryRAG(ctx, &pb.QueryRAGRequest{
				TenantId: tenant,
				Query:    query,
				TopK:     topK,
				MinScore: minScore,
			})
			if err != nil {
				return fmt.Errorf("query rag: %w", err)
			}
			if raw {
				for _, r := range resp.GetResults() {
					fmt.Printf("----- %s %s score=%.4f -----\n%s\n\n", r.GetSource(), r.GetChunkId(), r.GetScore(), r.GetContent())
				}
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SCORE\tSOURCE\tCHUNK\tPREVIEW")
			for _, r := range resp.GetResults() {
				preview := strings.ReplaceAll(strings.TrimSpace(r.GetContent()), "\n", " ")
				if len([]rune(preview)) > 96 {
					preview = string([]rune(preview)[:93]) + "..."
				}
				fmt.Fprintf(tw, "%.4f\t%s\t%s\t%s\n", r.GetScore(), r.GetSource(), r.GetChunkId(), preview)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "检索 query")
	cmd.Flags().StringVar(&tenant, "tenant", "default", "tenant id")
	cmd.Flags().StringVar(&dialTo, "addr", "", "gateway gRPC 地址（默认 GATEWAY_DIAL_ADDR）")
	cmd.Flags().Uint32Var(&topK, "top-k", 5, "返回结果数")
	cmd.Flags().Float64Var(&minScore, "min-score", 0, "最低分数")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "超时")
	cmd.Flags().BoolVar(&raw, "raw", false, "打印完整 chunk 内容")
	return cmd
}
