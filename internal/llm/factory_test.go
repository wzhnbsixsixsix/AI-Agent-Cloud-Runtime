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

func TestFactoryRoutesModelScopeWithSeparateToken(t *testing.T) {
	var glmCalled bool
	glmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		glmCalled = true
		http.Error(w, "unexpected GLM request", http.StatusInternalServerError)
	}))
	defer glmServer.Close()

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
		Provider:              "openai",
		OpenAIBaseURL:         glmServer.URL,
		OpenAIAPIKey:          "glm-token",
		OpenAIModel:           "glm-4.7-flash",
		OpenAITimeout:         time.Second,
		ModelScopeBaseURL:     modelScopeServer.URL,
		ModelScopeAccessToken: "modelscope-token",
		ModelScopeModel:       "Qwen/Qwen3.5-35B-A3B",
		ModelScopeTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	ch, err := provider.Stream(context.Background(), Req{
		Model:    "Qwen/Qwen3.5-35B-A3B",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}

	if glmCalled {
		t.Fatal("Qwen request was sent to the GLM endpoint")
	}
	if gotAuth != "Bearer modelscope-token" {
		t.Fatalf("unexpected ModelScope authorization header %q", gotAuth)
	}
	if gotModel != "Qwen/Qwen3.5-35B-A3B" {
		t.Fatalf("unexpected ModelScope model %q", gotModel)
	}
}

func TestFactoryRejectsModelScopeRunWithoutToken(t *testing.T) {
	provider, err := NewFromConfig(FactoryConfig{
		Provider:        "openai",
		OpenAIBaseURL:   "https://example.invalid/v1",
		OpenAIAPIKey:    "glm-token",
		OpenAIModel:     "glm-4.7-flash",
		ModelScopeModel: "Qwen/Qwen3.5-35B-A3B",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, err := provider.Stream(context.Background(), Req{Model: "Qwen/Qwen3.5-35B-A3B"}); err == nil {
		t.Fatal("expected missing ModelScope token error")
	}
}
