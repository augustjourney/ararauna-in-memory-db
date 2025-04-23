package storage

import (
	"ararauna/internal/config"
	"ararauna/internal/errs"
	"ararauna/pkg/ptr"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStorage(t *testing.T) *store {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return New(ctx, &config.Config{Storage: config.Storage{Shards: 4}})
}

func (s *store) totalKeys() int {
	total := 0
	for _, sh := range s.shards {
		sh.mu.RLock()
		total += len(sh.data)
		sh.mu.RUnlock()
	}
	return total
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, errs.ErrNotFound)
}

func TestSetGet_Roundtrip(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "foo", []byte("bar"), nil))

	got, err := s.Get(ctx, "foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), got.Data)
	assert.Nil(t, got.ExpiresAt)
}

func TestSet_Overwrite(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "k", []byte("v1"), nil))
	require.NoError(t, s.Set(ctx, "k", []byte("v2"), nil))

	got, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), got.Data)
}

func TestGet_TTL(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt *time.Time
		wantErr   error
	}{
		{"no ttl", nil, nil},
		{"future ttl", ptr.Of(time.Now().Add(time.Hour)), nil},
		{"past ttl", ptr.Of(time.Now().Add(-time.Hour)), errs.ErrNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStorage(t)
			ctx := context.Background()

			require.NoError(t, s.Set(ctx, "k", []byte("v"), tc.expiresAt))

			_, err := s.Get(ctx, "k")
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestDel_Found(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "k", []byte("v"), nil))

	deleted, err := s.Del(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "k", deleted)

	_, err = s.Get(ctx, "k")
	assert.ErrorIs(t, err, errs.ErrNotFound)
}

func TestDel_NotFound(t *testing.T) {
	s := newTestStorage(t)

	deleted, err := s.Del(context.Background(), "missing")
	assert.ErrorIs(t, err, errs.ErrNotFound)
	assert.Empty(t, deleted)
}

func TestContextCancelled(t *testing.T) {
	s := newTestStorage(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, getErr := s.Get(ctx, "k")
	assert.ErrorIs(t, getErr, context.Canceled)

	assert.ErrorIs(t, s.Set(ctx, "k", []byte("v"), nil), context.Canceled)

	_, delErr := s.Del(ctx, "k")
	assert.ErrorIs(t, delErr, context.Canceled)
}

func TestGC_SweepShardDeletesExpired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "alive", []byte("v"), ptr.Of(time.Now().Add(time.Hour))))
	require.NoError(t, s.Set(ctx, "dead", []byte("v"), ptr.Of(time.Now().Add(-time.Hour))))
	require.NoError(t, s.Set(ctx, "forever", []byte("v"), nil))

	done := make(chan struct{})
	for i := range s.shardsCount {
		s.gcSweepShard(i, done)
	}

	assert.Equal(t, 2, s.totalKeys(), "only alive + forever should remain")
}

func TestGC_TickAdvancesCursor(t *testing.T) {
	s := newTestStorage(t)
	require.Zero(t, s.gcCursor)

	s.gcTick(context.Background())

	assert.Zero(t, s.gcCursor, "cursor wraps around after full rotation")
}

func TestGC_SweepShardStopsOnDeadline(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	for i := range 200 {
		require.NoError(t, s.Set(ctx, fmt.Sprintf("k%d", i), []byte("v"), &past))
	}

	var idx int
	for i, sh := range s.shards {
		sh.mu.RLock()
		n := len(sh.data)
		sh.mu.RUnlock()
		if n > 1 {
			idx = i
			break
		}
	}

	done := make(chan struct{})
	close(done)

	before := len(s.shards[idx].data)
	s.gcSweepShard(idx, done)
	after := len(s.shards[idx].data)

	assert.Equal(t, before, after, "sweep must bail on first iter, deleting nothing")
}

func TestGC_TickRespectsCtxCancel(t *testing.T) {
	s := newTestStorage(t)
	bgCtx := context.Background()

	past := time.Now().Add(-time.Hour)
	for i := range 200 {
		require.NoError(t, s.Set(bgCtx, fmt.Sprintf("k%d", i), []byte("v"), &past))
	}

	ctx, cancel := context.WithCancel(bgCtx)
	cancel()

	cursorBefore := s.gcCursor
	totalBefore := s.totalKeys()

	s.gcTick(ctx)

	assert.Equal(t, cursorBefore, s.gcCursor, "cursor must not advance under cancelled ctx")
	assert.Equal(t, totalBefore, s.totalKeys(), "no keys must be deleted under cancelled ctx")
}

func TestGC_BackgroundWorkerCleansExpired(t *testing.T) {
	cfg := &config.Config{Storage: config.Storage{
		Shards:     4,
		GCInterval: 10 * time.Millisecond,
		GCBudget:   5 * time.Millisecond,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := New(ctx, cfg)

	past := time.Now().Add(-time.Hour)
	for i := range 50 {
		require.NoError(t, s.Set(ctx, fmt.Sprintf("k%d", i), []byte("v"), &past))
	}

	assert.Eventually(t, func() bool {
		return s.totalKeys() == 0
	}, time.Second, 20*time.Millisecond, "background GC should clean all expired keys")
}

func TestConcurrentSetGet(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	const goroutines = 16
	const perGoroutine = 500

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range perGoroutine {
				key := fmt.Sprintf("g%d-k%d", g, i)
				val := []byte(key)
				if !assert.NoError(t, s.Set(ctx, key, val, nil)) {
					return
				}
				got, err := s.Get(ctx, key)
				if !assert.NoError(t, err) {
					return
				}
				if !assert.Equal(t, val, got.Data) {
					return
				}
			}
		}(g)
	}
	wg.Wait()
}
