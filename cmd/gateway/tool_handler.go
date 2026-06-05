package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	"github.com/wzhnbsixsixsix/agentforge/internal/tool"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// toolHandler 处理 ExecTool / ListTools 两个 RPC。
//
// 流程（ExecTool）：
//   gateway 收到 RPC -> 生成 call_id -> 订阅 tool_results:{call_id} ->
//   XADD ToolTask 到 queue:tool_tasks -> 等待第一条 ToolResultEvent ->
//   组装 ExecToolResponse 返回。
type toolHandler struct {
	q        *queue.ToolStreamQueue
	bus      *queue.ToolBus
	registry *tool.Registry // 仅用于 ListTools 与名字校验
	log      *slog.Logger
	timeout  time.Duration
}

func (h *toolHandler) listTools(_ context.Context, _ *pb.ListToolsRequest) (*pb.ListToolsResponse, error) {
	descs := h.registry.List()
	out := &pb.ListToolsResponse{Tools: make([]*pb.ToolDescriptor, 0, len(descs))}
	for _, d := range descs {
		out.Tools = append(out.Tools, &pb.ToolDescriptor{
			Name:           d.Name,
			Description:    d.Description,
			ParametersJson: string(d.Schema),
		})
	}
	return out, nil
}

func (h *toolHandler) execTool(ctx context.Context, req *pb.ExecToolRequest) (*pb.ExecToolResponse, error) {
	if req.GetTool() == "" {
		return nil, status.Error(codes.InvalidArgument, "tool is required")
	}
	if _, ok := h.registry.Get(req.GetTool()); !ok {
		return nil, status.Errorf(codes.NotFound, "unknown tool %q", req.GetTool())
	}

	callID := obs.NewRunID() // 复用 ulid 生成器
	traceID := req.GetTraceId()
	if traceID == "" {
		traceID = obs.NewTraceID()
	}
	ctx = obs.WithRunID(obs.WithTraceID(ctx, traceID), callID)
	log := obs.LoggerFromContext(ctx).With("tool", req.GetTool())

	// 1) 先订阅
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	evCh, evCancel, err := h.bus.Subscribe(subCtx, callID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "subscribe: %v", err)
	}
	defer evCancel()

	// 2) 投递任务
	if _, err := h.q.Publish(ctx, queue.ToolTask{
		CallID:    callID,
		UserID:    req.GetUserId(),
		Tool:      req.GetTool(),
		ArgsJSON:  string(req.GetArgsJson()),
		TimeoutMS: int(req.GetTimeoutMs()),
		TraceID:   traceID,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "publish tool task: %v", err)
	}
	log.Info("tool task dispatched", "call_id", callID)

	// 3) 等结果
	timeout := h.timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if req.GetTimeoutMs() > 0 {
		// 给 server 侧多 5s 的容错（worker 端会自己超时返回错误事件）
		want := time.Duration(req.GetTimeoutMs())*time.Millisecond + 5*time.Second
		if want > timeout {
			timeout = want
		}
	}
	hard, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-hard.Done():
		return nil, status.Error(codes.DeadlineExceeded, "tool call timeout (gateway side)")
	case ev, ok := <-evCh:
		if !ok {
			return nil, status.Error(codes.Internal, "tool result channel closed")
		}
		var meta []byte
		if ev.MetaJSON != "" {
			meta = []byte(ev.MetaJSON)
		} else {
			meta = mustEmptyJSON()
		}
		return &pb.ExecToolResponse{
			CallId:       ev.CallID,
			ContainerId:  ev.ContainerID,
			ExitCode:     int32(ev.ExitCode),
			Content:      ev.Content,
			IsError:      ev.IsError,
			MetadataJson: meta,
			ElapsedMs:    uint64(ev.ElapsedMS),
			ErrorCode:    ev.ErrorCode,
			ErrorMessage: ev.ErrorMsg,
		}, nil
	}
}

func mustEmptyJSON() []byte {
	b, _ := json.Marshal(map[string]any{})
	return b
}
