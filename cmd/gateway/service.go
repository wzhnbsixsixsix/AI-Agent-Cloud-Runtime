package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// agentService 实现 pb.AgentServiceServer：
//   收到客户端第一条 RunRequest -> 生成 run_id+trace_id -> XADD 到任务队列 ->
//   订阅 events:{run_id} -> 把事件转换成 RunEvent 流回客户端 -> 看到 DONE/ERROR 关流。
type agentService struct {
	pb.UnimplementedAgentServiceServer
	q          *queue.StreamQueue
	ps         *queue.PubSub
	log        *slog.Logger
	runTimeout time.Duration
}

func (s *agentService) RunAgent(stream pb.AgentService_RunAgentServer) error {
	ctx := stream.Context()
	// W1 阶段只取第一条作为整轮的 prompt，简化客户端实现。
	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return status.Errorf(codes.InvalidArgument, "recv first: %v", err)
	}
	if first.GetPrompt() == "" {
		return status.Error(codes.InvalidArgument, "empty prompt")
	}

	runID := first.GetRunId()
	if runID == "" {
		runID = obs.NewRunID()
	}
	traceID := first.GetTraceId()
	if traceID == "" {
		traceID = obs.NewTraceID()
	}
	ctx = obs.WithRunID(obs.WithTraceID(ctx, traceID), runID)
	log := obs.LoggerFromContext(ctx)

	// 先订阅事件，再投递任务，避免投递后第一时间事件丢失
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	evCh, evCancel, err := s.ps.Subscribe(subCtx, runID)
	if err != nil {
		return status.Errorf(codes.Internal, "subscribe: %v", err)
	}
	defer evCancel()

	if _, err := s.q.Publish(ctx, queue.Task{
		RunID:   runID,
		UserID:  first.GetUserId(),
		Prompt:  first.GetPrompt(),
		Model:   first.GetModel(),
		TraceID: traceID,
	}); err != nil {
		return status.Errorf(codes.Internal, "publish task: %v", err)
	}
	log.Info("task dispatched", "user", first.GetUserId())

	// 总超时（防止任务卡死，gateway 永远挂）
	timeout := s.runTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	hard, hardCancel := context.WithTimeout(ctx, timeout)
	defer hardCancel()

	for {
		select {
		case <-hard.Done():
			_ = stream.Send(&pb.RunEvent{
				RunId:   runID,
				TraceId: traceID,
				Payload: &pb.RunEvent_Error{Error: &pb.Error{
					Code: "timeout", Message: "run timeout", Retriable: true,
				}},
			})
			return status.Error(codes.DeadlineExceeded, "run timeout")
		case ev, ok := <-evCh:
			if !ok {
				return nil
			}
			out := mapEvent(runID, traceID, ev)
			if out == nil {
				continue
			}
			if err := stream.Send(out); err != nil {
				log.Warn("send event", "err", err)
				return err
			}
			if ev.Kind == queue.EventDone || ev.Kind == queue.EventError {
				return nil
			}
		}
	}
}

func mapEvent(runID, traceID string, ev queue.Event) *pb.RunEvent {
	base := &pb.RunEvent{RunId: runID, TraceId: traceID}
	switch ev.Kind {
	case queue.EventToken:
		base.Payload = &pb.RunEvent_Token{Token: &pb.Token{Text: ev.Text, Index: ev.Index}}
	case queue.EventState:
		base.Payload = &pb.RunEvent_StateChanged{StateChanged: &pb.StateChanged{
			From:     stateEnum(ev.From),
			To:       stateEnum(ev.State),
			TsUnixMs: time.Now().UnixMilli(),
		}}
	case queue.EventDone:
		base.Payload = &pb.RunEvent_Done{Done: &pb.Done{TotalTokens: ev.Total, TsUnixMs: time.Now().UnixMilli()}}
	case queue.EventError:
		base.Payload = &pb.RunEvent_Error{Error: &pb.Error{Code: ev.Code, Message: ev.Message, Retriable: false}}
	default:
		return nil
	}
	return base
}

func stateEnum(s string) pb.RunState {
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
	default:
		return pb.RunState_RUN_STATE_UNSPECIFIED
	}
}
