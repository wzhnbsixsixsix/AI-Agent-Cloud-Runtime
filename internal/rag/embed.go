package rag

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
)

// HashEmbedder is deterministic and local, so W6 demos do not require an
// external embedding provider.
type HashEmbedder struct {
	dim int
}

// NewHashEmbedder returns a deterministic embedder with dim dimensions.
func NewHashEmbedder(dim int) (*HashEmbedder, error) {
	if dim <= 0 {
		return nil, ErrInvalidDim
	}
	return &HashEmbedder{dim: dim}, nil
}

// Dim returns the embedding dimension.
func (e *HashEmbedder) Dim() int { return e.dim }

// Embed hashes normalized tokens into a signed feature vector and L2 normalizes it.
func (e *HashEmbedder) Embed(text string) ([]float32, error) {
	if e.dim <= 0 {
		return nil, ErrInvalidDim
	}
	vec := make([]float32, e.dim)
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return vec, nil
	}
	for _, tk := range tokens {
		sum := sha256.Sum256([]byte(tk))
		idx := int(binary.BigEndian.Uint32(sum[0:4]) % uint32(e.dim))
		sign := float32(1)
		if sum[4]&1 == 1 {
			sign = -1
		}
		vec[idx] += sign
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v * v)
	}
	if norm == 0 {
		return vec, nil
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= scale
	}
	return vec, nil
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}
