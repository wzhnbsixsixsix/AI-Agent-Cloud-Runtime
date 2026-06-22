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
	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
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
	telemetry, err := obs.InitTelemetry(rootCtx, obs.TelemetryConfig{
		ServiceName:        cfg.OTELServiceName,
		DefaultServiceName: "scheduler",
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

	rdb, err := redisstore.New(rootCtx, redisstore.Options{
		Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB,
	})
	if err != nil {
		logger.Error("redis connect", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	rs := sched.NewRedis(rdb)
	var reg *discovery.Registration
	if cfg.DiscoveryEnabled {
		reg, err = discovery.Register(rootCtx, cfg.EtcdEndpoints, discovery.Instance{
			Service: "scheduler",
			ID:      cfg.NodeID,
			Addr:    cfg.AdvertiseAddr,
		}, 10)
		if err != nil {
			logger.Error("discovery register", "err", err)
		} else {
			defer reg.Close()
		}
	}
	var elector *discovery.Elector
	if cfg.RaftEnabled && cfg.DiscoveryEnabled {
		elector, err = discovery.StartSchedulerElection(rootCtx, cfg.EtcdEndpoints, cfg.NodeID, cfg.AdvertiseAddr, 3)
		if err != nil {
			logger.Error("scheduler election", "err", err)
		} else {
			defer elector.Close()
			logger.Info("scheduler election started", "node_id", cfg.NodeID, "key", "/agentforge/scheduler/leader")
		}
	}

	// gRPC
	grpcSrv := grpc.NewServer()
	pb.RegisterSchedulerServiceServer(grpcSrv, &schedulerService{
		s:           rs,
		nodeID:      cfg.NodeID,
		advertise:   cfg.AdvertiseAddr,
		raftEnabled: cfg.RaftEnabled,
		elector:     elector,
	})
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
	mux.HandleFunc("/leader", func(w http.ResponseWriter, r *http.Request) {
		leader := discovery.LeaderInfo{ID: cfg.NodeID, Addr: cfg.AdvertiseAddr}
		isLeader := true
		if elector != nil {
			if info, ok := elector.Leader(r.Context()); ok {
				leader = info
			}
			isLeader = elector.IsLeader()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"leader_id":   leader.ID,
			"leader_addr": leader.Addr,
			"is_leader":   isLeader,
			"raft_mode":   cfg.RaftEnabled,
		})
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
