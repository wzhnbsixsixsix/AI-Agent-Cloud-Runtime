package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/agent"
	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/hook"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/orchestrator"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	"github.com/wzhnbsixsixsix/agentforge/internal/rag"
	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
	agentskill "github.com/wzhnbsixsixsix/agentforge/internal/skill"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"
	"github.com/wzhnbsixsixsix/agentforge/internal/tool"

	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cfg, err := config.LoadWorker()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	workerID := os.Getenv("HOSTNAME")
	if workerID == "" {
		workerID = obs.NewTraceID()
	}
	logger = logger.With("worker_id", workerID)
	logger.Info("worker booting",
		"concurrency", cfg.Concurrency,
		"provider", cfg.LLMProvider,
		"sandbox_driver", cfg.SandboxDriver)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	telemetry, err := obs.InitTelemetry(rootCtx, obs.TelemetryConfig{
		ServiceName:        cfg.OTELServiceName,
		DefaultServiceName: "worker",
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
			Service: "worker",
			ID:      workerID,
			Addr:    workerID,
			Metadata: map[string]string{
				"concurrency": fmt.Sprintf("%d", cfg.Concurrency),
			},
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

	provider, err := llm.NewFromConfig(llm.FactoryConfig{
		Provider:      cfg.LLMProvider,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
		OpenAIAPIKey:  cfg.OpenAIAPIKey,
		OpenAIModel:   cfg.OpenAIModel,
		OpenAITimeout: cfg.OpenAITimeout,
	})
	if err != nil {
		logger.Error("llm provider init", "err", err)
		os.Exit(1)
	}

	store := history.NewRedis(rdb)
	pubsub := queue.NewPubSub(rdb)
	streamQ := queue.NewStream(rdb)
	if err := streamQ.EnsureGroup(rootCtx, cfg.ConsumerGroup); err != nil {
		logger.Error("ensure group", "err", err)
		os.Exit(1)
	}

	// 注册到 scheduler（best effort）
	go runScheduler(rootCtx, logger, cfg, workerID)

	// ---- W3/W4: sandbox + tools ----
	driver, dErr := makeSandboxDriver(rootCtx, cfg, logger)
	if dErr != nil {
		// sandbox 初始化失败不致命：worker 仍可处理 agent task；只是 tool consumer / agent tool-calling 不上。
		logger.Warn("sandbox driver disabled", "driver", cfg.SandboxDriver, "err", dErr)
	}
	var tRunner *tool.Runner
	if driver != nil {
		registry := tool.Builtins(tool.BuiltinsConfig{
			HTTPAllowList: cfg.ToolHTTPAllowList,
			HTTPMaxBytes:  cfg.ToolHTTPMaxBytes,
		})
		tRunner = &tool.Runner{
			Registry:    registry,
			Driver:      driver,
			Log:         logger.With("comp", "tool"),
			HardTimeout: cfg.SandboxExecHard,
		}
	}

	runner := agent.NewRunner(store, provider, pubsub)
	runner.ToolRunner = tRunner
	runner.ToolMaxSteps = cfg.AgentToolMaxSteps
	runner.MultiAgentEnabled = cfg.MultiAgentEnabled
	runner.SubagentMaxDepth = cfg.SubagentMaxDepth
	runner.SubagentMaxChildren = cfg.SubagentMaxChildren
	runner.SubagentTimeout = cfg.SubagentTimeout
	runner.SubagentChildren = map[string]int{}
	runner.CompactPolicy = orchestrator.CompactPolicy{
		Enabled:  cfg.ContextCompactEnabled,
		MaxChars: cfg.ContextCompactMaxChars,
		KeepHead: cfg.ContextCompactKeepHead,
		KeepTail: cfg.ContextCompactKeepTail,
	}
	var serviceConns []*grpc.ClientConn
	if cfg.SkillEnabled {
		conn, err := dialService(rootCtx, cfg.SkillServiceAddr)
		if err != nil {
			logger.Warn("skill service disabled", "addr", cfg.SkillServiceAddr, "err", err)
		} else {
			serviceConns = append(serviceConns, conn)
			runner.SkillSelector = &agentskill.CachedSelector{
				Next:     agentskill.GRPCSelector{Client: pb.NewSkillServiceClient(conn), TopK: cfg.SkillTopK},
				TTL:      cfg.SkillCacheTTL,
				Capacity: cfg.SkillCacheSize,
			}
			runner.SkillRenderer = agentskill.Renderer{}
			logger.Info("skill service selector enabled", "addr", cfg.SkillServiceAddr, "top_k", cfg.SkillTopK)
		}
	}
	if cfg.RAGEnabled {
		conn, err := dialService(rootCtx, cfg.RAGServiceAddr)
		if err != nil {
			logger.Warn("rag service disabled", "addr", cfg.RAGServiceAddr, "err", err)
		} else {
			serviceConns = append(serviceConns, conn)
			runner.RAGRetriever = rag.GRPCRetriever{Client: pb.NewRAGServiceClient(conn)}
			runner.RAGTenantID = cfg.RAGTenantID
			runner.RAGTopK = cfg.RAGTopK
			runner.RAGMinScore = cfg.RAGMinScore
			logger.Info("rag retriever service enabled", "addr", cfg.RAGServiceAddr, "tenant", cfg.RAGTenantID, "top_k", cfg.RAGTopK)
		}
	}
	if cfg.HookEnabled {
		conn, err := dialService(rootCtx, cfg.HookServiceAddr)
		if err != nil {
			logger.Warn("hook service disabled", "addr", cfg.HookServiceAddr, "err", err)
		} else {
			serviceConns = append(serviceConns, conn)
			hc := hook.GRPCClient{Client: pb.NewHookServiceClient(conn)}
			runner.HookClient = hc
			runner.HookFailClosed = cfg.HookFailClosed
			if tRunner != nil {
				tRunner.HookClient = hc
				tRunner.HookFailClosed = cfg.HookFailClosed
			}
			logger.Info("hook service enabled", "addr", cfg.HookServiceAddr)
		}
	}

	var wg sync.WaitGroup

	// ---- agent task consumers (W1) ----
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		consumer := fmt.Sprintf("%s-%d", workerID, i)
		go func(consumer string) {
			defer wg.Done()
			err := streamQ.Consume(rootCtx, cfg.ConsumerGroup, consumer, cfg.MaxRetry,
				func(ctx context.Context, d queue.Delivery) error {
					return runner.Run(ctx, d.Task)
				})
			if err != nil && err != context.Canceled {
				logger.Warn("agent consumer exited", "consumer", consumer, "err", err)
			}
		}(consumer)
	}

	var toolWG sync.WaitGroup
	if tRunner != nil {
		toolQ := queue.NewToolStream(rdb)
		if err := toolQ.EnsureGroup(rootCtx, cfg.ToolConsumerGroup); err != nil {
			logger.Error("ensure tool group", "err", err)
			os.Exit(1)
		}
		toolBus := queue.NewToolBus(rdb)
		tRunner.Bus = toolBus

		conc := cfg.ToolConcurrency
		if conc <= 0 {
			conc = cfg.Concurrency
		}
		for i := 0; i < conc; i++ {
			toolWG.Add(1)
			consumer := fmt.Sprintf("%s-tool-%d", workerID, i)
			go func(consumer string) {
				defer toolWG.Done()
				err := toolQ.Consume(rootCtx, cfg.ToolConsumerGroup, consumer, 1, tRunner.Handle)
				if err != nil && err != context.Canceled {
					logger.Warn("tool consumer exited", "consumer", consumer, "err", err)
				}
			}(consumer)
		}
		logger.Info("tool consumer running", "concurrency", conc, "tools", len(tRunner.Registry.List()))
	}

	<-rootCtx.Done()
	logger.Info("shutdown signal, waiting consumers")
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		toolWG.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		logger.Warn("consumers exit timeout")
	}
	if driver != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = driver.Close(closeCtx)
		cancel()
	}
	for _, conn := range serviceConns {
		_ = conn.Close()
	}
}

