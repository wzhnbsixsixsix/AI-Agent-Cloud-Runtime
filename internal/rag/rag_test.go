package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHashEmbedderDeterministic(t *testing.T) {
	emb, err := NewHashEmbedder(16)
	if err != nil {
		t.Fatalf("embedder: %v", err)
	}
	a, err := emb.Embed("Sandbox file tool")
	if err != nil {
		t.Fatalf("embed a: %v", err)
	}
	b, err := emb.Embed("sandbox   file tool")
	if err != nil {
		t.Fatalf("embed b: %v", err)
	}
	if len(a) != 16 || len(b) != 16 {
		t.Fatalf("bad dim: %d %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("embedding not deterministic at %d: %v != %v", i, a[i], b[i])
		}
	}
}

func TestHashEmbedderInvalidDim(t *testing.T) {
	if _, err := NewHashEmbedder(0); !errors.Is(err, ErrInvalidDim) {
		t.Fatalf("want ErrInvalidDim, got %v", err)
	}
}

func TestChunkTextStable(t *testing.T) {
	emb, _ := NewHashEmbedder(8)
	content := strings.Repeat("AgentForge RAG chunking works.\n\n", 80)
	first, err := ChunkText("t1", "README.md", content, emb)
	if err != nil {
		t.Fatalf("chunk first: %v", err)
	}
	second, err := ChunkText("t1", "README.md", content, emb)
	if err != nil {
		t.Fatalf("chunk second: %v", err)
	}
	if len(first) < 2 {
		t.Fatalf("want multiple chunks, got %d", len(first))
	}
	if first[0].ChunkID != second[0].ChunkID || first[0].Source != "README.md" || first[0].TenantID != "t1" {
		t.Fatalf("unstable chunks: %+v %+v", first[0], second[0])
	}
}

func TestChunkTextEmpty(t *testing.T) {
	emb, _ := NewHashEmbedder(8)
	if _, err := ChunkText("t", "s", "  ", emb); !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("want ErrEmptyContent, got %v", err)
	}
}

func TestKeywordReranker(t *testing.T) {
	results := []Result{
		{Chunk: Chunk{Source: "b", Content: "unrelated"}, Score: 0.5},
		{Chunk: Chunk{Source: "a", Content: "sandbox fs_write file"}, Score: 0.5},
	}
	got := KeywordReranker{}.Rerank("sandbox file", results)
	if len(got) != 2 || got[0].Source != "a" {
		t.Fatalf("bad rerank: %+v", got)
	}
}

type memoryStore struct {
	chunks []Chunk
}

func (m *memoryStore) EnsureSchema(context.Context, int) error { return nil }
func (m *memoryStore) Close() error                            { return nil }
func (m *memoryStore) DeleteSource(_ context.Context, tenantID, source string) error {
	var kept []Chunk
	for _, ch := range m.chunks {
		if ch.TenantID == tenantID && ch.Source == source {
			continue
		}
		kept = append(kept, ch)
	}
	m.chunks = kept
	return nil
}
func (m *memoryStore) UpsertChunks(_ context.Context, chunks []Chunk) error {
	m.chunks = append(m.chunks, chunks...)
	return nil
}
func (m *memoryStore) Query(_ context.Context, tenantID, query string, _ []float32, topK int, minScore float64) ([]Result, error) {
	var out []Result
	q := tokenize(query)
	for _, ch := range m.chunks {
		if ch.TenantID != tenantID {
			continue
		}
		score := float64(overlap(q, tokenize(ch.Content))) / 10
		if score >= minScore {
			out = append(out, Result{Chunk: ch, Score: score})
		}
	}
	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

func TestServiceTenantIsolationAndMinScore(t *testing.T) {
	emb, _ := NewHashEmbedder(8)
	svc := &Service{Store: &memoryStore{}, Embedder: emb, Reranker: KeywordReranker{}}
	if _, err := svc.IngestText(context.Background(), "tenant-a", "a.md", "sandbox fs_write file tool"); err != nil {
		t.Fatalf("ingest a: %v", err)
	}
	if _, err := svc.IngestText(context.Background(), "tenant-b", "b.md", "sandbox fs_read docs"); err != nil {
		t.Fatalf("ingest b: %v", err)
	}
	got, err := svc.Retrieve(context.Background(), "tenant-a", "sandbox file", 5, 0.1)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(got) != 1 || got[0].TenantID != "tenant-a" {
		t.Fatalf("bad tenant retrieval: %+v", got)
	}
	none, err := svc.Retrieve(context.Background(), "tenant-a", "sandbox file", 5, 0.9)
	if err != nil {
		t.Fatalf("retrieve none: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("want min score filter, got %+v", none)
	}
}
