package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	sched "github.com/wzhnbsixsixsix/agentforge/internal/scheduler"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.LoadScheduler()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	logger.Info("scheduler booting", "grpc", cfg.GRPCAddr, "http", cfg.HTTPAddr)

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

	rs := sched.NewRedis(rdb)

	// gRPC
	grpcSrv := grpc.NewServer()
	pb.RegisterSchedulerServiceServer(grpcSrv, &schedulerService{s: rs})
	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen grpc", "err", err)
		os.Exit(1)
	}
	go func() {
		logger.Info("grpc serving", "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logger.Error("grpc serve", "err", err)
		}
	}()

	// HTTP healthz + workers list
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/workers", func(w http.ResponseWriter, r *http.Request) {
		ws, err := rs.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ws)
	})
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("http serving", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http serve", "err", err)
		}
	}()

	<-rootCtx.Done()
	logger.Info("shutdown")
	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
	grpcSrv.GracefulStop()
}
