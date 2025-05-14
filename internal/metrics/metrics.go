package metrics

import (
	"ararauna/internal/config"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	vmm "github.com/VictoriaMetrics/metrics"
	"go.uber.org/zap"
)

var cmdWhitelist = []string{"PING", "GET", "SET", "DEL", "OTHER"}

type Recorder struct {
	cfg      *config.Config
	log      *zap.Logger
	set      *vmm.Set
	writeAll func(w io.Writer)
	pushOpts *vmm.PushOptions
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	cmdTotal   map[string]*vmm.Counter
	cmdLatency map[string]*vmm.Histogram
	cmdErrors  map[string]*vmm.Counter

	getHit     *vmm.Counter
	getMiss    *vmm.Counter
	getExpired *vmm.Counter

	setTotal   *vmm.Counter
	setWithTTL *vmm.Counter
	delTotal   *vmm.Counter

	walAppendTotal    *vmm.Counter
	walBytesTotal     *vmm.Counter
	walAppendDuration *vmm.Histogram
	walFsyncTotal     *vmm.Counter
	walFsyncDuration  *vmm.Histogram
	walSegmentTotal   *vmm.Counter

	connAccepted *vmm.Counter
	connActiveN  atomic.Int64

	gcEvictedTotal *vmm.Counter
}

func New(cfg *config.Config, log *zap.Logger) (*Recorder, error) {
	if !cfg.Metrics.Enabled {
		return nil, nil
	}

	r := &Recorder{
		cfg: cfg,
		log: log,
		set: vmm.NewSet(),
	}

	r.cmdTotal = make(map[string]*vmm.Counter, len(cmdWhitelist))
	r.cmdLatency = make(map[string]*vmm.Histogram, len(cmdWhitelist))
	r.cmdErrors = make(map[string]*vmm.Counter, len(cmdWhitelist))
	for _, name := range cmdWhitelist {
		lower := strings.ToLower(name)
		r.cmdTotal[name] = r.set.NewCounter(fmt.Sprintf(`ararauna_command_total{cmd="%s"}`, lower))
		r.cmdLatency[name] = r.set.NewHistogram(fmt.Sprintf(`ararauna_command_duration_seconds{cmd="%s"}`, lower))
		r.cmdErrors[name] = r.set.NewCounter(fmt.Sprintf(`ararauna_command_errors_total{cmd="%s"}`, lower))
	}

	r.getHit = r.set.NewCounter(`ararauna_get_hit_total`)
	r.getMiss = r.set.NewCounter(`ararauna_get_miss_total`)
	r.getExpired = r.set.NewCounter(`ararauna_get_expired_total`)

	r.setTotal = r.set.NewCounter(`ararauna_set_total`)
	r.setWithTTL = r.set.NewCounter(`ararauna_set_with_ttl_total`)
	r.delTotal = r.set.NewCounter(`ararauna_del_total`)

	r.walAppendTotal = r.set.NewCounter(`ararauna_wal_append_total`)
	r.walBytesTotal = r.set.NewCounter(`ararauna_wal_bytes_written_total`)
	r.walAppendDuration = r.set.NewHistogram(`ararauna_wal_append_duration_seconds`)
	r.walFsyncTotal = r.set.NewCounter(`ararauna_wal_fsync_total`)
	r.walFsyncDuration = r.set.NewHistogram(`ararauna_wal_fsync_duration_seconds`)
	r.walSegmentTotal = r.set.NewCounter(`ararauna_wal_segment_total`)

	r.connAccepted = r.set.NewCounter(`ararauna_conn_accepted_total`)
	r.set.NewGauge(`ararauna_conn_active`, func() float64 {
		return float64(r.connActiveN.Load())
	})

	r.gcEvictedTotal = r.set.NewCounter(`ararauna_gc_evicted_total`)

	r.writeAll = func(w io.Writer) {
		r.set.WritePrometheus(w)
		vmm.WriteProcessMetrics(w)
		vmm.WriteFDMetrics(w)
	}
	r.pushOpts = &vmm.PushOptions{
		Method:    http.MethodPost,
		WaitGroup: &r.wg,
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	if err := vmm.InitPushExtWithOptions(ctx, cfg.Metrics.PushURL, cfg.Metrics.PushInterval, r.writeAll, r.pushOpts); err != nil {
		cancel()
		return nil, fmt.Errorf("metrics: init push: %w", err)
	}

	return r, nil
}

func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.cancel()
	r.wg.Wait()

	cfg := r.cfg.Metrics
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = cfg.PushInterval / 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.push(ctx)
}
