package main

import (
	"context"
	"time"

	sched "github.com/wzhnbsixsixsix/agentforge/internal/scheduler"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"
)

type schedulerService struct {
	pb.UnimplementedSchedulerServiceServer
	s *sched.RedisScheduler
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
