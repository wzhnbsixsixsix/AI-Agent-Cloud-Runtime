package config

import "testing"

func TestLoadWorkerRequiresOpenAICompatibleKey(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := LoadWorker(); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestLoadWorkerReadsModelScopeConfig(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "modelscope")
	t.Setenv("MODELSCOPE_ACCESS_TOKEN", "modelscope-test-token")
	t.Setenv("MODELSCOPE_MODEL", "Qwen/Qwen3.5-35B-A3B")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatalf("load worker: %v", err)
	}
	if cfg.ModelScopeAccessToken != "modelscope-test-token" || cfg.ModelScopeModel != "Qwen/Qwen3.5-35B-A3B" {
		t.Fatalf("unexpected ModelScope config: %+v", cfg)
	}
}
