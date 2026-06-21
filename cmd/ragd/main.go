package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/rag"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.LoadRAG()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	svc, err := newRAGService(context.Background(), cfg.PostgresDSN, cfg.RAGEmbedDim)
	if err != nil {
		logger.Error("rag init", "err", err)
		os.Exit(1)
	}
	defer svc.Close()
	lis, err := net.Listen("tcp", cfg.RAGGRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}
	srv := grpc.NewServer()
	pb.RegisterRAGServiceServer(srv, &ragService{svc: svc})
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.DiscoveryEnabled {
		reg, err := discovery.Register(rootCtx, cfg.EtcdEndpoints, discovery.Instance{
			Service: "ragd",
			ID:      hostnameID("ragd"),
			Addr:    cfg.RAGGRPCAddr,
		}, 10)
		if err != nil {
			logger.Error("discovery register", "err", err)
		} else {
			defer reg.Close()
		}
	}
	go func() {
		logger.Info("ragd serving", "addr", cfg.RAGGRPCAddr, "dim", cfg.RAGEmbedDim)
		if err := srv.Serve(lis); err != nil {
			logger.Error("serve", "err", err)
			stop()
		}
	}()
	<-rootCtx.Done()
	srv.GracefulStop()
}

func hostnameID(prefix string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return prefix
	}
	return prefix + "-" + name
}

func newRAGService(ctx context.Context, dsn string, dim int) (*rag.Service, error) {
	if dsn == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}
	emb, err := rag.NewHashEmbedder(dim)
	if err != nil {
		return nil, err
	}
	store, err := rag.OpenPG(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := store.EnsureSchema(ctx, emb.Dim()); err != nil {
		_ = store.Close()
		return nil, err
	}
	return &rag.Service{Store: store, Embedder: emb, Reranker: rag.KeywordReranker{}}, nil
}

type ragService struct {
	pb.UnimplementedRAGServiceServer
	svc *rag.Service
}

func (s *ragService) IngestRAG(ctx context.Context, req *pb.IngestRAGRequest) (*pb.IngestRAGResponse, error) {
	chunks, err := s.svc.IngestText(ctx, req.GetTenantId(), req.GetSource(), req.GetContent())
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(chunks))
	for _, ch := range chunks {
		ids = append(ids, ch.ChunkID)
	}
	return &pb.IngestRAGResponse{TenantId: chunks[0].TenantID, Source: chunks[0].Source, Chunks: uint32(len(chunks)), ChunkIds: ids}, nil
}

func (s *ragService) QueryRAG(ctx context.Context, req *pb.QueryRAGRequest) (*pb.QueryRAGResponse, error) {
	results, err := s.svc.Retrieve(ctx, req.GetTenantId(), req.GetQuery(), int(req.GetTopK()), req.GetMinScore())
	if err != nil {
		return nil, err
	}
	return &pb.QueryRAGResponse{Results: toPBChunks(results)}, nil
}

func (s *ragService) RetrieveContext(ctx context.Context, req *pb.RetrieveContextRequest) (*pb.RetrieveContextResponse, error) {
	results, err := s.svc.Retrieve(ctx, req.GetTenantId(), req.GetQuery(), int(req.GetTopK()), req.GetMinScore())
	if err != nil {
		return nil, err
	}
	return &pb.RetrieveContextResponse{Results: toPBChunks(results), RenderedContext: rag.RenderSystemMessage(results)}, nil
}

func toPBChunks(results []rag.Result) []*pb.RAGChunk {
	out := make([]*pb.RAGChunk, 0, len(results))
	for _, r := range results {
		out = append(out, &pb.RAGChunk{
			TenantId:    r.TenantID,
			Source:      r.Source,
			ChunkId:     r.ChunkID,
			Ordinal:     uint32(r.Ordinal),
			Content:     r.Content,
			Score:       r.Score,
			VectorScore: r.VectorScore,
			TextScore:   r.TextScore,
		})
	}
	return out
}
