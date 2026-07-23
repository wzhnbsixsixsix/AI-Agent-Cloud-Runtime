package config

import "testing"

func TestLoadWorkerRequiresOpenAICompatibleKey(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := LoadWorker(); err == nil {
		t.Fatal("expected missing key error")
	}
}
