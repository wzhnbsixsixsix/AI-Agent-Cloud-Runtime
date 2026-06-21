//go:build integration_pg

package rag

import (
	"context"
	"os"
	"testing"
)

func TestPGStoreIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN required")
	}
	ctx := context.Background()
	store, err := OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	defer store.Close()
	emb, _ := NewHashEmbedder(32)
	if err := store.EnsureSchema(ctx, emb.Dim()); err != nil {
		t.Fatalf("schema: %v", err)
	}
	svc := &Service{Store: store, Embedder: emb, Reranker: KeywordReranker{}}
	if _, err := svc.IngestText(ctx, "it", "doc.md", "AgentForge W6 uses pgvector for RAG retrieval."); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	got, err := svc.Retrieve(ctx, "it", "pgvector RAG", 3, 0)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(got) == 0 || got[0].Source != "doc.md" {
		t.Fatalf("bad results: %+v", got)
	}
	if _, err := svc.IngestText(ctx, "it", "doc.md", "Replacement content mentions W6 only once."); err != nil {
		t.Fatalf("reingest: %v", err)
	}
	got, err = svc.Retrieve(ctx, "it", "Replacement", 3, 0)
	if err != nil {
		t.Fatalf("retrieve replacement: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want replacement source only once, got %+v", got)
	}
}
