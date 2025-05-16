package wal

import (
	"ararauna/internal/parser"
	"ararauna/internal/toolbox"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	segmentPrefix = "wal-"
	segmentSuffix = ".log"
	segmentDigits = 10
)

type SyncPolicy string

const (
	SyncAlways   SyncPolicy = "always"
	SyncEverysec SyncPolicy = "everysec"
	SyncNo       SyncPolicy = "no"
)

const (
	OpSet  = "SET"
	OpSetX = "SETX"
	OpDel  = "DEL"
)

func RecordSet(key string, value []byte) parser.Value {
	return parser.Array([]parser.Value{
		parser.Bulk([]byte(OpSet)),
		parser.Bulk([]byte(key)),
		parser.Bulk(value),
	})
}

func RecordSetX(key string, value []byte, expiresUnixNano int64) parser.Value {
	return parser.Array([]parser.Value{
		parser.Bulk([]byte(OpSetX)),
		parser.Bulk([]byte(key)),
		parser.Bulk(value),
		parser.Bulk([]byte(strconv.FormatInt(expiresUnixNano, 10))),
	})
}

func RecordDel(key string) parser.Value {
	return parser.Array([]parser.Value{
		parser.Bulk([]byte(OpDel)),
		parser.Bulk([]byte(key)),
	})
}

type Writer struct {
	tb          *toolbox.Toolbox
	dir         string
	segmentSize int64
	syncPolicy  SyncPolicy

	mu    sync.Mutex
	file  *os.File
	bufw  *bufio.Writer
	size  int64
	seq   uint64
	dirty bool

	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
}

func New(tb *toolbox.Toolbox) (*Writer, error) {
	cfg := tb.Cfg.WAL
	if !cfg.Enabled {
		return nil, nil
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("wal: mkdir %s: %w", cfg.Dir, err)
	}

	last, err := lastSegmentSeq(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("wal: get last segment seq: %w", err)
	}

	w := &Writer{
		tb:          tb,
		dir:         cfg.Dir,
		segmentSize: cfg.SegmentSize,
		syncPolicy:  SyncPolicy(cfg.SyncPolicy),
		seq:         last,
	}

	if w.syncPolicy == SyncEverysec {
		w.stopCh = make(chan struct{})
		w.doneCh = make(chan struct{})
		go w.runEverysec()
	}

	return w, nil
}

func (w *Writer) Append(rec parser.Value) error {
	if w == nil {
		return nil
	}
	start := time.Now()

	var buf bytes.Buffer
	if err := parser.Encode(rec, &buf); err != nil {
		return fmt.Errorf("wal: parser encode: %w", err)
	}

	payload := buf.Bytes()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.openNextSegmentLocked(); err != nil {
			return fmt.Errorf("wal: open next segment: %w", err)
		}
	}

	if _, err := w.bufw.Write(payload); err != nil {
		return fmt.Errorf("wal: write: %w", err)
	}

	w.size += int64(len(payload))
	w.dirty = true
	w.tb.Metrics.IncWALAppend()
	w.tb.Metrics.AddWALBytes(int64(len(payload)))

	if err := w.bufw.Flush(); err != nil {
		return fmt.Errorf("wal: flush: %w", err)
	}

	if w.syncPolicy == SyncAlways {
		syncStart := time.Now()
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("wal: fsync: %w", err)
		}
		w.dirty = false
		w.tb.Metrics.IncWALFsync()
		w.tb.Metrics.ObserveWALFsync(time.Since(syncStart))
	}

	if w.size >= w.segmentSize {
		if err := w.rotateLocked(); err != nil {
			return fmt.Errorf("wal: rotate: %w", err)
		}
	}
	w.tb.Metrics.ObserveWALAppend(time.Since(start))
	return nil
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}

	if w.syncPolicy == SyncEverysec && w.stopCh != nil {
		w.stopOnce.Do(func() {
			close(w.stopCh)
			<-w.doneCh
		})
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}

	var firstErr error
	if err := w.bufw.Flush(); err != nil {
		firstErr = fmt.Errorf("wal: flush on close: %w", err)
	}

	if w.dirty {
		if err := w.file.Sync(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("wal: fsync on close: %w", err)
		}
		w.dirty = false
	}

	if err := w.file.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("wal: close file: %w", err)
	}

	w.file = nil
	w.bufw = nil

	return firstErr
}

