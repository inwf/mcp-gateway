package gateway

import (
	"sync"
	"time"
)

// afterFunc schedules f to run after d. Tests substitute it to control
// when the trailing edge fires.
type afterFunc func(d time.Duration, f func())

func realAfter(d time.Duration, f func()) { time.AfterFunc(d, f) }

// debouncer collapses a burst of triggers into a bounded number of runs.
//
// It fires on both edges: immediately on the first trigger, and once
// more at the end of the quiet window if further triggers arrived. The
// leading edge is what keeps a single change from being delayed, and it
// is also what stops a server that changes continuously from starving
// the action forever — which is what a purely trailing debounce would
// do.
type debouncer struct {
	delay  time.Duration
	action func()
	after  afterFunc

	mu      sync.Mutex
	cooling bool
	pending bool
	stopped bool
}

func newDebouncer(delay time.Duration, action func(), after afterFunc) *debouncer {
	if after == nil {
		after = realAfter
	}
	return &debouncer{delay: delay, action: action, after: after}
}

// trigger requests a run.
func (d *debouncer) trigger() {
	d.mu.Lock()

	if d.stopped {
		d.mu.Unlock()
		return
	}
	// With no delay configured there is nothing to coalesce, and the
	// caller asked for every change to be applied at once.
	if d.delay <= 0 {
		d.mu.Unlock()
		d.action()
		return
	}
	if d.cooling {
		d.pending = true
		d.mu.Unlock()
		return
	}
	d.cooling = true
	d.mu.Unlock()

	// Outside the lock: the action may take a while, and a trigger from
	// another goroutine must not wait on it.
	d.action()
	d.after(d.delay, d.expire)
}

func (d *debouncer) expire() {
	d.mu.Lock()

	if d.stopped {
		d.mu.Unlock()
		return
	}
	if !d.pending {
		d.cooling = false
		d.mu.Unlock()
		return
	}
	d.pending = false
	d.mu.Unlock()

	d.action()
	d.after(d.delay, d.expire)
}

// stop prevents further runs. A timer already scheduled will find the
// debouncer stopped and do nothing, so shutting down does not have to
// wait for the window to close.
func (d *debouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
}
