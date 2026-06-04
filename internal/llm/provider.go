// Package llm 抽象 LLM provider，支持流式 token 输出。
// W1 实现 OpenAI 兼容 + Mock；后续可加 Anthropic / 本地模型。
package llm

import "context"

// Role LLM 消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message 一条对话消息。
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Req 一次 chat completion 请求。
type Req struct {
	Model    string
	Messages []Message
	// 推理 / 采样参数（W1 可缺省）
	Temperature float32
	MaxTokens   int
}

// TokenEvent 流式输出的一帧。
//   Token 非空 -> 一个增量 token（或一段）
//   Done=true -> 结束信号
//   Err 非空  -> 终止错误
type TokenEvent struct {
	Token string
	Done  bool
	Err   error
}

// Provider LLM 提供方接口。
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Req) (<-chan TokenEvent, error)
}