func (w *Writer) Replay(apply func(parser.Value) error) error {
	if w == nil {
		return nil
	}

	segments, err := listSegments(w.dir)
	if err != nil {
		return fmt.Errorf("wal: list segments: %w", err)
	}

	for _, seg := range segments {
		if err := w.replaySegment(filepath.Join(w.dir, seg), apply); err != nil {
			return fmt.Errorf("wal: replay segment: %w", err)
		}
	}

	return nil
}

func (w *Writer) openNextSegmentLocked() error {
	w.seq++

	name := segmentName(w.seq)
	path := filepath.Join(w.dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("wal: open segment %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("wal: stat segment %s: %w", path, err)
	}

	w.file = f
	w.bufw = bufio.NewWriter(f)
	w.size = info.Size()

	return nil
}

func (w *Writer) rotateLocked() error {
	if err := w.bufw.Flush(); err != nil {
		return fmt.Errorf("wal: flush on rotate: %w", err)
	}

	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("wal: fsync on rotate: %w", err)
	}

	w.dirty = false

	if err := w.file.Close(); err != nil {
		return fmt.Errorf("wal: close on rotate: %w", err)
	}

	w.file = nil
	w.bufw = nil
	w.size = 0

	if err := w.openNextSegmentLocked(); err != nil {
		return err
	}
	w.tb.Metrics.IncWALSegment()
	return nil
}

func (w *Writer) runEverysec() {
	defer close(w.doneCh)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			if w.dirty && w.file != nil {
				syncStart := time.Now()
				if err := w.file.Sync(); err != nil {
					w.tb.Logger.Warn("wal: background fsync failed", zap.Error(err))
				} else {
					w.dirty = false
					w.tb.Metrics.IncWALFsync()
					w.tb.Metrics.ObserveWALFsync(time.Since(syncStart))
				}
			}
			w.mu.Unlock()
		}
	}
}

func segmentName(seq uint64) string {
	return fmt.Sprintf("%s%0*d%s", segmentPrefix, segmentDigits, seq, segmentSuffix)
}

func parseSegmentSeq(name string) (uint64, bool) {
	if !strings.HasPrefix(name, segmentPrefix) || !strings.HasSuffix(name, segmentSuffix) {
		return 0, false
	}

	mid := strings.TrimSuffix(strings.TrimPrefix(name, segmentPrefix), segmentSuffix)

	n, err := strconv.ParseUint(mid, 10, 64)
	if err != nil {
		return 0, false
	}

	return n, true
}

func listSegments(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("wal: read dir %s: %w", dir, err)
	}

	type seg struct {
		name string
		seq  uint64
	}

	var out []seg
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		seq, ok := parseSegmentSeq(e.Name())
		if !ok {
			continue
		}

		out = append(out, seg{name: e.Name(), seq: seq})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })

	names := make([]string, len(out))
	for i, s := range out {
		names[i] = s.name
	}

	return names, nil
}

func lastSegmentSeq(dir string) (uint64, error) {
	segs, err := listSegments(dir)
	if err != nil {
		return 0, err
	}

	if len(segs) == 0 {
		return 0, nil
	}

	seq, _ := parseSegmentSeq(segs[len(segs)-1])

	return seq, nil
}

func (w *Writer) replaySegment(path string, apply func(parser.Value) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("wal: open %s: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)

	for {
		if _, err := r.Peek(1); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return fmt.Errorf("wal: peek %s: %w", path, err)
		}

		v, err := parser.Decode(r)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				w.tb.Logger.Warn("wal: truncated tail in segment, skipping", zap.String("file", path))
				return nil
			}
			return fmt.Errorf("wal: decode %s: %w", path, err)
		}

		if err := apply(v); err != nil {
			return fmt.Errorf("wal: apply %s: %w", path, err)
		}
	}
}
