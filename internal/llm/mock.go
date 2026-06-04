package llm

import (
	"context"
	"strings"
	"time"
)

// MockProvider 不调外部，吐固定串作为流式输出。
// 默认 5 个 token，每 30ms 一个。
type MockProvider struct {
	Tokens   []string
	Interval time.Duration
}

// NewMock 构造 mock provider；tokens 为空则使用默认。
func NewMock(tokens []string, interval time.Duration) *MockProvider {
	if len(tokens) == 0 {
		tokens = []string{"Hello", ", ", "I am ", "AgentForge", "."}
	}
	if interval <= 0 {
		interval = 30 * time.Millisecond
	}
	return &MockProvider{Tokens: tokens, Interval: interval}
}

// Name returns provider name.
func (m *MockProvider) Name() string { return "mock" }

// Stream emits configured tokens one by one.
func (m *MockProvider) Stream(ctx context.Context, req Req) (<-chan TokenEvent, error) {
	out := make(chan TokenEvent, len(m.Tokens)+1)
	go func() {
		defer close(out)
		// 让 prompt 出现在第一个 token 里，方便调试可观测
		preview := ""
		if len(req.Messages) > 0 {
			preview = strings.TrimSpace(req.Messages[len(req.Messages)-1].Content)
			if len(preview) > 32 {
				preview = preview[:32] + "..."
			}
		}
		if preview != "" {
			if !send(ctx, out, TokenEvent{Token: "[mock recv: " + preview + "] "}) {
				return
			}
		}
		for _, tk := range m.Tokens {
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.Interval):
			}
			if !send(ctx, out, TokenEvent{Token: tk}) {
				return
			}
		}
		send(ctx, out, TokenEvent{Done: true})
	}()
	return out, nil
}
