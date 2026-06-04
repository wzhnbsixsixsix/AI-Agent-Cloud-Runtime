package acp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	acp "github.com/wzhnbsixsixsix/agentforge/pkg/acp"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/protobuf/proto"
)

// Client 是 ACP 客户端，agentctl 与 cmd/bench 共用。
type Client struct {
	conn         net.Conn
	br           *bufio.Reader
	writeTimeout time.Duration
	readTimeout  time.Duration

	runID   string
	traceID string

	pingNonce uint64
}

// DialOptions 客户端连接参数。
type DialOptions struct {
	Addr         string
	UserID       string
	DialTimeout  time.Duration
	WriteTimeout time.Duration
	ReadTimeout  time.Duration
}

// Dial 建立连接 + 完成 HELLO/HELLO_ACK 握手。
func Dial(ctx context.Context, opts DialOptions) (*Client, error) {
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 5 * time.Second
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = 10 * time.Second
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = 30 * time.Second
	}
	d := net.Dialer{Timeout: opts.DialTimeout, KeepAlive: 30 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.Addr, err)
	}
	c := &Client{
		conn:         conn,
		br:           bufio.NewReaderSize(conn, 32*1024),
		writeTimeout: opts.WriteTimeout,
		readTimeout:  opts.ReadTimeout,
	}
	helloPayload, _ := acp.MarshalJSONPayload(acp.Hello{
		ClientVersion: "agentforge-1.0",
		UserID:        opts.UserID,
	})
	if err := c.writeFrame(acp.Frame{Type: acp.FrameHello, Payload: helloPayload}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	ack, err := c.readFrame()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ack.Type != acp.FrameHelloAck {
		_ = conn.Close()
		return nil, fmt.Errorf("expect HELLO_ACK got %s", ack.Type)
	}
	var ackBody acp.HelloAck
	if err := acp.UnmarshalJSONPayload(ack.Payload, &ackBody); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("decode hello_ack: %w", err)
	}
	c.runID = ackBody.RunID
	c.traceID = ackBody.TraceID
	return c, nil
}

// RunID 由服务端分配的 run_id。
func (c *Client) RunID() string { return c.runID }

// TraceID 服务端分配的 trace_id。
func (c *Client) TraceID() string { return c.traceID }

// SendRun 发送 RUN 帧。
func (c *Client) SendRun(req *pb.RunRequest) error {
	if req.GetRunId() == "" {
		req.RunId = c.runID
	}
	if req.GetTraceId() == "" {
		req.TraceId = c.traceID
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	return c.writeFrame(acp.Frame{Type: acp.FrameRun, Payload: raw})
}

// Ping 发一次 PING，等待对应 PONG，返回 RTT。
func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	nonce := atomic.AddUint64(&c.pingNonce, 1)
	payload := make([]byte, 8)
	for i := 0; i < 8; i++ {
		payload[7-i] = byte(nonce >> (i * 8))
	}
	start := time.Now()
	if err := c.writeFrame(acp.Frame{Type: acp.FramePing, Payload: payload}); err != nil {
		return 0, err
	}
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		f, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		if f.Type == acp.FramePong {
			return time.Since(start), nil
		}
		// 中途有 EVENT 等帧，忽略；ping 通常用在 idle 连接上
	}
}

// NextEvent 阻塞读下一帧 EVENT/ERROR/CLOSE，返回 (event, endStream, error)。
//   - 收到 CLOSE / END_STREAM 时 endStream=true
//   - 业务 ERROR 帧通过 *pb.RunEvent.Error 表达
func (c *Client) NextEvent() (*pb.RunEvent, bool, error) {
	for {
		f, err := c.readFrame()
		if err != nil {
			return nil, false, err
		}
		switch f.Type {
		case acp.FrameEvent:
			ev := &pb.RunEvent{}
			if err := proto.Unmarshal(f.Payload, ev); err != nil {
				return nil, false, fmt.Errorf("decode event: %w", err)
			}
			return ev, f.Flags.Has(acp.FlagEndStream), nil
		case acp.FrameError:
			var ep acp.ErrorPayload
			_ = acp.UnmarshalJSONPayload(f.Payload, &ep)
			ev := &pb.RunEvent{
				RunId:   c.runID,
				TraceId: c.traceID,
				Payload: &pb.RunEvent_Error{Error: &pb.Error{Code: ep.Code, Message: ep.Message, Retriable: ep.Retriable}},
			}
			return ev, true, nil
		case acp.FrameClose:
			return nil, true, nil
		case acp.FramePong:
			continue
		default:
			// 未知帧静默丢弃
			continue
		}
	}
}

// SendResume 发送 RESUME 帧（断线续传），之后调用 NextEvent 读回放事件。
func (c *Client) SendResume(runID string, lastSeq uint64) error {
	payload, _ := acp.MarshalJSONPayload(acp.Resume{RunID: runID, LastSeq: lastSeq})
	return c.writeFrame(acp.Frame{Type: acp.FrameResume, Payload: payload})
}

// Close 主动关闭连接。
func (c *Client) Close() error {
	cp, _ := acp.MarshalJSONPayload(acp.Close{Code: "client_done"})
	_ = c.writeFrame(acp.Frame{Type: acp.FrameClose, Flags: acp.FlagEndStream, Payload: cp})
	return c.conn.Close()
}

func (c *Client) writeFrame(f acp.Frame) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	return acp.WriteFrame(c.conn, f)
}

func (c *Client) readFrame() (acp.Frame, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	return acp.ReadFrame(c.br)
}

// IsClosed 简易检测连接是否已关闭。
func (c *Client) IsClosed() bool {
	if c.conn == nil {
		return true
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(time.Microsecond))
	one := make([]byte, 1)
	if _, err := c.conn.Read(one); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			_ = c.conn.SetReadDeadline(time.Time{})
			return false
		}
		return true
	}
	return false
}
