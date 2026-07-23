package llm

import (
	"fmt"
	"strings"
	"time"
)

// FactoryConfig 构造 Provider 所需的所有配置。
type FactoryConfig struct {
	Provider        string // openai | mock
	OpenAIBaseURL   string
	OpenAIAPIKey    string
	OpenAIModel     string
	OpenAIMaxTokens int
	ThinkingEnabled bool
	OpenAITimeout   time.Duration
}

// NewFromConfig 按 cfg 选择 provider。
func NewFromConfig(cfg FactoryConfig) (Provider, error) {
	switch strings.ToLower(cfg.Provider) {
	case "", "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("openai api key empty")
		}
		return NewOpenAIWithOptions(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAITimeout, cfg.OpenAIMaxTokens, cfg.ThinkingEnabled), nil
	case "mock":
		return NewMock(nil, 0), nil
	default:
		return nil, fmt.Errorf("unknown llm provider: %s", cfg.Provider)
	}
}
