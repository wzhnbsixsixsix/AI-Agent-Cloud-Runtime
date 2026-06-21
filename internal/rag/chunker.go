package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

const (
	defaultChunkMaxChars = 1200
	defaultChunkOverlap  = 160
)

// ChunkText splits text into deterministic chunks and embeds each chunk.
func ChunkText(tenantID, source, content string, embedder Embedder) ([]Chunk, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "default"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "unknown"
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, ErrEmptyContent
	}

	parts := splitText(content, defaultChunkMaxChars, defaultChunkOverlap)
	out := make([]Chunk, 0, len(parts))
	for idx, part := range parts {
		emb, err := embedder.Embed(part)
		if err != nil {
			return nil, err
		}
		out = append(out, Chunk{
			TenantID:  tenantID,
			Source:    source,
			ChunkID:   chunkID(tenantID, source, idx, part),
			Ordinal:   idx,
			Content:   part,
			Embedding: emb,
		})
	}
	return out, nil
}

func splitText(content string, maxChars, overlap int) []string {
	paragraphs := strings.Split(content, "\n\n")
	var chunks []string
	var cur strings.Builder
	flush := func() {
		txt := strings.TrimSpace(cur.String())
		if txt != "" {
			chunks = append(chunks, txt)
		}
		cur.Reset()
	}
	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if len([]rune(para)) > maxChars {
			flush()
			chunks = append(chunks, splitLong(para, maxChars, overlap)...)
			continue
		}
		if cur.Len() > 0 && len([]rune(cur.String()))+len([]rune(para))+2 > maxChars {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	flush()
	return chunks
}

func splitLong(s string, maxChars, overlap int) []string {
	rs := []rune(s)
	var out []string
	for start := 0; start < len(rs); {
		end := start + maxChars
		if end > len(rs) {
			end = len(rs)
		}
		if end < len(rs) {
			for cut := end; cut > start+maxChars/2; cut-- {
				if unicode.IsSpace(rs[cut-1]) {
					end = cut
					break
				}
			}
		}
		out = append(out, strings.TrimSpace(string(rs[start:end])))
		if end >= len(rs) {
			break
		}
		start = end - overlap
		if start < 0 {
			start = 0
		}
	}
	return out
}

func chunkID(tenantID, source string, ordinal int, content string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", tenantID, source, ordinal, content)))
	return hex.EncodeToString(sum[:16])
}
