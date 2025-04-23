package storage

import (
	"ararauna/internal/config"
	"ararauna/internal/errs"
	"context"
	"hash/fnv"
	"sync"
	"time"
)

const (
	defaultGCInterval = 100 * time.Millisecond
	defaultGCBudget   = 50 * time.Millisecond
)

type store struct {
	shardsCount int
	shards      []*_shard
	gcInterval  time.Duration
	gcBudget    time.Duration
	gcCursor    int
}

type _shard struct {
	mu   sync.RWMutex
	data map[Key]Value
}

type Value struct {
	Data      []byte
	ExpiresAt *time.Time
}

type Key string

func New(ctx context.Context, cfg *config.Config) *store {
	shards := make([]*_shard, cfg.Storage.Shards)

	for idx := range shards {
		shards[idx] = &_shard{
			data: make(map[Key]Value, 128),
		}
	}

	interval := cfg.Storage.GCInterval
	if interval <= 0 {
		interval = defaultGCInterval
	}
	budget := cfg.Storage.GCBudget
	if budget <= 0 {
		budget = defaultGCBudget
	}

	s := &store{
		shards:      shards,
		shardsCount: cfg.Storage.Shards,
		gcInterval:  interval,
		gcBudget:    budget,
	}

	go s.runGC(ctx)

	return s
}

func (c *store) getShardId(key string) int {
	h := fnv.New32()
	h.Write([]byte(key))
	return int(h.Sum32()) % c.shardsCount
}

func (s *store) Get(ctx context.Context, key string) (*Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	shardId := s.getShardId(key)
	shard := s.shards[shardId]
	shard.mu.RLock()
	value, found := shard.data[Key(key)]
	shard.mu.RUnlock()

	if !found {
		return nil, errs.ErrNotFound
	}

	if value.ExpiresAt != nil && time.Now().After(*value.ExpiresAt) {
		return nil, errs.ErrNotFound
	}

	return &value, nil
}

func (s *store) Set(ctx context.Context, key string, value []byte, expiresAt *time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	shardId := s.getShardId(key)
	shard := s.shards[shardId]
	shard.mu.Lock()
	shard.data[Key(key)] = Value{Data: value, ExpiresAt: expiresAt}
	shard.mu.Unlock()
	return nil
}

func (s *store) Del(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	shardId := s.getShardId(key)
	shard := s.shards[shardId]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, found := shard.data[Key(key)]; !found {
		return "", errs.ErrNotFound
	}

	delete(shard.data, Key(key))
	return key, nil
}

func (s *store) runGC(ctx context.Context) {
	ticker := time.NewTicker(s.gcInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.gcTick(ctx)
		}
	}
}

func (s *store) gcTick(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, s.gcBudget)
	defer cancel()
	done := tickCtx.Done()

	for range s.shardsCount {
		select {
		case <-done:
			return
		default:
		}
		s.gcSweepShard(s.gcCursor, done)
		s.gcCursor = (s.gcCursor + 1) % s.shardsCount
	}
}

func (s *store) gcSweepShard(idx int, done <-chan struct{}) {
	shard := s.shards[idx]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	for key, value := range shard.data {
		select {
		case <-done:
			return
		default:
		}
		if value.ExpiresAt != nil && now.After(*value.ExpiresAt) {
			delete(shard.data, key)
		}
	}
}
