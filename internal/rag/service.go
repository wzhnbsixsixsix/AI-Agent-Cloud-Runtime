package rag

import (
	"context"
	"strings"
)

// Service coordinates chunking, embedding, storage, and reranking.
type Service struct {
	Store    Store
	Embedder Embedder
	Reranker Reranker
}

// IngestText replaces all chunks for tenant/source with chunks from content.
func (s *Service) IngestText(ctx context.Context, tenantID, source, content string) ([]Chunk, error) {
	if s == nil || s.Store == nil || s.Embedder == nil {
		return nil, nil
	}
	chunks, err := ChunkText(tenantID, source, content, s.Embedder)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteSource(ctx, chunks[0].TenantID, chunks[0].Source); err != nil {
		return nil, err
	}
	if err := s.Store.UpsertChunks(ctx, chunks); err != nil {
		return nil, err
	}
	return chunks, nil
}

// Retrieve returns relevant chunks for query.
func (s *Service) Retrieve(ctx context.Context, tenantID, query string, topK int, minScore float64) ([]Result, error) {
	if s == nil || s.Store == nil || s.Embedder == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	if topK <= 0 {
		topK = 5
	}
	if tenantID == "" {
		tenantID = "default"
	}
	emb, err := s.Embedder.Embed(query)
	if err != nil {
		return nil, err
	}
	raw, err := s.Store.Query(ctx, tenantID, query, emb, topK*3, minScore)
	if err != nil {
		return nil, err
	}
	if s.Reranker != nil {
		raw = s.Reranker.Rerank(query, raw)
	}
	if len(raw) > topK {
		raw = raw[:topK]
	}
	return raw, nil
}

// Close releases store resources.
func (s *Service) Close() error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.Close()
}