func dialService(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return grpc.DialContext(dialCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
}

// makeSandboxDriver 按 cfg.SandboxDriver 选择 driver 实现。
func makeSandboxDriver(ctx context.Context, cfg *config.Worker, log *slog.Logger) (sandbox.Driver, error) {
	switch cfg.SandboxDriver {
	case "", "disabled":
		return nil, fmt.Errorf("sandbox driver disabled by config")
	case "memory":
		return sandbox.NewMemoryDriver(cfg.SandboxWorkspaceRoot, cfg.SandboxPoolSize)
	case "docker":
		return sandbox.NewDockerDriver(ctx, sandbox.DockerOptions{
			Image:           cfg.SandboxImage,
			PoolSize:        cfg.SandboxPoolSize,
			AcquireTimeout:  cfg.SandboxAcquireTimeout,
			WorkspaceRoot:   cfg.SandboxWorkspaceRoot,
			MemoryBytes:     cfg.SandboxMemoryMB * 1024 * 1024,
			CPUQuota:        cfg.SandboxCPUQuotaUS,
			PidsLimit:       cfg.SandboxPidsLimit,
			ExecTimeoutHard: cfg.SandboxExecHard,
			Logger:          log,
		})
	default:
		return nil, fmt.Errorf("unknown sandbox driver: %s", cfg.SandboxDriver)
	}
}

// runScheduler 周期向 scheduler 注册 + 心跳。
func runScheduler(ctx context.Context, logger *slog.Logger, cfg *config.Worker, workerID string) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, cfg.SchedulerDial,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		logger.Warn("scheduler dial failed (ignored)", "err", err)
		return
	}
	defer conn.Close()
	cli := pb.NewSchedulerServiceClient(conn)

	if _, err := cli.Register(ctx, &pb.RegisterRequest{
		WorkerId:    workerID,
		Addr:        workerID,
		Concurrency: int32(cfg.Concurrency),
	}); err != nil {
		logger.Warn("register failed", "err", err)
		return
	}
	logger.Info("scheduler registered")

	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hbCtx, c := context.WithTimeout(ctx, 3*time.Second)
			_, err := cli.Heartbeat(hbCtx, &pb.HeartbeatRequest{WorkerId: workerID})
			c()
			if err != nil {
				logger.Warn("heartbeat", "err", err)
			}
		}
	}
}
