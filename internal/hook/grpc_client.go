package hook

import (
	"context"
	"fmt"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
)

type GRPCClient struct {
	Client pb.HookServiceClient
}

func (c GRPCClient) Execute(ctx context.Context, req Request) (Response, error) {
	if c.Client == nil {
		return Response{Allowed: true}, nil
	}
	resp, err := c.Client.ExecuteHook(ctx, &pb.ExecuteHookRequest{
		Event:       toPBEvent(req.Event),
		RunId:       req.RunID,
		TraceId:     req.TraceID,
		PayloadJson: req.PayloadJSON,
	})
	if err != nil {
		return Response{}, err
	}
	return Response{
		Allowed:              resp.GetAllowed(),
		Reason:               resp.GetReason(),
		PayloadJSON:          resp.GetPayloadJson(),
		AppendSystemMessages: resp.GetAppendSystemMessages(),
		MatchedHooks:         resp.GetMatchedHooks(),
	}, nil
}

func (c GRPCClient) List(ctx context.Context) ([]Info, error) {
	if c.Client == nil {
		return nil, nil
	}
	resp, err := c.Client.ListHooks(ctx, &pb.ListHooksRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(resp.GetHooks()))
	for _, h := range resp.GetHooks() {
		out = append(out, Info{
			ID:          h.GetId(),
			Event:       fromPBEvent(h.GetEvent()),
			Enabled:     h.GetEnabled(),
			Source:      h.GetSource(),
			Description: h.GetDescription(),
		})
	}
	return out, nil
}

func toPBEvent(ev Event) pb.HookEvent {
	return EventToProto(ev)
}

func EventToProto(ev Event) pb.HookEvent {
	switch ev {
	case EventPreLLM:
		return pb.HookEvent_HOOK_EVENT_PRE_LLM
	case EventPostLLM:
		return pb.HookEvent_HOOK_EVENT_POST_LLM
	case EventPreToolUse:
		return pb.HookEvent_HOOK_EVENT_PRE_TOOL_USE
	case EventPostToolUse:
		return pb.HookEvent_HOOK_EVENT_POST_TOOL_USE
	default:
		return pb.HookEvent_HOOK_EVENT_UNSPECIFIED
	}
}

func fromPBEvent(ev pb.HookEvent) Event {
	return EventFromProto(ev)
}

func EventFromProto(ev pb.HookEvent) Event {
	switch ev {
	case pb.HookEvent_HOOK_EVENT_PRE_LLM:
		return EventPreLLM
	case pb.HookEvent_HOOK_EVENT_POST_LLM:
		return EventPostLLM
	case pb.HookEvent_HOOK_EVENT_PRE_TOOL_USE:
		return EventPreToolUse
	case pb.HookEvent_HOOK_EVENT_POST_TOOL_USE:
		return EventPostToolUse
	default:
		return Event(fmt.Sprintf("unknown:%d", ev))
	}
}
