package config

import "testing"

func TestLoadWorkerUsesWEEXFallbackKey(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WEEX_API_KEY", "weex-test-key")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if cfg.OpenAIAPIKey != "weex-test-key" {
		t.Fatalf("expected WEEX key fallback, got %q", cfg.OpenAIAPIKey)
	}
}

func TestLoadWorkerRequiresOpenAICompatibleKey(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WEEX_API_KEY", "")
	if _, err := LoadWorker(); err == nil {
		t.Fatal("expected missing key error")
	}
}
