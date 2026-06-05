package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
)

// HTTPFetchTool 在宿主机（worker 进程）网络上取一个 HTTP(S) URL。
//
// 设计取舍：sandbox 自身 net=none 拒绝任何流量，所以 http_fetch 故意不走 sandbox.Exec，
// 而是在 worker 进程里直接 net/http。这样既保留 sandbox 强隔离，又能让 Agent 拿到外部数据。
// 真要做"沙箱内代理 + 流量审计"是 W8/W9 的事。
type HTTPFetchTool struct {
	AllowList []string // host 白名单（精确，或前缀 . 表示后缀匹配 .example.com）；空=全允许
	MaxBytes  int64    // 默认 1 MiB
	Client    *http.Client
}

const httpFetchSchema = `{
  "type":"object",
  "properties":{
    "url":{"type":"string","description":"http(s) URL"},
    "method":{"type":"string","enum":["GET","HEAD","POST"],"default":"GET"},
    "headers":{"type":"object","additionalProperties":{"type":"string"}},
    "body":{"type":"string","description":"only used when method=POST"},
    "timeout_ms":{"type":"integer","minimum":1,"maximum":30000,"default":10000}
  },
  "required":["url"]
}`

func (t *HTTPFetchTool) Descriptor() Descriptor {
	return Descriptor{
		Name: "http_fetch",
		Description: "Fetch an HTTP(S) URL using the worker's host network. " +
			"Sandbox has no network of its own, so this is the only way for an Agent to reach the internet. " +
			"Body is capped at MaxBytes (default 1MiB) and may be truncated.",
		Schema: json.RawMessage(httpFetchSchema),
	}
}

func (t *HTTPFetchTool) Invoke(ctx context.Context, _ sandbox.Sandbox, args json.RawMessage) (Result, error) {
	var a struct {
		URL       string            `json:"url"`
		Method    string            `json:"method"`
		Headers   map[string]string `json:"headers"`
		Body      string            `json:"body"`
		TimeoutMS int               `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{}, fmt.Errorf("bad args: %w", err)
	}
	if a.URL == "" {
		return Result{}, errors.New("url required")
	}
	if a.Method == "" {
		a.Method = "GET"
	}

	u, err := url.Parse(a.URL)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Result{Content: "only http(s) supported", IsError: true}, nil
	}
	if !t.allowedHost(u.Host) {
		return Result{Content: fmt.Sprintf("host %q not in allowlist", u.Host), IsError: true}, nil
	}

	timeout := time.Duration(a.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cli := t.Client
	if cli == nil {
		cli = &http.Client{Timeout: timeout}
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if a.Body != "" {
		body = strings.NewReader(a.Body)
	}
	req, err := http.NewRequestWithContext(rctx, a.Method, a.URL, body)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	defer resp.Body.Close()

	maxN := t.MaxBytes
	if maxN <= 0 {
		maxN = 1 * 1024 * 1024
	}
	rd := io.LimitReader(resp.Body, maxN+1)
	buf, err := io.ReadAll(rd)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	truncated := int64(len(buf)) > maxN
	if truncated {
		buf = buf[:maxN]
	}
	return Result{
		Content: string(buf),
		IsError: resp.StatusCode >= 400,
		Metadata: map[string]any{
			"status":       resp.StatusCode,
			"content_type": resp.Header.Get("Content-Type"),
			"bytes":        len(buf),
			"truncated":    truncated,
		},
	}, nil
}

// allowedHost 支持精确 + 前导点后缀匹配。
//
//	allow=["example.com"]   → "example.com" ✓, "api.example.com" ✗
//	allow=[".example.com"]  → "example.com" ✓, "api.example.com" ✓
func (t *HTTPFetchTool) allowedHost(host string) bool {
	if len(t.AllowList) == 0 {
		return true
	}
	h := strings.ToLower(strings.SplitN(host, ":", 2)[0])
	for _, a := range t.AllowList {
		a = strings.ToLower(a)
		if a == h {
			return true
		}
		if strings.HasPrefix(a, ".") {
			suffix := a[1:]
			if h == suffix || strings.HasSuffix(h, a) {
				return true
			}
		}
	}
	return false
}
