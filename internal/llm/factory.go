package llm

import (
	"fmt"
	"strings"
	"time"
)

// FactoryConfig 构造 Provider 所需的所有配置。
type FactoryConfig struct {
	Provider              string // openai | modelscope | mock
	OpenAIBaseURL         string
	OpenAIAPIKey          string
	OpenAIModel           string
	OpenAIMaxTokens       int
	ThinkingEnabled       bool
	OpenAITimeout         time.Duration
	ModelScopeBaseURL     string
	ModelScopeAccessToken string
	ModelScopeModel       string
	ModelScopeMaxTokens   int
	ModelScopeTimeout     time.Duration
}

// NewFromConfig 按 cfg 选择 provider。
func NewFromConfig(cfg FactoryConfig) (Provider, error) {
	switch strings.ToLower(cfg.Provider) {
	case "", "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("openai api key empty")
		}
		primary := NewOpenAIWithOptions(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAITimeout, cfg.OpenAIMaxTokens, cfg.ThinkingEnabled)
		if cfg.ModelScopeModel == "" {
			return primary, nil
		}
		var modelscope Provider = unavailableProvider{
			name:   "modelscope",
			reason: "MODELSCOPE_ACCESS_TOKEN is required for ModelScope models",
		}
		if cfg.ModelScopeAccessToken != "" {
			modelscope = NewOpenAIWithOptions(cfg.ModelScopeBaseURL, cfg.ModelScopeAccessToken, cfg.ModelScopeModel, cfg.ModelScopeTimeout, cfg.ModelScopeMaxTokens, false)
		}
		return &ModelRouter{
			Default: primary,
			Routes: map[string]Provider{
				cfg.OpenAIModel:     primary,
				cfg.ModelScopeModel: modelscope,
			},
			PrefixRoutes: map[string]Provider{"Qwen/": modelscope},
		}, nil
	case "modelscope":
		if cfg.ModelScopeAccessToken == "" {
			return nil, fmt.Errorf("modelscope access token empty")
		}
		modelscope := NewOpenAIWithOptions(cfg.ModelScopeBaseURL, cfg.ModelScopeAccessToken, cfg.ModelScopeModel, cfg.ModelScopeTimeout, cfg.ModelScopeMaxTokens, false)
		return &ModelRouter{
			Default: modelscope,
			Routes: map[string]Provider{
				cfg.ModelScopeModel: modelscope,
				cfg.OpenAIModel: unavailableProvider{
					name:   "openai",
					reason: "OPENAI_API_KEY and LLM_PROVIDER=openai are required for GLM models",
				},
			},
			PrefixRoutes: map[string]Provider{"Qwen/": modelscope},
		}, nil
	case "mock":
		return NewMock(nil, 0), nil
	default:
		return nil, fmt.Errorf("unknown llm provider: %s", cfg.Provider)
	}
}
