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
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Tools       []openaiToolDef `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	Temperature float32         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openaiMessage struct {
	Role       Role             `json:"role"`
	Content    string           `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
}

type openaiToolDef struct {
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openaiToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openaiToolCallFunction `json:"function"`
}

type openaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

type toolCallBuilder struct {
	ID        string
	Type      string
	Name      string
	Arguments strings.Builder
}

// Stream 发起请求并返回 token 通道。
func (p *OpenAIProvider) Stream(ctx context.Context, req Req) (<-chan TokenEvent, error) {
	model := req.Model
	if model == "" {
		model = p.Model
	}
	body := openaiChatReq{
		Model:       model,
		Messages:    toOpenAIMessages(req.Messages),
		Tools:       toOpenAITools(req.Tools),
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
		builders := map[int]*toolCallBuilder{}
		order := make([]int, 0, 4)
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
						for _, tc := range ch.Delta.ToolCalls {
							b := builders[tc.Index]
							if b == nil {
								b = &toolCallBuilder{}
								builders[tc.Index] = b
								order = append(order, tc.Index)
							}
							if tc.ID != "" {
								b.ID = tc.ID
							}
							if tc.Type != "" {
								b.Type = tc.Type
							}
							if tc.Function.Name != "" {
								b.Name = tc.Function.Name
							}
							if tc.Function.Arguments != "" {
								b.Arguments.WriteString(tc.Function.Arguments)
							}
						}
						if ch.FinishReason != nil && *ch.FinishReason != "" {
							reason := StopReason(*ch.FinishReason)
							if reason == StopReasonToolCalls && len(builders) > 0 {
								if !send(ctx, out, TokenEvent{
									ToolCalls:  builtToolCalls(order, builders),
									StopReason: StopReasonToolCalls,
								}) {
									return
								}
							}
							send(ctx, out, TokenEvent{Done: true, StopReason: reason})
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

func toOpenAIMessages(in []Message) []openaiMessage {
	out := make([]openaiMessage, 0, len(in))
	for _, m := range in {
		om := openaiMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]openaiToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				typ := tc.Type
				if typ == "" {
					typ = "function"
				}
				om.ToolCalls = append(om.ToolCalls, openaiToolCall{
					ID:   tc.ID,
					Type: typ,
					Function: openaiToolCallFunction{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		out = append(out, om)
	}
	return out
}

func toOpenAITools(in []ToolDefinition) []openaiToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]openaiToolDef, 0, len(in))
	for _, t := range in {
		out = append(out, openaiToolDef{
			Type: "function",
			Function: openaiToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

func builtToolCalls(order []int, builders map[int]*toolCallBuilder) []ToolCall {
	out := make([]ToolCall, 0, len(order))
	for _, idx := range order {
		b := builders[idx]
		if b == nil {
			continue
		}
		typ := b.Type
		if typ == "" {
			typ = "function"
		}
		out = append(out, ToolCall{
			ID:        b.ID,
			Type:      typ,
			Name:      b.Name,
			Arguments: b.Arguments.String(),
		})
	}
	return out
}

func send(ctx context.Context, ch chan<- TokenEvent, ev TokenEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- ev:
		return true
	}
}
