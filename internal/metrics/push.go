package metrics

import (
	"context"
	"fmt"

	vmm "github.com/VictoriaMetrics/metrics"
)

func (r *Recorder) push(ctx context.Context) error {
	if err := vmm.PushMetricsExt(ctx, r.cfg.Metrics.PushURL, r.writeAll, r.pushOpts); err != nil {
		return fmt.Errorf("metrics: push: %w", err)
	}
	return nil
}
