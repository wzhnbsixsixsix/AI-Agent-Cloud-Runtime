package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Dispatcher 把 ACP RUN 帧分派给业务层（gateway）执行，
// 同时把业务事件流回调给 ACP 会话。
//
// 设计上把"业务"与"协议"解耦：ACP 包不依赖 internal/queue / internal/agent。
type Dispatcher interface {
	// Run 启动一次 agent 任务。
	//   - runID/traceID: 由 ACP 会话生成，业务层应直接使用
	//   - prompt/userID/model: 来自客户端 RUN 帧
	//   - emit: 业务层每产生一条事件，调一次 emit；emit 返回 error 表示连接已断，业务可提前停。
	//
	// 当 agent 任务结束（DONE / ERROR / ctx 取消）时，Run 必须返回。
	Run(ctx context.Context, req RunInput, emit EmitFunc) error
}

// RunInput 业务输入。
type RunInput struct {
	RunID   string
	TraceID string
	UserID  string
	Prompt  string
	Model   string
}

// BusinessEvent 业务侧事件（与 internal/queue.Event 字段对齐，但不依赖该包）。
type BusinessEvent struct {
	Kind    string // "state" | "token" | "done" | "error"
	State   string
	From    string
	Text    string
	Index   int64
	Total   int64
	Code    string
	Message string
}

// EmitFunc 业务层向 ACP 推送一条事件；返回 error 表示 ACP 已无法发送（客户端断开 / 连接关闭）。
type EmitFunc func(ev BusinessEvent) error

// ServerConfig ACP server 启动参数。
type ServerConfig struct {
	Addr            string
	ReadTimeout     time.Duration // 单帧读超时（含心跳）
	WriteTimeout    time.Duration
	MaxConnections  int
	ShutdownTimeout time.Duration
}

// Defaults 填充缺省值。
func (c *ServerConfig) Defaults() {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 1024
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 5 * time.Second
	}
}

// Server ACP TCP 服务端。
type Server struct {
	cfg    ServerConfig
	disp   Dispatcher
	cache  *EventCache
	log    *slog.Logger
	idGen  IDGenerator
	listen net.Listener

	wg     sync.WaitGroup
	connWg sync.WaitGroup
	sem    chan struct{}
	closed chan struct{}
}

// IDGenerator 用于生成 run_id / trace_id；外部传入避免依赖 obs。
type IDGenerator interface {
	NewRunID() string
	NewTraceID() string
}

// NewServer 构造 ACP server。
func NewServer(cfg ServerConfig, disp Dispatcher, cache *EventCache, log *slog.Logger, idGen IDGenerator) *Server {
	cfg.Defaults()
	return &Server{
		cfg:    cfg,
		disp:   disp,
		cache:  cache,
		log:    log,
		idGen:  idGen,
		sem:    make(chan struct{}, cfg.MaxConnections),
		closed: make(chan struct{}),
	}
}

// ListenAndServe 监听并阻塞处理连接，直到 ctx 取消。
func (s *Server) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{KeepAlive: 30 * time.Second}
	ln, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("acp listen %s: %w", s.cfg.Addr, err)
	}
	s.listen = ln
	s.log.Info("acp serving", "addr", s.cfg.Addr)

	// shutdown goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = ln.Close()
		close(s.closed)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				// 正常关停
				s.gracefulWait()
				s.wg.Wait()
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return fmt.Errorf("accept: %w", err)
		}

		select {
		case s.sem <- struct{}{}:
		default:
			s.log.Warn("acp too many connections, drop", "remote", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		s.connWg.Add(1)
		go func(c net.Conn) {
			defer func() {
				<-s.sem
				s.connWg.Done()
			}()
			s.handleConn(ctx, c)
		}(conn)
	}
}

func (s *Server) gracefulWait() {
	done := make(chan struct{})
	go func() {
		s.connWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(s.cfg.ShutdownTimeout):
		s.log.Warn("acp shutdown timeout, some connections may be cut")
	}
}

// handleConn 处理单个连接的整个生命周期。
func (s *Server) handleConn(parent context.Context, conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	sess := newSession(conn, s.cfg, s.log, s.disp, s.cache, s.idGen)
	if err := sess.serve(ctx); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		s.log.Warn("acp session ended", "remote", conn.RemoteAddr().String(), "err", err)
	}
}
