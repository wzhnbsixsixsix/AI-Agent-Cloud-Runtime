package acp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/wzhnbsixsixsix/agentforge/pkg/acp"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/protobuf/proto"
)

// session 单连接会话。
//   - serve: 主循环；负责 HELLO -> RUN -> EVENT 流 -> CLOSE
//   - 写帧统一走 sendCh + writer goroutine，避免业务 goroutine 与心跳并发写 conn。
type session struct {
	conn   net.Conn
	cfg    ServerConfig
	log    *slog.Logger
	disp   Dispatcher
	cache  *EventCache
	idGen  IDGenerator
	br     *bufio.Reader

	sendCh   chan acp.Frame
	sendDone chan struct{}
	closed   atomic.Bool

	// 业务层赋值
	runID, traceID string
	seqGen         uint64
}

func newSession(c net.Conn, cfg ServerConfig, log *slog.Logger, d Dispatcher, ec *EventCache, idGen IDGenerator) *session {
	return &session{
		conn:     c,
		cfg:      cfg,
		log:      log,
		disp:     d,
		cache:    ec,
		idGen:    idGen,
		br:       bufio.NewReaderSize(c, 32*1024),
		sendCh:   make(chan acp.Frame, 256),
		sendDone: make(chan struct{}),
	}
}

// serve 阻塞执行整个会话直到结束。
func (s *session) serve(ctx context.Context) error {
	// writer goroutine
	go s.writerLoop()
	defer func() {
		// 关闭 sendCh，writer goroutine 退出
		if !s.closed.Swap(true) {
			close(s.sendCh)
		}
		<-s.sendDone
	}()

	// 1. 读 HELLO
	helloFrame, err := s.readFrame()
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if helloFrame.Type != acp.FrameHello {
		return fmt.Errorf("expect HELLO, got %s", helloFrame.Type)
	}
	var hello acp.Hello
	if err := acp.UnmarshalJSONPayload(helloFrame.Payload, &hello); err != nil {
		return fmt.Errorf("decode hello: %w", err)
	}

	// 2. 分配/沿用 run_id
	if hello.RunID != "" {
		s.runID = hello.RunID
	} else {
		s.runID = s.idGen.NewRunID()
	}
	s.traceID = s.idGen.NewTraceID()

	// 3. HELLO_ACK
	ackPayload, _ := acp.MarshalJSONPayload(acp.HelloAck{
		ServerVersion: "agentforge-1.0",
		RunID:         s.runID,
		TraceID:       s.traceID,
		MaxFrameSize:  acp.MaxPayloadSize,
	})
	s.send(acp.Frame{Type: acp.FrameHelloAck, Payload: ackPayload})

	// 4. 主循环：读下一帧并按类型分派
	//    - RUN：启动业务执行
	//    - RESUME：从缓存回放
	//    - PING：回 PONG
	for {
		f, err := s.readFrame()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		switch f.Type {
		case acp.FramePing:
			s.send(acp.Frame{Type: acp.FramePong, Payload: f.Payload})
		case acp.FrameClose:
			return nil
		case acp.FrameRun:
			if err := s.handleRun(ctx, f.Payload); err != nil {
				return err
			}
			return nil
		case acp.FrameResume:
			if err := s.handleResume(ctx, f.Payload); err != nil {
				return err
			}
			return nil
		default:
			s.log.Warn("acp unexpected frame", "type", f.Type, "remote", s.conn.RemoteAddr().String())
		}
	}
}

func (s *session) handleRun(ctx context.Context, payload []byte) error {
	var req pb.RunRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return s.sendError("bad_request", "decode RunRequest: "+err.Error(), false)
	}
	if req.GetPrompt() == "" {
		return s.sendError("bad_request", "empty prompt", false)
	}
	// run_id 沿用 HELLO 阶段确定的，traceID 同
	in := RunInput{
		RunID:   s.runID,
		TraceID: s.traceID,
		UserID:  req.GetUserId(),
		Prompt:  req.GetPrompt(),
		Model:   req.GetModel(),
	}

	emit := s.makeEmit(ctx)
	err := s.disp.Run(ctx, in, emit)
	if err != nil {
		// 业务侧异常，发 ERROR + END_STREAM
		_ = s.sendError("internal", err.Error(), true)
	}
	// 结束后给客户端一个 CLOSE
	cp, _ := acp.MarshalJSONPayload(acp.Close{Code: "completed"})
	s.send(acp.Frame{Type: acp.FrameClose, Flags: acp.FlagEndStream, Payload: cp})
	return nil
}

