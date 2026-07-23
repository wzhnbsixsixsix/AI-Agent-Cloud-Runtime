package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
	"google.golang.org/grpc"
)

type UIEvent struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Data      any       `json:"data"`
	CreatedAt time.Time `json:"createdAt"`
}
type EventStore struct{ rdb *redis.Client }

func NewEventStore(rdb *redis.Client) *EventStore { return &EventStore{rdb} }
func (s *EventStore) key(runID string) string     { return "ui:run_events:" + runID }
func (s *EventStore) seqKey(runID string) string  { return "ui:run_event_seq:" + runID }
func (s *EventStore) Append(ctx context.Context, runID, kind string, data any) (UIEvent, error) {
	id, err := s.rdb.Incr(ctx, s.seqKey(runID)).Result()
	if err != nil {
		return UIEvent{}, err
	}
	ev := UIEvent{ID: id, Type: kind, Data: data, CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(ev)
	if err != nil {
		return UIEvent{}, err
	}
	pipe := s.rdb.TxPipeline()
	pipe.RPush(ctx, s.key(runID), raw)
	pipe.Expire(ctx, s.key(runID), 24*time.Hour)
	pipe.Expire(ctx, s.seqKey(runID), 24*time.Hour)
	_, err = pipe.Exec(ctx)
	return ev, err
}
func (s *EventStore) After(ctx context.Context, runID string, last int64) ([]UIEvent, error) {
	raw, err := s.rdb.LRange(ctx, s.key(runID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	out := []UIEvent{}
	for _, v := range raw {
		var ev UIEvent
		if json.Unmarshal([]byte(v), &ev) == nil && ev.ID > last {
			out = append(out, ev)
		}
	}
	return out, nil
}

type Service struct {
	Store        *Store
	Docker       *DockerManager
	Events       *EventStore
	gateway      pb.AgentServiceClient
	defaultImage string
	mu           sync.Mutex
}

func NewService(store *Store, docker *DockerManager, events *EventStore, conn *grpc.ClientConn, defaultImage string) *Service {
	return &Service{Store: store, Docker: docker, Events: events, gateway: pb.NewAgentServiceClient(conn), defaultImage: defaultImage}
}
func (s *Service) Create(ctx context.Context, in CreateAgentInput) (AgentSpec, error) {
	if err := in.Validate(s.defaultImage); err != nil {
		return AgentSpec{}, err
	}
	in = normalizeCreate(in, s.defaultImage)
	id := strings.ToLower(ulid.Make().String())
	a := AgentSpec{ID: id, Name: in.Name, Role: in.Role, SystemPrompt: in.SystemPrompt, Model: in.Model, Image: in.Image, CPUQuotaUS: in.CPUQuotaUS, MemoryMB: in.MemoryMB, PidsLimit: in.PidsLimit, Tools: in.Tools, WorkspacePolicy: in.WorkspacePolicy, Status: StatusProvisioning, VolumeName: "agentforge-workspace-" + id}
	if err := s.Store.Create(ctx, a); err != nil {
		return AgentSpec{}, err
	}
	cid, err := s.Docker.Create(ctx, a)
	if err != nil {
		_ = s.Store.UpdateRuntime(ctx, a.ID, StatusFailed, "", err.Error())
		return s.Store.Get(ctx, a.ID)
	}
	if err := s.Store.UpdateRuntime(ctx, a.ID, StatusRunning, cid, ""); err != nil {
		return AgentSpec{}, err
	}
	return s.Store.Get(ctx, a.ID)
}
func (s *Service) StartAgent(ctx context.Context, id string) (AgentSpec, error) {
	a, e := s.Store.Get(ctx, id)
	if e != nil {
		return a, e
	}
	if e = s.Docker.Start(ctx, a.ContainerID); e != nil && !strings.Contains(e.Error(), "already started") {
		return a, e
	}
	e = s.Store.UpdateRuntime(ctx, id, StatusRunning, a.ContainerID, "")
	if e != nil {
		return a, e
	}
	return s.Store.Get(ctx, id)
}
func (s *Service) StopAgent(ctx context.Context, id string) (AgentSpec, error) {
	a, e := s.Store.Get(ctx, id)
	if e != nil {
		return a, e
	}
	if e = s.Docker.Stop(ctx, a.ContainerID); e != nil && !strings.Contains(e.Error(), "is not running") {
		return a, e
	}
	e = s.Store.UpdateRuntime(ctx, id, StatusStopped, a.ContainerID, "")
	if e != nil {
		return a, e
	}
	return s.Store.Get(ctx, id)
}
func (s *Service) DeleteAgent(ctx context.Context, id string) error {
	a, e := s.Store.Get(ctx, id)
	if e != nil {
		return e
	}
	if e = s.Docker.Delete(ctx, a); e != nil {
		return e
	}
	return s.Store.Delete(ctx, id)
}
func (s *Service) StartRun(ctx context.Context, agentID, prompt string) (AgentRun, error) {
	if strings.TrimSpace(prompt) == "" {
		return AgentRun{}, fmt.Errorf("prompt is required")
	}
	a, e := s.Store.Get(ctx, agentID)
	if e != nil {
		return AgentRun{}, e
	}
	if a.Status != StatusRunning {
		return AgentRun{}, fmt.Errorf("agent is %s", a.Status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := AgentRun{RunID: ulid.Make().String(), AgentID: a.ID, TraceID: ulid.Make().String(), Prompt: prompt, Status: "running"}
	if e = s.Store.CreateRun(ctx, r); e != nil {
		if IsConflict(e) {
			return AgentRun{}, fmt.Errorf("agent already has an active run")
		}
		return AgentRun{}, e
	}
	go s.consumeRun(r, a)
	return r, nil
}
func (s *Service) consumeRun(r AgentRun, a AgentSpec) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	stream, err := s.gateway.RunAgent(ctx)
	if err == nil {
		err = stream.Send(&pb.RunRequest{RunId: r.RunID, TraceId: r.TraceID, UserId: "local-dev", AgentId: a.ID, Prompt: r.Prompt, Model: AgentModel, SystemPrompt: a.SystemPrompt})
	}
	if err == nil {
		err = stream.CloseSend()
	}
	if err != nil {
		s.finishRun(r.RunID, "failed", "", err.Error())
		return
	}
	var summary strings.Builder
	for {
		ev, e := stream.Recv()
		if e != nil {
			if e.Error() == "EOF" {
				s.finishRun(r.RunID, "failed", summary.String(), "gateway stream ended without terminal event")
			} else {
				s.finishRun(r.RunID, "failed", summary.String(), e.Error())
			}
			return
		}
		kind, data, terminal, status := convertEvent(ev)
		if kind == "token" {
			if v, ok := data.(map[string]any); ok {
				if text, ok := v["text"].(string); ok {
					summary.WriteString(text)
				}
			}
		}
		if _, e := s.Events.Append(ctx, r.RunID, kind, data); e != nil {
			continue
		}
		if terminal {
			s.finishRun(r.RunID, status, summary.String(), "")
			return
		}
	}
}
func (s *Service) finishRun(id, status, summary, runErr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Store.FinishRun(ctx, id, status, summary, runErr)
}
func convertEvent(ev *pb.RunEvent) (string, any, bool, string) {
	switch x := ev.Payload.(type) {
	case *pb.RunEvent_StateChanged:
		return "state", map[string]any{"from": x.StateChanged.From.String(), "to": x.StateChanged.To.String()}, false, ""
	case *pb.RunEvent_Token:
		return "token", map[string]any{"text": x.Token.Text, "index": x.Token.Index}, false, ""
	case *pb.RunEvent_Done:
		return "done", map[string]any{"totalTokens": x.Done.TotalTokens}, true, "completed"
	case *pb.RunEvent_Error:
		return "error", map[string]any{"code": x.Error.Code, "message": x.Error.Message}, true, "failed"
	}
	return "unknown", map[string]any{}, false, ""
}
