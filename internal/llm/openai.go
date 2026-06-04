package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider 调用 OpenAI 兼容 /v1/chat/completions stream=true 接口。
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration

	httpc *http.Client
}

// NewOpenAI 构造 provider。timeout<=0 时默认 60s。
func NewOpenAI(baseURL, apiKey, model string, timeout time.Duration) *OpenAIProvider {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &OpenAIProvider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		Timeout: timeout,
		httpc:   &http.Client{Timeout: timeout},
	}
}

// Name 返回 provider 名。
func (p *OpenAIProvider) Name() string { return "openai" }

type openaiChatReq struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

// Stream 发起请求并返回 token 通道。
func (p *OpenAIProvider) Stream(ctx context.Context, req Req) (<-chan TokenEvent, error) {
	model := req.Model
	if model == "" {
		model = p.Model
	}
	body := openaiChatReq{
		Model:       model,
		Messages:    req.Messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	url := p.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := p.httpc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("openai http %d: %s", resp.StatusCode, string(errBody))
	}

	out := make(chan TokenEvent, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				line = bytes.TrimRight(line, "\r\n")
				if bytes.HasPrefix(line, []byte("data:")) {
					payload := bytes.TrimSpace(line[len("data:"):])
					if len(payload) == 0 {
						continue
					}
					if bytes.Equal(payload, []byte("[DONE]")) {
						send(ctx, out, TokenEvent{Done: true})
						return
					}
					var c openaiStreamChunk
					if jerr := json.Unmarshal(payload, &c); jerr != nil {
						// 跳过非 JSON keepalive
						continue
					}
					for _, ch := range c.Choices {
						if ch.Delta.Content != "" {
							if !send(ctx, out, TokenEvent{Token: ch.Delta.Content}) {
								return
							}
						}
						if ch.FinishReason != nil && *ch.FinishReason != "" {
							send(ctx, out, TokenEvent{Done: true})
							return
						}
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					send(ctx, out, TokenEvent{Err: fmt.Errorf("sse read: %w", err)})
				} else {
					send(ctx, out, TokenEvent{Done: true})
				}
				return
			}
		}
	}()
	return out, nil
}

func send(ctx context.Context, ch chan<- TokenEvent, ev TokenEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- ev:
		return true
	}
}