func (s *session) handleResume(ctx context.Context, payload []byte) error {
	var r acp.Resume
	if err := acp.UnmarshalJSONPayload(payload, &r); err != nil {
		return s.sendError("bad_request", "decode Resume: "+err.Error(), false)
	}
	if r.RunID == "" {
		return s.sendError("bad_request", "empty run_id", false)
	}
	if s.cache == nil {
		return s.sendError("not_supported", "event cache disabled", false)
	}
	events, err := s.cache.Replay(ctx, r.RunID, r.LastSeq)
	if err != nil {
		return s.sendError("internal", "replay: "+err.Error(), true)
	}
	for _, ev := range events {
		s.send(acp.Frame{Type: acp.FrameEvent, Seq: ev.Seq, Payload: ev.Payload})
		// 维持本地 seqGen 单调
		if ev.Seq > atomic.LoadUint64(&s.seqGen) {
			atomic.StoreUint64(&s.seqGen, ev.Seq)
		}
	}
	// W2 简化：回放完毕即认为 run 已落盘，发 CLOSE 收尾。
	cp, _ := acp.MarshalJSONPayload(acp.Close{Code: "resumed"})
	s.send(acp.Frame{Type: acp.FrameClose, Flags: acp.FlagEndStream, Payload: cp})
	return nil
}

// makeEmit 把业务事件编码成 ACP EVENT 帧并写出，同步落缓存。
func (s *session) makeEmit(ctx context.Context) EmitFunc {
	var emitMu sync.Mutex
	return func(ev BusinessEvent) error {
		emitMu.Lock()
		defer emitMu.Unlock()
		if s.closed.Load() {
			return errors.New("session closed")
		}
		evPB := businessToProto(s.runID, s.traceID, ev)
		raw, err := proto.Marshal(evPB)
		if err != nil {
			return err
		}
		seq := atomic.AddUint64(&s.seqGen, 1)
		flags := acp.Flags(0)
		if ev.Kind == "done" || ev.Kind == "error" {
			flags |= acp.FlagEndStream
		}
		// 先落缓存（耐久），再发出去（即使发送失败客户端重连也能补到）
		if s.cache != nil {
			_ = s.cache.Append(ctx, s.runID, seq, raw)
		}
		s.send(acp.Frame{Type: acp.FrameEvent, Flags: flags, Seq: seq, Payload: raw})
		return nil
	}
}

func businessToProto(runID, traceID string, ev BusinessEvent) *pb.RunEvent {
	out := &pb.RunEvent{RunId: runID, TraceId: traceID}
	switch ev.Kind {
	case "token":
		out.Payload = &pb.RunEvent_Token{Token: &pb.Token{Text: ev.Text, Index: ev.Index}}
	case "state":
		out.Payload = &pb.RunEvent_StateChanged{StateChanged: &pb.StateChanged{
			From: stateEnumStr(ev.From), To: stateEnumStr(ev.State), TsUnixMs: time.Now().UnixMilli(),
		}}
	case "done":
		out.Payload = &pb.RunEvent_Done{Done: &pb.Done{TotalTokens: ev.Total, TsUnixMs: time.Now().UnixMilli()}}
	case "error":
		out.Payload = &pb.RunEvent_Error{Error: &pb.Error{Code: ev.Code, Message: ev.Message}}
	}
	return out
}

func stateEnumStr(s string) pb.RunState {
	switch s {
	case "PENDING":
		return pb.RunState_RUN_STATE_PENDING
	case "RUNNING":
		return pb.RunState_RUN_STATE_RUNNING
	case "WAITING_TOOL":
		return pb.RunState_RUN_STATE_WAITING_TOOL
	case "COMPACTING":
		return pb.RunState_RUN_STATE_COMPACTING
	case "DONE":
		return pb.RunState_RUN_STATE_DONE
	case "FAILED":
		return pb.RunState_RUN_STATE_FAILED
	}
	return pb.RunState_RUN_STATE_UNSPECIFIED
}

func (s *session) sendError(code, msg string, retriable bool) error {
	payload, _ := acp.MarshalJSONPayload(acp.ErrorPayload{Code: code, Message: msg, Retriable: retriable})
	s.send(acp.Frame{Type: acp.FrameError, Flags: acp.FlagEndStream, Payload: payload})
	return nil
}

func (s *session) send(f acp.Frame) {
	if s.closed.Load() {
		return
	}
	select {
	case s.sendCh <- f:
	default:
		// 队列满，杀连接（背压策略：防止内存爆）
		s.log.Warn("acp send queue full, drop conn", "remote", s.conn.RemoteAddr().String())
		s.closed.Store(true)
		_ = s.conn.Close()
	}
}

func (s *session) writerLoop() {
	defer close(s.sendDone)
	for f := range s.sendCh {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
		if err := acp.WriteFrame(s.conn, f); err != nil {
			s.log.Debug("acp write err", "err", err)
			_ = s.conn.Close()
			// 排空剩余
			for range s.sendCh {
			}
			return
		}
	}
}

func (s *session) readFrame() (acp.Frame, error) {
	_ = s.conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
	return acp.ReadFrame(s.br)
}
