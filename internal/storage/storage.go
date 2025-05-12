package storage

import (
	"ararauna/internal/errs"
	"ararauna/internal/parser"
	"ararauna/internal/toolbox"
	"ararauna/internal/wal"
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"
)

type store struct {
	tb          *toolbox.Toolbox
	shardsCount int
	shards      []*_shard
	gcInterval  time.Duration
	gcBudget    time.Duration
	gcCursor    int
	wal         *wal.Writer
}

type Storage interface {
	Get(ctx context.Context, key string) (*Value, error)
	Set(ctx context.Context, key string, value []byte, expiresAt *time.Time) error
	Del(ctx context.Context, key string) (string, error)
}

var _ Storage = (*store)(nil)

type _shard struct {
	mu   sync.RWMutex
	data map[Key]Value
}

type Value struct {
	Data      []byte
	ExpiresAt *time.Time
}

type Key string

func New(ctx context.Context, tb *toolbox.Toolbox, w *wal.Writer) (*store, error) {
	shards := make([]*_shard, tb.Cfg.Storage.PartitionsNumber)

	for idx := range shards {
		shards[idx] = &_shard{
			data: make(map[Key]Value, 128),
		}
	}

	s := &store{
		tb:          tb,
		shards:      shards,
		shardsCount: tb.Cfg.Storage.PartitionsNumber,
		gcInterval:  tb.Cfg.Storage.GCInterval,
		gcBudget:    tb.Cfg.Storage.GCBudget,
		wal:         w,
	}

	if err := w.Replay(s.applyFromWAL); err != nil {
		return nil, fmt.Errorf("storage: wal replay: %w", err)
	}

	go s.runGC(ctx)

	return s, nil
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

	var rec parser.Value
	if expiresAt != nil {
		rec = wal.RecordSetX(key, value, expiresAt.UnixNano())
	} else {
		rec = wal.RecordSet(key, value)
	}

	if err := s.wal.Append(rec); err != nil {
		return err
	}

	s.applySet(key, value, expiresAt)

	return nil
}

func (s *store) Del(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	shardId := s.getShardId(key)
	shard := s.shards[shardId]

	shard.mu.RLock()
	_, found := shard.data[Key(key)]
	shard.mu.RUnlock()

	if !found {
		return "", errs.ErrNotFound
	}

	if err := s.wal.Append(wal.RecordDel(key)); err != nil {
		return "", err
	}

	shard.mu.Lock()
	delete(shard.data, Key(key))
	shard.mu.Unlock()

	return key, nil
}

func (s *store) applySet(key string, value []byte, expiresAt *time.Time) {
	shardId := s.getShardId(key)
	shard := s.shards[shardId]

	shard.mu.Lock()
	shard.data[Key(key)] = Value{Data: value, ExpiresAt: expiresAt}
	shard.mu.Unlock()
}

func (s *store) applyDel(key string) {
	shardId := s.getShardId(key)
	shard := s.shards[shardId]

	shard.mu.Lock()
	delete(shard.data, Key(key))
	shard.mu.Unlock()
}

func (s *store) applyFromWAL(rec parser.Value) error {
	if rec.Kind != parser.KindArray || rec.Null || len(rec.Array) == 0 {
		return fmt.Errorf("storage: wal record is not a non-empty array")
	}

	for _, item := range rec.Array {
		if item.Kind != parser.KindBulkString || item.Null {
			return fmt.Errorf("storage: wal record has non-bulk argument")
		}
	}

	op := string(rec.Array[0].Bulk)

	switch op {
	case wal.OpSet:
		if len(rec.Array) != 3 {
			return fmt.Errorf("storage: wal SET expects 3 args, got %d", len(rec.Array))
		}

		s.applySet(string(rec.Array[1].Bulk), rec.Array[2].Bulk, nil)

		return nil
	case wal.OpSetX:
		if len(rec.Array) != 4 {
			return fmt.Errorf("storage: wal SETX expects 4 args, got %d", len(rec.Array))
		}

		nano, err := strconv.ParseInt(string(rec.Array[3].Bulk), 10, 64)
		if err != nil {
			return fmt.Errorf("storage: wal SETX bad ttl: %w", err)
		}

		t := time.Unix(0, nano)
		s.applySet(string(rec.Array[1].Bulk), rec.Array[2].Bulk, &t)

		return nil
	case wal.OpDel:
		if len(rec.Array) != 2 {
			return fmt.Errorf("storage: wal DEL expects 2 args, got %d", len(rec.Array))
		}

		s.applyDel(string(rec.Array[1].Bulk))

		return nil
	default:
		return fmt.Errorf("storage: unknown wal op %q", op)
	}
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
