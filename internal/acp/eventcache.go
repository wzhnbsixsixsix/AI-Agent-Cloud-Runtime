// Package acp 是 ACP 协议的服务端/客户端实现，依赖 pkg/acp 帧规范。
package acp

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// EventCache 用 Redis ZSet 缓存某次 run 的所有 EVENT 帧 payload。
//   - key:    acp:events:{run_id}
//   - score:  seq（uint64 转 float64，<= 2^53 安全）
//   - member: 帧 payload（即 protobuf 编码的 RunEvent）
//
// 为了让重复 ZADD 不互相覆盖，member 末尾追加 8 字节的大端 seq，使每个成员唯一。
type EventCache struct {
	cli *redis.Client
	ttl time.Duration
}

// NewEventCache 构造缓存。ttl 建议 1h。
func NewEventCache(cli *redis.Client, ttl time.Duration) *EventCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &EventCache{cli: cli, ttl: ttl}
}

// Key 返回该 run 对应的 Redis key。
func (c *EventCache) Key(runID string) string {
	return "acp:events:" + runID
}

// Append 把一条事件追加到缓存。
func (c *EventCache) Append(ctx context.Context, runID string, seq uint64, payload []byte) error {
	key := c.Key(runID)
	member := encodeMember(seq, payload)
	if err := c.cli.ZAdd(ctx, key, redis.Z{Score: float64(seq), Member: member}).Err(); err != nil {
		return fmt.Errorf("zadd: %w", err)
	}
	// 滚动续期 TTL（成本极低）
	c.cli.Expire(ctx, key, c.ttl)
	return nil
}

// Replay 取出 seq > sinceSeq 的全部缓存事件，按 seq 升序返回。
func (c *EventCache) Replay(ctx context.Context, runID string, sinceSeq uint64) ([]CachedEvent, error) {
	key := c.Key(runID)
	// score 用 (sinceSeq 表示开区间
	res, err := c.cli.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("(%d", sinceSeq),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("zrangebyscore: %w", err)
	}
	out := make([]CachedEvent, 0, len(res))
	for _, z := range res {
		seq, payload, ok := decodeMember(z.Member.(string))
		if !ok {
			continue
		}
		out = append(out, CachedEvent{Seq: seq, Payload: payload})
	}
	return out, nil
}

// CachedEvent 是回放出的一条事件。
type CachedEvent struct {
	Seq     uint64
	Payload []byte
}

// encodeMember 把 (seq, payload) 编码为 ZSet member：payload || BE(seq)
// 这样不同 seq 的相同 payload 不会被 ZADD 去重。
func encodeMember(seq uint64, payload []byte) string {
	b := make([]byte, len(payload)+8)
	copy(b, payload)
	for i := 0; i < 8; i++ {
		b[len(payload)+7-i] = byte(seq >> (i * 8))
	}
	return string(b)
}

// decodeMember 还原 (seq, payload)。
func decodeMember(s string) (uint64, []byte, bool) {
	if len(s) < 8 {
		return 0, nil, false
	}
	tail := s[len(s)-8:]
	var seq uint64
	for i := 0; i < 8; i++ {
		seq = (seq << 8) | uint64(tail[i])
	}
	payload := []byte(s[:len(s)-8])
	return seq, payload, true
}
