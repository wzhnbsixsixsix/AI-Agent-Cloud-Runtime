package acp

import "encoding/json"

// Hello 客户端首帧 payload（JSON）。
type Hello struct {
	ClientVersion string `json:"client_version"`
	UserID        string `json:"user_id,omitempty"`
	// RunID 非空表示客户端希望续接已有 run（配合 RESUME 帧使用）。
	RunID string `json:"run_id,omitempty"`
}

// HelloAck 服务端响应。
type HelloAck struct {
	ServerVersion string `json:"server_version"`
	RunID         string `json:"run_id"`
	TraceID       string `json:"trace_id"`
	// MaxFrameSize 服务端能接受的最大 payload，便于客户端切片。
	MaxFrameSize int `json:"max_frame_size"`
}

// Resume 续传帧 payload。
type Resume struct {
	RunID   string `json:"run_id"`
	LastSeq uint64 `json:"last_seq"`
}

// Close 关连接帧。
type Close struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// ErrorPayload 业务错误帧 payload（与 protobuf Error 对齐字段）。
type ErrorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retriable bool   `json:"retriable,omitempty"`
}

// MarshalJSONPayload 是控制帧 payload 编码的统一入口。
func MarshalJSONPayload(v any) ([]byte, error) { return json.Marshal(v) }

// UnmarshalJSONPayload 解析控制帧 payload。
func UnmarshalJSONPayload(data []byte, v any) error { return json.Unmarshal(data, v) }
