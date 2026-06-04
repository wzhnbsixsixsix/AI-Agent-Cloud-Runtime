// Package redisstore 提供带健康检查与重试的 Redis 客户端。
package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Options Redis 连接参数。
type Options struct {
	Addr     string
	Password string
	DB       int
}

// New 返回连接已健康的 *redis.Client；最多重试 30s。
func New(ctx context.Context, opt Options) (*redis.Client, error) {
	cli := redis.NewClient(&redis.Options{
		Addr:     opt.Addr,
		Password: opt.Password,
		DB:       opt.DB,
	})

	deadline := time.Now().Add(30 * time.Second)
	backoff := 200 * time.Millisecond
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := cli.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			return cli, nil
		}
		if time.Now().After(deadline) {
			_ = cli.Close()
			return nil, fmt.Errorf("redis ping after retries: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = cli.Close()
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}
