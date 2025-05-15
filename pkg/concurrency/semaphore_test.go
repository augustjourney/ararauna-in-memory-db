package concurrency

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSemaphore_TryAcquire(t *testing.T) {
	s := NewSemaphore(2)

	assert.True(t, s.TryAcquire())
	assert.True(t, s.TryAcquire())
	assert.False(t, s.TryAcquire())

	s.Release()
	assert.True(t, s.TryAcquire())
	assert.False(t, s.TryAcquire())
}

func TestSemaphore_TryAcquireZeroValueAlwaysSucceeds(t *testing.T) {
	var s Semaphore

	for i := 0; i < 1000; i++ {
		assert.True(t, s.TryAcquire())
	}
}

func TestSemaphore_ReleaseZeroValueIsNoop(t *testing.T) {
	var s Semaphore
	assert.NotPanics(t, func() { s.Release() })
}
