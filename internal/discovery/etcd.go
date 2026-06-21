// Package discovery contains the W8 etcd-backed service discovery and
// scheduler leader election primitives.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const (
	servicePrefix   = "/agentforge/services"
	schedulerLeader = "/agentforge/scheduler/leader"
)

type Instance struct {
	Service   string            `json:"service"`
	ID        string            `json:"id"`
	Addr      string            `json:"addr"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt int64             `json:"updated_at"`
}

type Registration struct {
	client *clientv3.Client
	cancel context.CancelFunc
	done   chan struct{}
}

func Register(ctx context.Context, endpoints []string, inst Instance, ttlSeconds int64) (*Registration, error) {
	if inst.Service == "" || inst.ID == "" || inst.Addr == "" {
		return nil, fmt.Errorf("discovery: service, id, and addr are required")
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 10
	}
	cli, err := newClient(endpoints)
	if err != nil {
		return nil, err
	}
	lease, err := cli.Grant(ctx, ttlSeconds)
	if err != nil {
		_ = cli.Close()
		return nil, err
	}
	regCtx, cancel := context.WithCancel(ctx)
	reg := &Registration{client: cli, cancel: cancel, done: make(chan struct{})}
	key := fmt.Sprintf("%s/%s/%s", servicePrefix, inst.Service, inst.ID)
	inst.UpdatedAt = time.Now().Unix()
	raw, err := json.Marshal(inst)
	if err != nil {
		cancel()
		_ = cli.Close()
		return nil, err
	}
	if _, err := cli.Put(ctx, key, string(raw), clientv3.WithLease(lease.ID)); err != nil {
		cancel()
		_ = cli.Close()
		return nil, err
	}
	keepAlive, err := cli.KeepAlive(regCtx, lease.ID)
	if err != nil {
		cancel()
		_ = cli.Close()
		return nil, err
	}
	go func() {
		defer close(reg.done)
		defer cli.Close()
		for {
			select {
			case <-regCtx.Done():
				revokeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, _ = cli.Revoke(revokeCtx, lease.ID)
				cancel()
				return
			case _, ok := <-keepAlive:
				if !ok {
					return
				}
			}
		}
	}()
	return reg, nil
}

func (r *Registration) Close() error {
	if r == nil {
		return nil
	}
	r.cancel()
	<-r.done
	return nil
}

type LeaderInfo struct {
	ID   string `json:"id"`
	Addr string `json:"addr"`
}

type Elector struct {
	client   *clientv3.Client
	id       string
	addr     string
	key      string
	ttl      int
	mu       sync.RWMutex
	leader   LeaderInfo
	isLeader bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func StartSchedulerElection(ctx context.Context, endpoints []string, id, addr string, ttlSeconds int) (*Elector, error) {
	if id == "" || addr == "" {
		return nil, fmt.Errorf("discovery: scheduler id and advertise addr are required")
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3
	}
	cli, err := newClient(endpoints)
	if err != nil {
		return nil, err
	}
	eCtx, cancel := context.WithCancel(ctx)
	e := &Elector{
		client: cli,
		id:     id,
		addr:   addr,
		key:    schedulerLeader,
		ttl:    ttlSeconds,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go e.loop(eCtx)
	return e, nil
}

func (e *Elector) Close() error {
	if e == nil {
		return nil
	}
	e.cancel()
	<-e.done
	return nil
}

func (e *Elector) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isLeader
}

func (e *Elector) Leader(ctx context.Context) (LeaderInfo, bool) {
	if e == nil {
		return LeaderInfo{}, false
	}
	if info, ok := e.cachedLeader(); ok {
		return info, true
	}
	resp, err := e.client.Get(
		ctx,
		e.key,
		clientv3.WithPrefix(),
		clientv3.WithSort(clientv3.SortByCreateRevision, clientv3.SortAscend),
		clientv3.WithLimit(1),
	)
	if err != nil || len(resp.Kvs) == 0 {
		return LeaderInfo{}, false
	}
	info, ok := decodeLeader(resp.Kvs[0].Value)
	if ok {
		e.setLeader(info, e.id == info.ID)
	}
	return info, ok
}

func (e *Elector) loop(ctx context.Context) {
	defer close(e.done)
	defer e.client.Close()
	for ctx.Err() == nil {
		session, err := concurrency.NewSession(e.client, concurrency.WithTTL(e.ttl), concurrency.WithContext(ctx))
		if err != nil {
			sleepContext(ctx, time.Second)
			continue
		}
		el := concurrency.NewElection(session, e.key)
		value, _ := json.Marshal(LeaderInfo{ID: e.id, Addr: e.addr})
		if err := el.Campaign(ctx, string(value)); err != nil {
			_ = session.Close()
			sleepContext(ctx, time.Second)
			continue
		}
		e.setLeader(LeaderInfo{ID: e.id, Addr: e.addr}, true)
		select {
		case <-ctx.Done():
		case <-session.Done():
		}
		e.setLeader(LeaderInfo{}, false)
		resignCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = el.Resign(resignCtx)
		cancel()
		_ = session.Close()
	}
}

func (e *Elector) cachedLeader() (LeaderInfo, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.leader.ID == "" {
		return LeaderInfo{}, false
	}
	return e.leader, true
}

func (e *Elector) setLeader(info LeaderInfo, isLeader bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.leader = info
	e.isLeader = isLeader
}

func decodeLeader(raw []byte) (LeaderInfo, bool) {
	var info LeaderInfo
	if err := json.Unmarshal(raw, &info); err == nil && info.ID != "" {
		return info, true
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) == 2 && parts[0] != "" {
		return LeaderInfo{ID: parts[0], Addr: parts[1]}, true
	}
	return LeaderInfo{}, false
}

func newClient(endpoints []string) (*clientv3.Client, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("discovery: no etcd endpoints configured")
	}
	return clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 3 * time.Second,
	})
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
