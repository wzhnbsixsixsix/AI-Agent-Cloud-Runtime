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
	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

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
	logger.Info("worker booting", "concurrency", cfg.Concurrency, "provider", cfg.LLMProvider)

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

	runner := agent.NewRunner(store, provider, pubsub)

	// 注册到 scheduler（best effort，scheduler 不可用时仅打日志）
	go runScheduler(rootCtx, logger, cfg, workerID)

	// N 个 consumer goroutine
	var wg sync.WaitGroup
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
				logger.Warn("consumer exited", "consumer", consumer, "err", err)
			}
		}(consumer)
	}

	<-rootCtx.Done()
	logger.Info("shutdown signal, waiting consumers")
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		logger.Warn("consumers exit timeout")
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


