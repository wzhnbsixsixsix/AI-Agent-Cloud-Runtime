package llm

import (
	"context"
	"fmt"
	"strings"
)

// ModelRouter sends requests for explicitly configured models to their provider.
// Unknown or empty models continue to use Default for backward compatibility.
type ModelRouter struct {
	Default      Provider
	Routes       map[string]Provider
	PrefixRoutes map[string]Provider
}

func (r *ModelRouter) Name() string { return "model-router" }

func (r *ModelRouter) Stream(ctx context.Context, req Req) (<-chan TokenEvent, error) {
	if provider, ok := r.Routes[strings.TrimSpace(req.Model)]; ok {
		return provider.Stream(ctx, req)
	}
	for prefix, provider := range r.PrefixRoutes {
		if strings.HasPrefix(strings.TrimSpace(req.Model), prefix) {
			return provider.Stream(ctx, req)
		}
	}
	if r.Default == nil {
		return nil, fmt.Errorf("no llm provider configured for model %q", req.Model)
	}
	return r.Default.Stream(ctx, req)
}

type unavailableProvider struct {
	name   string
	reason string
}

func (p unavailableProvider) Name() string { return p.name }

func (p unavailableProvider) Stream(_ context.Context, _ Req) (<-chan TokenEvent, error) {
	return nil, fmt.Errorf("%s", p.reason)
}
