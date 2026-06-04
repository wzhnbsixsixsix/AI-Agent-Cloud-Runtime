package acp

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
)

// stubIDGen 测试用 ID 生成器。
type stubIDGen struct {
	runSeq, traceSeq uint64
}

func (g *stubIDGen) NewRunID() string {
	n := atomic.AddUint64(&g.runSeq, 1)
	return "run-" + itoa(n)
}
func (g *stubIDGen) NewTraceID() string {
	n := atomic.AddUint64(&g.traceSeq, 1)
	return "trace-" + itoa(n)
}

func itoa(n uint64) string {
	buf := make([]byte, 0, 20)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// echoDispatcher 把 prompt 拆成 token 流回。
type echoDispatcher struct {
	tokens []string
}

func (e *echoDispatcher) Run(ctx context.Context, in RunInput, emit EmitFunc) error {
	if err := emit(BusinessEvent{Kind: "state", From: "PENDING", State: "RUNNING"}); err != nil {
		return err
	}
	tokens := e.tokens
	if len(tokens) == 0 {
		tokens = []string{"hello", " ", "world"}
	}
	for i, t := range tokens {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := emit(BusinessEvent{Kind: "token", Text: t, Index: int64(i)}); err != nil {
			return err
		}
	}
	return emit(BusinessEvent{Kind: "done", Total: int64(len(tokens))})
}

// startTestServer 启动 ACP server 监听随机端口。
func startTestServer(t *testing.T, disp Dispatcher) (string, *EventCache, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := NewEventCache(rdb, time.Hour)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := NewServer(ServerConfig{Addr: addr, ReadTimeout: 5 * time.Second}, disp, cache, slog.Default(), &stubIDGen{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = srv.ListenAndServe(ctx)
		close(done)
	}()
	// 等监听就绪
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cleanup := func() {
		cancel()
		<-done
		mr.Close()
	}
	return addr, cache, cleanup
}

func TestACP_HappyPath(t *testing.T) {
	addr, _, stop := startTestServer(t, &echoDispatcher{tokens: []string{"a", "b", "c"}})
	defer stop()

	cli, err := Dial(context.Background(), DialOptions{Addr: addr, UserID: "alice"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()

	if cli.RunID() == "" || cli.TraceID() == "" {
		t.Fatalf("missing ids: %s/%s", cli.RunID(), cli.TraceID())
	}

	if err := cli.SendRun(&pb.RunRequest{Prompt: "ignored", UserId: "alice"}); err != nil {
		t.Fatalf("send run: %v", err)
	}

	var tokens []string
	var done bool
	for {
		ev, end, err := cli.NextEvent()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("next: %v", err)
		}
		if ev == nil {
			break
		}
		switch p := ev.Payload.(type) {
		case *pb.RunEvent_Token:
			tokens = append(tokens, p.Token.Text)
		case *pb.RunEvent_Done:
			done = true
		case *pb.RunEvent_Error:
			t.Fatalf("err event: %s", p.Error.Message)
		}
		if end {
			break
		}
	}
	if !done {
		t.Fatalf("did not see done event")
	}
	if got := tokens; len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("tokens mismatch: %v", got)
	}
}

func TestACP_PingPong(t *testing.T) {
	addr, _, stop := startTestServer(t, &echoDispatcher{})
	defer stop()
	cli, err := Dial(context.Background(), DialOptions{Addr: addr})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	for i := 0; i < 3; i++ {
		rtt, err := cli.Ping(context.Background())
		if err != nil {
			t.Fatalf("ping: %v", err)
		}
		if rtt <= 0 || rtt > 2*time.Second {
			t.Fatalf("bad rtt %s", rtt)
		}
	}
}

func TestACP_ResumeFromCache(t *testing.T) {
	// 先跑一次 run 把事件落到 cache，再用同 run_id 发 RESUME 取出回放。
	addr, cache, stop := startTestServer(t, &echoDispatcher{tokens: []string{"x", "y"}})
	defer stop()

	// 第一阶段：完整跑一次
	cli, err := Dial(context.Background(), DialOptions{Addr: addr})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	runID := cli.RunID()
	if err := cli.SendRun(&pb.RunRequest{Prompt: "p"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	for {
		_, end, err := cli.NextEvent()
		if err != nil || end {
			break
		}
	}
	_ = cli.Close()

	// 验证 cache 里有数据
	evs, err := cache.Replay(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(evs) < 3 {
		t.Fatalf("expect cached events >=3, got %d", len(evs))
	}

	// 第二阶段：新连接，发 RESUME，希望收到 seq>1 的全部事件
	cli2, err := Dial(context.Background(), DialOptions{Addr: addr})
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer cli2.Close()
	if err := cli2.SendResume(runID, 1); err != nil {
		t.Fatalf("send resume: %v", err)
	}
	got := 0
	for {
		ev, end, err := cli2.NextEvent()
		if err != nil {
			break
		}
		if ev != nil {
			got++
		}
		if end {
			break
		}
	}
	// 至少应回放 (cached_count - 1) 条（从 seq>1 起）
	if got < len(evs)-1 {
		t.Fatalf("resume got %d events, want >= %d", got, len(evs)-1)
	}
}
