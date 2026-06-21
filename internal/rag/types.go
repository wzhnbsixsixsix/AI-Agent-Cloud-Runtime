// Package rag implements W6 retrieval augmented generation primitives.
package rag

import (
	"context"
	"errors"
)

var (
	ErrEmptyContent = errors.New("rag content is empty")
	ErrEmptyQuery   = errors.New("rag query is empty")
	ErrInvalidDim   = errors.New("rag embedding dimension must be positive")
)

// Chunk is one indexed document fragment.
type Chunk struct {
	TenantID  string
	Source    string
	ChunkID   string
	Ordinal   int
	Content   string
	Embedding []float32
}

// Result is one retrieved chunk with scoring details.
type Result struct {
	Chunk
	Score       float64
	VectorScore float64
	TextScore   float64
}

// Embedder turns text into a fixed-size vector.
type Embedder interface {
	Dim() int
	Embed(text string) ([]float32, error)
}

// Store persists and retrieves chunks.
type Store interface {
	EnsureSchema(ctx context.Context, dim int) error
	DeleteSource(ctx context.Context, tenantID, source string) error
	UpsertChunks(ctx context.Context, chunks []Chunk) error
	Query(ctx context.Context, tenantID, query string, embedding []float32, topK int, minScore float64) ([]Result, error)
	Close() error
}

// Retriever is the small interface agent.Runner needs.
type Retriever interface {
	Retrieve(ctx context.Context, tenantID, query string, topK int, minScore float64) ([]Result, error)
}

// Reranker can reorder or filter raw store results.
type Reranker interface {
	Rerank(query string, results []Result) []Result
}
