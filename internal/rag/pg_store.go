package rag

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PGStore stores RAG chunks in Postgres + pgvector.
type PGStore struct {
	db *sql.DB
}

// OpenPG opens a pgx-backed database/sql connection.
func OpenPG(ctx context.Context, dsn string) (*PGStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &PGStore{db: db}, nil
}

// EnsureSchema creates the pgvector extension and rag_chunks table.
func (s *PGStore) EnsureSchema(ctx context.Context, dim int) error {
	if dim <= 0 {
		return ErrInvalidDim
	}
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS rag_chunks (
			tenant_id TEXT NOT NULL,
			source TEXT NOT NULL,
			chunk_id TEXT PRIMARY KEY,
			ordinal INT NOT NULL,
			content TEXT NOT NULL,
			embedding vector(%d) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`, dim),
		`CREATE INDEX IF NOT EXISTS rag_chunks_tenant_source_idx ON rag_chunks (tenant_id, source)`,
		`CREATE INDEX IF NOT EXISTS rag_chunks_tenant_fts_idx ON rag_chunks USING GIN (to_tsvector('simple', content))`,
		`CREATE INDEX IF NOT EXISTS rag_chunks_embedding_hnsw_idx ON rag_chunks USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSource removes prior chunks for a tenant/source pair.
func (s *PGStore) DeleteSource(ctx context.Context, tenantID, source string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rag_chunks WHERE tenant_id=$1 AND source=$2`, tenantID, source)
	return err
}

// UpsertChunks writes chunks. Re-ingest should call DeleteSource first.
func (s *PGStore) UpsertChunks(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO rag_chunks (tenant_id, source, chunk_id, ordinal, content, embedding)
		VALUES ($1, $2, $3, $4, $5, $6::vector)
		ON CONFLICT (chunk_id) DO UPDATE SET
			tenant_id=EXCLUDED.tenant_id,
			source=EXCLUDED.source,
			ordinal=EXCLUDED.ordinal,
			content=EXCLUDED.content,
			embedding=EXCLUDED.embedding,
			created_at=now()`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, ch := range chunks {
		if _, err := stmt.ExecContext(ctx, ch.TenantID, ch.Source, ch.ChunkID, ch.Ordinal, ch.Content, vectorLiteral(ch.Embedding)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Query runs a hybrid vector + full-text query.
func (s *PGStore) Query(ctx context.Context, tenantID, query string, embedding []float32, topK int, minScore float64) ([]Result, error) {
	if topK <= 0 {
		topK = 5
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, source, chunk_id, ordinal, content,
			GREATEST(0, 1 - (embedding <=> $2::vector)) AS vector_score,
			ts_rank_cd(to_tsvector('simple', content), plainto_tsquery('simple', $3)) AS text_score,
			(GREATEST(0, 1 - (embedding <=> $2::vector)) * 0.75
				+ ts_rank_cd(to_tsvector('simple', content), plainto_tsquery('simple', $3)) * 0.25) AS score
		FROM rag_chunks
		WHERE tenant_id=$1
		ORDER BY score DESC, source ASC, ordinal ASC
		LIMIT $4`, tenantID, vectorLiteral(embedding), query, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.TenantID, &r.Source, &r.ChunkID, &r.Ordinal, &r.Content, &r.VectorScore, &r.TextScore, &r.Score); err != nil {
			return nil, err
		}
		if r.Score >= minScore {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// Close closes the underlying DB.
func (s *PGStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
