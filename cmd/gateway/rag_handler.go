package main

import (
	"context"
	"fmt"
	"time"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type ragHandler struct {
	conn *grpc.ClientConn
	cli  pb.RAGServiceClient
}

func newRAGHandler(ctx context.Context, addr string) (*ragHandler, error) {
	if addr == "" {
		return nil, fmt.Errorf("RAG_SERVICE_ADDR is required when RAG_ENABLED=true")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	return &ragHandler{conn: conn, cli: pb.NewRAGServiceClient(conn)}, nil
}

func (h *ragHandler) close() {
	if h != nil && h.conn != nil {
		_ = h.conn.Close()
	}
}

func (h *ragHandler) ingest(ctx context.Context, req *pb.IngestRAGRequest) (*pb.IngestRAGResponse, error) {
	if h == nil || h.cli == nil {
		return nil, status.Error(codes.Unavailable, "rag service disabled on this gateway")
	}
	return h.cli.IngestRAG(ctx, req)
}

func (h *ragHandler) query(ctx context.Context, req *pb.QueryRAGRequest) (*pb.QueryRAGResponse, error) {
	if h == nil || h.cli == nil {
		return nil, status.Error(codes.Unavailable, "rag service disabled on this gateway")
	}
	return h.cli.QueryRAG(ctx, req)
}
