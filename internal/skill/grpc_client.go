package skill

import (
	"context"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
)

type GRPCSelector struct {
	Client pb.SkillServiceClient
	TopK   int
}

func (s GRPCSelector) Select(ctx context.Context, query string) ([]Skill, error) {
	if s.Client == nil {
		return nil, nil
	}
	resp, err := s.Client.SelectSkills(ctx, &pb.SelectSkillsRequest{Query: query, TopK: uint32(s.TopK)})
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(resp.GetSkills()))
	for _, sk := range resp.GetSkills() {
		out = append(out, Skill{
			Name:        sk.GetName(),
			Description: sk.GetDescription(),
			SHA256:      sk.GetSha256(),
			Path:        sk.GetPath(),
			Content:     sk.GetContent(),
		})
	}
	return out, nil
}
