package controller

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DedupRecorder wraps a record.EventRecorder to suppress duplicate
// (reason, message) Events emitted within dedupWindow of each other.
type DedupRecorder struct {
	inner       record.EventRecorder
	dedupWindow time.Duration

	mu   sync.Mutex
	seen map[string]time.Time // key = reason + "\x00" + message
}

// NewDedupRecorder constructs a DedupRecorder wrapping inner.
func NewDedupRecorder(inner record.EventRecorder, dedupWindow time.Duration) *DedupRecorder {
	return &DedupRecorder{
		inner:       inner,
		dedupWindow: dedupWindow,
		seen:        make(map[string]time.Time),
	}
}

// Event behaves like EventRecorder.Event but suppresses near-duplicates.
func (d *DedupRecorder) Event(obj client.Object, eventType, reason, message string) {
	d.mu.Lock()
	key := reason + "\x00" + message
	now := time.Now()
	if t, ok := d.seen[key]; ok && now.Sub(t) < d.dedupWindow {
		d.mu.Unlock()
		return
	}
	d.seen[key] = now
	d.mu.Unlock()

	// Type assertion needed because record.EventRecorder takes runtime.Object.
	if ro, ok := obj.(runtime.Object); ok {
		d.inner.Event(ro, eventType, reason, message)
	}
}
