package scheduler

import (
	"context"
	"encoding/json"
	"time"

	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	"github.com/redis/go-redis/v9"
)

// RedisScheduler W1 实现：worker 信息以 String(JSON) + TTL 存到 Redis。
type RedisScheduler struct {
	cli *redis.Client
}

// NewRedis 构造。
func NewRedis(cli *redis.Client) *RedisScheduler { return &RedisScheduler{cli: cli} }

func (s *RedisScheduler) Register(ctx context.Context, info WorkerInfo, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	info.UpdatedAt = time.Now()
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	pipe := s.cli.TxPipeline()
	pipe.Set(ctx, redisstore.Keys.WorkerKey(info.WorkerID), string(payload), ttl)
	pipe.SAdd(ctx, redisstore.Keys.WorkerSet, info.WorkerID)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisScheduler) Heartbeat(ctx context.Context, workerID string, inFlight int32, load float64, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	key := redisstore.Keys.WorkerKey(workerID)
	raw, err := s.cli.Get(ctx, key).Result()
	if err != nil {
		// 心跳前要求已注册；没注册则忽略
		return err
	}
	var info WorkerInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return err
	}
	info.InFlight = inFlight
	info.Load = load
	info.UpdatedAt = time.Now()
	payload, _ := json.Marshal(info)
	return s.cli.Set(ctx, key, string(payload), ttl).Err()
}

func (s *RedisScheduler) Pick(ctx context.Context, runID string) (string, error) {
	return "", ErrNotImplemented
}

func (s *RedisScheduler) List(ctx context.Context) ([]WorkerInfo, error) {
	ids, err := s.cli.SMembers(ctx, redisstore.Keys.WorkerSet).Result()
	if err != nil {
		return nil, err
	}
	out := make([]WorkerInfo, 0, len(ids))
	for _, id := range ids {
		raw, err := s.cli.Get(ctx, redisstore.Keys.WorkerKey(id)).Result()
		if err != nil {
			// TTL 过期 -> 从 set 清除
			_ = s.cli.SRem(ctx, redisstore.Keys.WorkerSet, id).Err()
			continue
		}
		var info WorkerInfo
		if err := json.Unmarshal([]byte(raw), &info); err == nil {
			out = append(out, info)
		}
	}
	return out, nil
}
