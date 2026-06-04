// Package history 提供"可变事件流式"消息存储抽象。
// W1 仅实现 Append/Patch/Hide/Render；Fold 留到 W4。
package history

import "context"

// Role 消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 是历史里的最小单元。
type Message struct {
	ID       string            `json:"id"`        // ULID
	Role     Role              `json:"role"`
	Content  string            `json:"content"`
	Visible  bool              `json:"visible"`
	Version  uint32            `json:"version"`
	ParentID string            `json:"parent_id,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// Store 是历史存储的能力契约。
type Store interface {
	// Append 追加一条消息，返回 message id（如果入参 ID 为空，由实现生成 ULID）。
	Append(ctx context.Context, runID string, m Message) (string, error)
	// Patch 原地改写 content。
	Patch(ctx context.Context, runID, msgID, content string) error
	// Hide 软删（Visible=false）。
	Hide(ctx context.Context, runID, msgID string) error
	// Render 返回当前 visible 消息按时间顺序的视图。
	Render(ctx context.Context, runID string) ([]Message, error)
}
