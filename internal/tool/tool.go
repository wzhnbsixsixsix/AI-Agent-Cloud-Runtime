// Package tool 提供 Agent 可调用的工具集。
//
// W3 阶段实现 5 个内置 tool（bash / fs_read / fs_write / fs_list / http_fetch），
// 设计上完全兼容 OpenAI/Anthropic 的 function-calling Schema，W4 接 LLM 时可直接复用。
//
// 边界：
//   - bash/fs_* 需要 sandbox.Sandbox（Exec 进入隔离容器）
//   - http_fetch 故意走宿主机网络（sandbox 自身 net=none），避免在容器里再做代理
//   - 所有工具的 IsError=true 表示业务失败（exit_code!=0、HTTP 4xx/5xx），
//     不直接返回 Go error；Go error 仅用于真正的内部异常（参数解析失败、driver 崩等）
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
)

// Result 是一个 tool 执行结果。
type Result struct {
	Content  string         `json:"content"`
	IsError  bool           `json:"is_error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Descriptor 给 OpenAI/Anthropic function-calling 用，W4 直接 inline 进 system prompt。
type Descriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"parameters"` // JSON Schema (Draft 7)
}

// Tool 抽象。每个内置 tool 单独实现。
type Tool interface {
	Descriptor() Descriptor
	// Invoke 执行 tool。
	//
	// 约定：
	//   - 业务失败（命令非 0 退出、HTTP 4xx/5xx 等）→ Result{IsError:true}, err=nil
	//   - 真正的内部异常（参数 JSON 错、sandbox driver 故障等）→ err!=nil
	Invoke(ctx context.Context, sb sandbox.Sandbox, args json.RawMessage) (Result, error)
}

// Registry 存全部已注册 tools，按名字查。
type Registry struct {
	mu sync.RWMutex
	m  map[string]Tool
}

func NewRegistry() *Registry { return &Registry{m: map[string]Tool{}} }

// Register 注册一个 tool；重名报错。
func (r *Registry) Register(t Tool) error {
	d := t.Descriptor()
	if d.Name == "" {
		return errors.New("tool: empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.m[d.Name]; dup {
		return fmt.Errorf("tool: duplicate %q", d.Name)
	}
	r.m[d.Name] = t
	return nil
}

// Get 取 tool；ok=false 表示未注册。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.m[name]
	return t, ok
}

// List 返回所有 Descriptor，按 Name 升序。
func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ds := make([]Descriptor, 0, len(r.m))
	for _, t := range r.m {
		ds = append(ds, t.Descriptor())
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })
	return ds
}

// BuiltinsConfig 控制内置 tool 的行为。
type BuiltinsConfig struct {
	HTTPAllowList []string // host 白名单；空切片=全允许
	HTTPMaxBytes  int64    // http_fetch 单次返回最大字节，默认 1MiB
}

// Builtins 一次性注册全部 5 个内置 tool。
func Builtins(cfg BuiltinsConfig) *Registry {
	r := NewRegistry()
	_ = r.Register(&BashTool{})
	_ = r.Register(&FsReadTool{})
	_ = r.Register(&FsWriteTool{})
	_ = r.Register(&FsListTool{})
	_ = r.Register(&HTTPFetchTool{
		AllowList: cfg.HTTPAllowList,
		MaxBytes:  cfg.HTTPMaxBytes,
	})
	return r
}
