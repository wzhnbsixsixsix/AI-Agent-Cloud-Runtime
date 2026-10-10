package llm

import (
	"fmt"
	"strings"
	"time"
)

// FactoryConfig 构造 Provider 所需的所有配置。
type FactoryConfig struct {
	Provider              string // modelscope | mock
	ModelScopeBaseURL     string
	ModelScopeAccessToken string
	ModelScopeModel       string
	ModelScopeMaxTokens   int
	ModelScopeTimeout     time.Duration
}

// NewFromConfig 按 cfg 选择 provider。
func NewFromConfig(cfg FactoryConfig) (Provider, error) {
	switch strings.ToLower(cfg.Provider) {
	case "", "modelscope":
		if cfg.ModelScopeAccessToken == "" {
			return nil, fmt.Errorf("modelscope access token empty")
		}
		if !strings.HasPrefix(cfg.ModelScopeModel, "Qwen/") {
			return nil, fmt.Errorf("MODELSCOPE_MODEL must identify a Qwen/ model")
		}
		modelscope := NewOpenAIWithOptions(cfg.ModelScopeBaseURL, cfg.ModelScopeAccessToken, cfg.ModelScopeModel, cfg.ModelScopeTimeout, cfg.ModelScopeMaxTokens, false)
		return &ModelRouter{
			// No fallback: obsolete persisted model IDs must not use Qwen credentials.
			Routes: map[string]Provider{
				"":                  modelscope,
				cfg.ModelScopeModel: modelscope,
			},
		}, nil
	case "mock":
		return NewMock(nil, 0), nil
	default:
		return nil, fmt.Errorf("unknown llm provider: %s", cfg.Provider)
	}
}
