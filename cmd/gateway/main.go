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
	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"
	"github.com/wzhnbsixsixsix/agentforge/internal/tool"

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
	telemetry, err := obs.InitTelemetry(rootCtx, obs.TelemetryConfig{
		ServiceName:        cfg.OTELServiceName,
		DefaultServiceName: "gateway",
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
			Service: "gateway",
			ID:      hostnameID("gateway"),
			Addr:    cfg.GRPCAddr,
		}, 10)
		if err != nil {
			logger.Error("discovery register", "err", err)
		} else {
			defer reg.Close()
		}
	}

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

	// W3: tool stream
	toolQ := queue.NewToolStream(rdb)
	if err := toolQ.EnsureGroup(rootCtx, "tool-runtime"); err != nil {
		logger.Error("ensure tool group", "err", err)
		os.Exit(1)
	}
	toolBus := queue.NewToolBus(rdb)
	// gateway 只用 registry 拿 descriptor + 校验名字，不真的执行 tool；
	// HTTP allow-list/max-bytes 在这里无所谓（worker 才是执行端）。
	toolReg := tool.Builtins(tool.BuiltinsConfig{})
	tHandler := &toolHandler{
		q:        toolQ,
		bus:      toolBus,
		registry: toolReg,
		log:      logger.With("comp", "tool"),
		timeout:  cfg.ToolCallTimeout,
	}

	var rHandler *ragHandler
	if cfg.RAGEnabled {
		h, err := newRAGHandler(rootCtx, cfg.RAGServiceAddr)
		if err != nil {
			logger.Warn("rag service disabled", "err", err)
		} else {
			rHandler = h
			defer rHandler.close()
			logger.Info("rag service proxy enabled", "addr", cfg.RAGServiceAddr)
		}
	}

	// ---- gRPC server ----
	srv := grpc.NewServer()
	svc := &agentService{
		q:          streamQ,
		ps:         pubsub,
		log:        logger,
		runTimeout: cfg.RunTimeout,
		tool:       tHandler,
		rag:        rHandler,
	}
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

func hostnameID(prefix string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return prefix
	}
	return prefix + "-" + name
}
