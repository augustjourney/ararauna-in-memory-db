package wal

import (
	"ararauna/internal/config"
	"ararauna/internal/parser"
	"ararauna/internal/toolbox"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestToolbox(dir string, policy SyncPolicy, segmentSize int64) *toolbox.Toolbox {
	cfg := config.Default()
	cfg.WAL.Enabled = true
	cfg.WAL.Dir = dir
	cfg.WAL.SegmentSize = segmentSize
	cfg.WAL.SyncPolicy = string(policy)
	return toolbox.New(cfg, zap.NewNop())
}

func openTestWAL(t *testing.T, policy SyncPolicy, segmentSize int64) (*Writer, string) {
	t.Helper()
	dir := t.TempDir()

	w, err := New(newTestToolbox(dir, policy, segmentSize))
	require.NoError(t, err)

	t.Cleanup(func() { _ = w.Close() })

	return w, dir
}

func collectReplay(t *testing.T, dir string) []parser.Value {
	t.Helper()

	w, err := New(newTestToolbox(dir, SyncAlways, 1<<20))
	require.NoError(t, err)
	defer w.Close()

	var got []parser.Value
	require.NoError(t, w.Replay(func(v parser.Value) error {
		got = append(got, v)
		return nil
	}))

	return got
}

func TestAppendReplayRoundTrip(t *testing.T) {
	w, dir := openTestWAL(t, SyncAlways, 1<<20)

	want := []parser.Value{
		RecordSet("foo", []byte("bar")),
		RecordSetX("k1", []byte("v1"), 1234567890),
		RecordDel("foo"),
		RecordSet("hello", []byte("world")),
	}

	for _, rec := range want {
		require.NoError(t, w.Append(rec))
	}
	require.NoError(t, w.Close())

	got := collectReplay(t, dir)
	assert.Equal(t, want, got)
}

func TestSegmentRotation(t *testing.T) {
	w, dir := openTestWAL(t, SyncAlways, 64)

	const n = 30
	for i := range n {
		require.NoError(t, w.Append(RecordSet("k", []byte{byte(i)})))
	}
	require.NoError(t, w.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	segments := 0
	for _, e := range entries {
		if _, ok := parseSegmentSeq(e.Name()); ok {
			segments++
		}
	}

	assert.Greater(t, segments, 1, "expected multiple segments")

	got := collectReplay(t, dir)
	require.Len(t, got, n)

	for i, v := range got {
		require.Equal(t, parser.KindArray, v.Kind)
		require.Len(t, v.Array, 3)
		assert.Equal(t, []byte{byte(i)}, v.Array[2].Bulk)
	}
}

func TestTruncatedTail(t *testing.T) {
	w, dir := openTestWAL(t, SyncAlways, 1<<20)

	for i := range 3 {
		require.NoError(t, w.Append(RecordSet("k", []byte{byte(i)})))
	}
	require.NoError(t, w.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	last := filepath.Join(dir, entries[0].Name())

	f, err := os.OpenFile(last, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, f.Sync())
	require.NoError(t, err)

	_, err = f.Write([]byte("*3\r\n$3\r\nSET\r\n$1\r\nx\r\n$3\r\nab"))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got := collectReplay(t, dir)
	require.Len(t, got, 3)

	for i, v := range got {
		assert.Equal(t, []byte{byte(i)}, v.Array[2].Bulk)
	}
}

func TestSyncPolicies(t *testing.T) {
	for _, p := range []SyncPolicy{SyncAlways, SyncEverysec, SyncNo} {
		t.Run(string(p), func(t *testing.T) {
			w, dir := openTestWAL(t, p, 1<<20)
			require.NoError(t, w.Append(RecordSet("foo", []byte("bar"))))
			require.NoError(t, w.Append(RecordDel("foo")))
			require.NoError(t, w.Close())

			got := collectReplay(t, dir)
			require.Len(t, got, 2)
			assert.Equal(t, "SET", string(got[0].Array[0].Bulk))
			assert.Equal(t, "DEL", string(got[1].Array[0].Bulk))
		})
	}
}

func TestReopenContinuesNumbering(t *testing.T) {
	dir := t.TempDir()
	tb := newTestToolbox(dir, SyncAlways, 64)

	w1, err := New(tb)
	require.NoError(t, err)
	for i := range 5 {
		require.NoError(t, w1.Append(RecordSet("k", []byte{byte(i)})))
	}
	require.NoError(t, w1.Close())

	w2, err := New(tb)
	require.NoError(t, err)
	for i := range 5 {
		require.NoError(t, w2.Append(RecordSet("k", []byte{byte(100 + i)})))
	}
	require.NoError(t, w2.Close())

	got := collectReplay(t, dir)
	assert.Len(t, got, 10)
	for i := range 5 {
		assert.Equal(t, []byte{byte(i)}, got[i].Array[2].Bulk)
	}
	for i := range 5 {
		assert.Equal(t, []byte{byte(100 + i)}, got[5+i].Array[2].Bulk)
	}
}
