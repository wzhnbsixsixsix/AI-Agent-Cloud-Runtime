package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.LoadGateway()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	logger.Info("gateway booting", "addr", cfg.GRPCAddr)

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

	srv := grpc.NewServer()
	svc := &agentService{q: streamQ, ps: pubsub, log: logger}
	pb.RegisterAgentServiceServer(srv, svc)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("grpc serving", "addr", cfg.GRPCAddr)
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		logger.Error("serve err", "err", err)
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
}
