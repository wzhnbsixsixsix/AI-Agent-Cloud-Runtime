package rag

import (
	"context"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
)

type GRPCRetriever struct {
	Client pb.RAGServiceClient
}

func (r GRPCRetriever) Retrieve(ctx context.Context, tenantID, query string, topK int, minScore float64) ([]Result, error) {
	if r.Client == nil {
		return nil, nil
	}
	resp, err := r.Client.RetrieveContext(ctx, &pb.RetrieveContextRequest{
		TenantId: tenantID,
		Query:    query,
		TopK:     uint32(topK),
		MinScore: minScore,
	})
	if err != nil {
		return nil, err
	}
	return ResultsFromPB(resp.GetResults()), nil
}

func ResultsFromPB(chunks []*pb.RAGChunk) []Result {
	out := make([]Result, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, Result{
			Chunk: Chunk{
				TenantID: ch.GetTenantId(),
				Source:   ch.GetSource(),
				ChunkID:  ch.GetChunkId(),
				Ordinal:  int(ch.GetOrdinal()),
				Content:  ch.GetContent(),
			},
			Score:       ch.GetScore(),
			VectorScore: ch.GetVectorScore(),
			TextScore:   ch.GetTextScore(),
		})
	}
	return out
}
