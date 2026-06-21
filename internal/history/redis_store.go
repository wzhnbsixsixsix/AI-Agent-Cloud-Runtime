package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	"github.com/redis/go-redis/v9"
)

// ErrNotFound 消息不存在。
var ErrNotFound = errors.New("history: message not found")

// RedisStore 用 Hash 存内容、ZSet（score=1） + lex 排序存顺序。
//
//	key history:{run_id}:msgs   Hash field=msg_id value=json(Message)
//	key history:{run_id}:order  ZSet member=msg_id score=0（按 lex 排序，ULID 已是字典序时间序）
type RedisStore struct {
	cli *redis.Client
}

// NewRedis 构造 RedisStore。
func NewRedis(cli *redis.Client) *RedisStore { return &RedisStore{cli: cli} }

func (s *RedisStore) Append(ctx context.Context, runID string, m Message) (string, error) {
	if m.ID == "" {
		m.ID = obs.NewRunID()
	}
	if m.Version == 0 {
		m.Version = 1
	}
	// 默认可见
	if !m.Visible {
		m.Visible = true
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal message: %w", err)
	}
	pipe := s.cli.TxPipeline()
	pipe.HSet(ctx, redisstore.Keys.HistoryMsgs(runID), m.ID, string(payload))
	pipe.ZAdd(ctx, redisstore.Keys.HistoryOrder(runID), redis.Z{Score: 0, Member: m.ID})
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("append: %w", err)
	}
	return m.ID, nil
}

func (s *RedisStore) load(ctx context.Context, runID, msgID string) (*Message, error) {
	raw, err := s.cli.HGet(ctx, redisstore.Keys.HistoryMsgs(runID), msgID).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &m, nil
}

func (s *RedisStore) saveLocked(ctx context.Context, runID string, m *Message) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.cli.HSet(ctx, redisstore.Keys.HistoryMsgs(runID), m.ID, string(payload)).Err()
}

func (s *RedisStore) Patch(ctx context.Context, runID, msgID, content string) error {
	m, err := s.load(ctx, runID, msgID)
	if err != nil {
		return err
	}
	m.Content = content
	m.Version++
	return s.saveLocked(ctx, runID, m)
}

func (s *RedisStore) Hide(ctx context.Context, runID, msgID string) error {
	m, err := s.load(ctx, runID, msgID)
	if err != nil {
		return err
	}
	m.Visible = false
	m.Version++
	return s.saveLocked(ctx, runID, m)
}

func (s *RedisStore) Fold(ctx context.Context, runID string, fromID, toID, summary string) (string, error) {
	if _, err := s.load(ctx, runID, fromID); err != nil {
		return "", err
	}
	if _, err := s.load(ctx, runID, toID); err != nil {
		return "", err
	}
	if fromID > toID {
		fromID, toID = toID, fromID
	}
	ids, err := s.cli.ZRangeByLex(ctx, redisstore.Keys.HistoryOrder(runID), &redis.ZRangeBy{
		Min: "[" + fromID,
		Max: "[" + toID,
	}).Result()
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotFound
	}

	msgs := make([]*Message, 0, len(ids))
	for _, id := range ids {
		m, err := s.load(ctx, runID, id)
		if err != nil {
			return "", err
		}
		if m.Visible {
			m.Visible = false
			m.Version++
			msgs = append(msgs, m)
		}
	}

	pipe := s.cli.TxPipeline()
	for _, m := range msgs {
		payload, err := json.Marshal(m)
		if err != nil {
			return "", fmt.Errorf("marshal message: %w", err)
		}
		pipe.HSet(ctx, redisstore.Keys.HistoryMsgs(runID), m.ID, string(payload))
	}

	folded := Message{
		ID:      obs.NewRunID(),
		Role:    RoleAssistant,
		Content: summary,
		Visible: true,
		Version: 1,
		Tags: map[string]string{
			"compacted": "true",
			"fold_from": fromID,
			"fold_to":   toID,
		},
	}
	payload, err := json.Marshal(folded)
	if err != nil {
		return "", fmt.Errorf("marshal folded message: %w", err)
	}
	pipe.HSet(ctx, redisstore.Keys.HistoryMsgs(runID), folded.ID, string(payload))
	pipe.ZAdd(ctx, redisstore.Keys.HistoryOrder(runID), redis.Z{Score: 0, Member: folded.ID})
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("fold: %w", err)
	}
	return folded.ID, nil
}

func (s *RedisStore) Render(ctx context.Context, runID string) ([]Message, error) {
	ids, err := s.cli.ZRangeByLex(ctx, redisstore.Keys.HistoryOrder(runID), &redis.ZRangeBy{
		Min: "-", Max: "+",
	}).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	raws, err := s.cli.HMGet(ctx, redisstore.Keys.HistoryMsgs(runID), ids...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(raws))
	for _, r := range raws {
		s, ok := r.(string)
		if !ok {
			continue
		}
		var m Message
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			continue
		}
		if !m.Visible {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
