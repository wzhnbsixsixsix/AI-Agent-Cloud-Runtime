package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFactoryRoutesModelScope(t *testing.T) {
	var gotAuth, gotModel string
	modelScopeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &payload)
		gotModel = payload.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer modelScopeServer.Close()

	provider, err := NewFromConfig(FactoryConfig{
		Provider:              "modelscope",
		ModelScopeBaseURL:     modelScopeServer.URL,
		ModelScopeAccessToken: "modelscope-token",
		ModelScopeModel:       "Qwen/Qwen3.5-35B-A3B",
		ModelScopeTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	for _, requestedModel := range []string{"", "Qwen/Qwen3.5-35B-A3B"} {
		ch, err := provider.Stream(context.Background(), Req{
			Model:    requestedModel,
			Messages: []Message{{Role: RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		for range ch {
		}
		if gotAuth != "Bearer modelscope-token" {
			t.Fatal("unexpected ModelScope authorization")
		}
		if gotModel != "Qwen/Qwen3.5-35B-A3B" {
			t.Fatalf("unexpected ModelScope model %q", gotModel)
		}
	}
}

func TestFactoryRejectsModelScopeWithoutToken(t *testing.T) {
	_, err := NewFromConfig(FactoryConfig{
		Provider:        "modelscope",
		ModelScopeModel: "Qwen/Qwen3.5-35B-A3B",
	})
	if err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestFactoryRejectsRetiredProviderAndModels(t *testing.T) {
	if _, err := NewFromConfig(FactoryConfig{Provider: "openai"}); err == nil {
		t.Fatal("retired provider accepted")
	}
	provider, err := NewFromConfig(FactoryConfig{
		ModelScopeBaseURL:     "https://example.invalid/v1",
		ModelScopeAccessToken: "test-token",
		ModelScopeModel:       "Qwen/Qwen3.5-35B-A3B",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"glm-4.7-flash", "unknown/model", "Qwen/not-configured"} {
		if _, err := provider.Stream(context.Background(), Req{Model: model}); err == nil {
			t.Fatalf("unconfigured model %q accepted", model)
		}
	}
}
