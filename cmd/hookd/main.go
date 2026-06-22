package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
	"github.com/wzhnbsixsixsix/agentforge/internal/hook"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.LoadHook()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	engine, err := hook.LoadWithConfig(hook.Config{
		Root:           cfg.HookRoot,
		Timeout:        cfg.HookTimeout,
		FailClosed:     cfg.HookFailClosed,
		MaxStdoutBytes: cfg.HookMaxStdoutBytes,
	})
	if err != nil {
		logger.Error("hook load", "err", err)
		os.Exit(1)
	}
	lis, err := net.Listen("tcp", cfg.HookGRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}
	srv := grpc.NewServer()
	pb.RegisterHookServiceServer(srv, &hookService{engine: engine, log: logger})
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	telemetry, err := obs.InitTelemetry(rootCtx, obs.TelemetryConfig{
		ServiceName:        cfg.OTELServiceName,
		DefaultServiceName: "hookd",
		OTELEnabled:        cfg.OTELEnabled,
		OTLPEndpoint:       cfg.OTELExporterOTLPEndpoint,
		MetricsEnabled:     cfg.MetricsEnabled,
		MetricsAddr:        cfg.MetricsAddr,
		MetricsPath:        cfg.MetricsPath,
	}, logger)
	if err != nil {
		logger.Warn("telemetry init", "err", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = telemetry.Shutdown(shutdownCtx)
		cancel()
	}()
	if cfg.DiscoveryEnabled {
		reg, err := discovery.Register(rootCtx, cfg.EtcdEndpoints, discovery.Instance{
			Service: "hookd",
			ID:      hostnameID("hookd"),
			Addr:    cfg.HookGRPCAddr,
		}, 10)
		if err != nil {
			logger.Error("discovery register", "err", err)
		} else {
			defer reg.Close()
		}
	}
	go func() {
		logger.Info("hookd serving", "addr", cfg.HookGRPCAddr, "hooks", len(engine.Hooks))
		if err := srv.Serve(lis); err != nil {
			logger.Error("serve", "err", err)
			stop()
		}
	}()
	<-rootCtx.Done()
	srv.GracefulStop()
}

func hostnameID(prefix string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return prefix
	}
	return prefix + "-" + name
}

type hookService struct {
	pb.UnimplementedHookServiceServer
	engine *hook.Engine
	log    *slog.Logger
}

func (s *hookService) ExecuteHook(ctx context.Context, req *pb.ExecuteHookRequest) (*pb.ExecuteHookResponse, error) {
	resp, err := s.engine.Execute(ctx, hook.Request{
		Event:       hook.EventFromProto(req.GetEvent()),
		RunID:       req.GetRunId(),
		TraceID:     req.GetTraceId(),
		PayloadJSON: req.GetPayloadJson(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.ExecuteHookResponse{
		Allowed:              resp.Allowed,
		Reason:               resp.Reason,
		PayloadJson:          resp.PayloadJSON,
		AppendSystemMessages: resp.AppendSystemMessages,
		MatchedHooks:         resp.MatchedHooks,
	}, nil
}

func (s *hookService) ListHooks(ctx context.Context, _ *pb.ListHooksRequest) (*pb.ListHooksResponse, error) {
	infos := s.engine.List()
	out := make([]*pb.HookInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, &pb.HookInfo{
			Id:          info.ID,
			Event:       hook.EventToProto(info.Event),
			Enabled:     info.Enabled,
			Source:      info.Source,
			Description: info.Description,
		})
	}
	return &pb.ListHooksResponse{Hooks: out}, nil
}
