// Package obs 提供日志/trace_id/metrics 三件套的轻量封装。
// W1 仅做 slog + trace_id 贯穿，metrics 是 noop；W9 替换为 OTel + Prometheus。
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey int

const (
	ctxKeyTraceID ctxKey = iota + 1
	ctxKeyRunID
	ctxKeyLogger
)

// InitLogger 配置全局 slog default。
// format: "json" | "text"；level: debug/info/warn/error。
func InitLogger(format, level string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	w := io.Writer(os.Stdout)
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithTraceID 把 trace_id 写入 context。
func WithTraceID(ctx context.Context, tid string) context.Context {
	return context.WithValue(ctx, ctxKeyTraceID, tid)
}

// TraceIDFromContext 取 trace_id；不存在则返回空。
func TraceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTraceID).(string)
	return v
}

// WithRunID 把 run_id 写入 context。
func WithRunID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, ctxKeyRunID, rid)
}

// RunIDFromContext 取 run_id。
func RunIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRunID).(string)
	return v
}

// LoggerFromContext 返回带 trace_id / run_id 字段的 logger。
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKeyLogger).(*slog.Logger); ok && l != nil {
		return l
	}
	logger := slog.Default()
	if tid := TraceIDFromContext(ctx); tid != "" {
		logger = logger.With("trace_id", tid)
	}
	if rid := RunIDFromContext(ctx); rid != "" {
		logger = logger.With("run_id", rid)
	}
	return logger
}

// WithLogger 把 logger 注入 context。
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKeyLogger, l)
}
