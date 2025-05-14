package metrics

import "time"

func (r *Recorder) ObserveCommand(name string, dur time.Duration, errored bool) {
	if r == nil {
		return
	}
	key := name
	if _, ok := r.cmdLatency[name]; !ok {
		key = "OTHER"
	}
	r.cmdTotal[key].Inc()
	r.cmdLatency[key].Update(dur.Seconds())
	if errored {
		r.cmdErrors[key].Inc()
	}
}

func (r *Recorder) IncGetHit() {
	if r == nil {
		return
	}
	r.getHit.Inc()
}

func (r *Recorder) IncGetMiss() {
	if r == nil {
		return
	}
	r.getMiss.Inc()
}

func (r *Recorder) IncGetExpired() {
	if r == nil {
		return
	}
	r.getExpired.Inc()
}

func (r *Recorder) IncSet(withTTL bool) {
	if r == nil {
		return
	}
	r.setTotal.Inc()
	if withTTL {
		r.setWithTTL.Inc()
	}
}

func (r *Recorder) IncDel() {
	if r == nil {
		return
	}
	r.delTotal.Inc()
}

func (r *Recorder) IncWALAppend() {
	if r == nil {
		return
	}
	r.walAppendTotal.Inc()
}

func (r *Recorder) AddWALBytes(n int64) {
	if r == nil {
		return
	}
	r.walBytesTotal.Add(int(n))
}

func (r *Recorder) ObserveWALAppend(d time.Duration) {
	if r == nil {
		return
	}
	r.walAppendDuration.Update(d.Seconds())
}

func (r *Recorder) IncWALFsync() {
	if r == nil {
		return
	}
	r.walFsyncTotal.Inc()
}

func (r *Recorder) ObserveWALFsync(d time.Duration) {
	if r == nil {
		return
	}
	r.walFsyncDuration.Update(d.Seconds())
}

func (r *Recorder) IncWALSegment() {
	if r == nil {
		return
	}
	r.walSegmentTotal.Inc()
}

func (r *Recorder) IncConnAccepted() {
	if r == nil {
		return
	}
	r.connAccepted.Inc()
}

func (r *Recorder) IncConnActive() {
	if r == nil {
		return
	}
	r.connActiveN.Add(1)
}

func (r *Recorder) DecConnActive() {
	if r == nil {
		return
	}
	r.connActiveN.Add(-1)
}

func (r *Recorder) AddGCEvicted(n int64) {
	if r == nil {
		return
	}
	r.gcEvictedTotal.Add(int(n))
}
