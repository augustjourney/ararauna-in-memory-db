package metrics

import (
	"testing"
	"time"
)

func TestNilReceiverNoPanic(t *testing.T) {
	var r *Recorder

	r.ObserveCommand("GET", time.Millisecond, false)
	r.ObserveCommand("UNKNOWN", time.Millisecond, true)
	r.IncGetHit()
	r.IncGetMiss()
	r.IncGetExpired()
	r.IncSet(false)
	r.IncSet(true)
	r.IncDel()
	r.IncWALAppend()
	r.AddWALBytes(100)
	r.ObserveWALAppend(time.Millisecond)
	r.IncWALFsync()
	r.ObserveWALFsync(time.Millisecond)
	r.IncWALSegment()
	r.IncConnAccepted()
	r.IncConnActive()
	r.DecConnActive()
	r.AddGCEvicted(10)
}

func TestCloseNil(t *testing.T) {
	var r *Recorder
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
