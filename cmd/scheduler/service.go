package main

import (
	"context"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
	sched "github.com/wzhnbsixsixsix/agentforge/internal/scheduler"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
)

type schedulerService struct {
	pb.UnimplementedSchedulerServiceServer
	s           *sched.RedisScheduler
	nodeID      string
	advertise   string
	raftEnabled bool
	elector     *discovery.Elector
}

func (svc *schedulerService) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	info := sched.WorkerInfo{
		WorkerID:    req.GetWorkerId(),
		Addr:        req.GetAddr(),
		Concurrency: req.GetConcurrency(),
		Labels:      req.GetLabels(),
	}
	ttl := 15 * time.Second
	if err := svc.s.Register(ctx, info, ttl); err != nil {
		return nil, err
	}
	return &pb.RegisterResponse{Session: req.GetWorkerId(), TtlSeconds: int64(ttl / time.Second)}, nil
}

func (svc *schedulerService) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if err := svc.s.Heartbeat(ctx, req.GetWorkerId(), req.GetInFlight(), req.GetLoad(), 15*time.Second); err != nil {
		return &pb.HeartbeatResponse{Ok: false}, err
	}
	return &pb.HeartbeatResponse{Ok: true}, nil
}

func (svc *schedulerService) Health(ctx context.Context, _ *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{Status: "ok", TsUnixMs: time.Now().UnixMilli()}, nil
}

func (svc *schedulerService) Pick(ctx context.Context, req *pb.PickRequest) (*pb.PickResponse, error) {
	leader := svc.leaderInfo(ctx)
	if svc.raftEnabled && svc.elector != nil && !svc.elector.IsLeader() {
		return &pb.PickResponse{
			IsLeader:   false,
			LeaderId:   leader.ID,
			LeaderAddr: leader.Addr,
			Reason:     "not_leader",
		}, nil
	}
	workerID, err := svc.s.Pick(ctx, req.GetRunId())
	if err != nil {
		return nil, err
	}
	ws, _ := svc.s.List(ctx)
	var picked sched.WorkerInfo
	for _, w := range ws {
		if w.WorkerID == workerID {
			picked = w
			break
		}
	}
	return &pb.PickResponse{
		WorkerId:   workerID,
		Addr:       picked.Addr,
		IsLeader:   true,
		LeaderId:   leader.ID,
		LeaderAddr: leader.Addr,
		Reason:     "lowest_load",
	}, nil
}

func (svc *schedulerService) Leader(ctx context.Context, _ *pb.LeaderRequest) (*pb.LeaderResponse, error) {
	leader := svc.leaderInfo(ctx)
	return &pb.LeaderResponse{
		LeaderId:   leader.ID,
		LeaderAddr: leader.Addr,
		IsLeader:   svc.elector == nil || svc.elector.IsLeader(),
	}, nil
}

func (svc *schedulerService) leaderInfo(ctx context.Context) discovery.LeaderInfo {
	if svc.elector != nil {
		if info, ok := svc.elector.Leader(ctx); ok {
			return info
		}
	}
	return discovery.LeaderInfo{ID: svc.nodeID, Addr: svc.advertise}
}
