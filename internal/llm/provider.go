// Package llm 抽象 LLM provider，支持流式 token 输出。
// W1 实现 OpenAI 兼容 + Mock；后续可加 Anthropic / 本地模型。
package llm

import (
	"context"
	"encoding/json"
)

// Role LLM 消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 一条对话消息。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDefinition 描述一次 LLM 可调用的 tool。
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall 是 LLM 流式返回的一次 function call。
type ToolCall struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Req 一次 chat completion 请求。
type Req struct {
	Model    string
	Messages []Message
	Tools    []ToolDefinition
	// 推理 / 采样参数（W1 可缺省）
	Temperature float32
	MaxTokens   int
}

// StopReason 描述一次流结束的原因。
type StopReason string

const (
	StopReasonStop      StopReason = "stop"
	StopReasonToolCalls StopReason = "tool_calls"
)

// TokenEvent 流式输出的一帧。
//   Token 非空 -> 一个增量 token（或一段）
//   ToolCalls 非空 -> 模型请求调用 tool
//   Done=true -> 结束信号
//   Err 非空  -> 终止错误
type TokenEvent struct {
	Token      string
	ToolCalls  []ToolCall
	Done       bool
	StopReason StopReason
	Err        error
}

// Provider LLM 提供方接口。
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Req) (<-chan TokenEvent, error)
}
