package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	internalacp "github.com/wzhnbsixsixsix/agentforge/internal/acp"
	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg, err := config.LoadGateway()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	logger.Info("gateway booting", "grpc", cfg.GRPCAddr, "acp", cfg.ACPAddr)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb, err := redisstore.New(rootCtx, redisstore.Options{
		Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB,
	})
	if err != nil {
		logger.Error("redis connect", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	streamQ := queue.NewStream(rdb)
	if err := streamQ.EnsureGroup(rootCtx, "workers"); err != nil {
		logger.Error("ensure group", "err", err)
		os.Exit(1)
	}
	pubsub := queue.NewPubSub(rdb)

	// ---- gRPC server ----
	srv := grpc.NewServer()
	svc := &agentService{q: streamQ, ps: pubsub, log: logger, runTimeout: cfg.RunTimeout}
	pb.RegisterAgentServiceServer(srv, svc)
	healthSvc := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSvc)
	healthSvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen grpc", "err", err)
		os.Exit(1)
	}

	// ---- ACP server ----
	cache := internalacp.NewEventCache(rdb, cfg.ACPCacheTTL)
	disp := &acpDispatcher{q: streamQ, ps: pubsub, log: logger, runTimeout: cfg.RunTimeout}
	acpSrv := internalacp.NewServer(internalacp.ServerConfig{
		Addr:           cfg.ACPAddr,
		ReadTimeout:    cfg.ACPReadTimeout,
		MaxConnections: cfg.ACPMaxConnections,
	}, disp, cache, logger, idGenAdapter{})

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("grpc serving", "addr", cfg.GRPCAddr)
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
		}
	}()
	if cfg.ACPAddr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := acpSrv.ListenAndServe(rootCtx); err != nil {
				errCh <- err
			}
		}()
	}

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("serve err", "err", err)
		stop()
	}
	stopped := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		logger.Warn("graceful timeout, force stop")
		srv.Stop()
	}
	wg.Wait()
}
