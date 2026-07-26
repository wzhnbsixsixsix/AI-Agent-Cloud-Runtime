package llm

import (
	"context"
	"testing"
)

type recordingProvider struct {
	name string
	req  Req
}

func (p *recordingProvider) Name() string { return p.name }

func (p *recordingProvider) Stream(_ context.Context, req Req) (<-chan TokenEvent, error) {
	p.req = req
	out := make(chan TokenEvent, 1)
	out <- TokenEvent{Done: true}
	close(out)
	return out, nil
}

func TestModelRouterSelectsConfiguredModel(t *testing.T) {
	glm := &recordingProvider{name: "glm"}
	qwen := &recordingProvider{name: "modelscope"}
	router := &ModelRouter{
		Default: glm,
		Routes: map[string]Provider{
			"Qwen/Qwen3.6-27B": qwen,
		},
	}

	ch, err := router.Stream(context.Background(), Req{Model: "Qwen/Qwen3.6-27B"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}
	if qwen.req.Model != "Qwen/Qwen3.6-27B" {
		t.Fatalf("Qwen request was not routed to ModelScope: %+v", qwen.req)
	}
	if glm.req.Model != "" {
		t.Fatalf("default provider unexpectedly received request: %+v", glm.req)
	}
}

func TestModelRouterFallsBackToDefault(t *testing.T) {
	glm := &recordingProvider{name: "glm"}
	router := &ModelRouter{Default: glm, Routes: map[string]Provider{}}

	ch, err := router.Stream(context.Background(), Req{Model: "custom-model"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}
	if glm.req.Model != "custom-model" {
		t.Fatalf("default provider did not receive request: %+v", glm.req)
	}
}
