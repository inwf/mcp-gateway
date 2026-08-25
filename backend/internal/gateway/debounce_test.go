package gateway

import (
	"sync"
	"testing"
	"time"
)

// fakeClock collects the callbacks a debouncer schedules and fires them
// on demand, so the window can be closed without waiting for it.
type fakeClock struct {
	mu      sync.Mutex
	pending []func()
	delays  []time.Duration
}

func (c *fakeClock) after(d time.Duration, f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delays = append(c.delays, d)
	c.pending = append(c.pending, f)
}

// fire runs the callbacks scheduled so far.
func (c *fakeClock) fire() {
	c.mu.Lock()
	due := c.pending
	c.pending = nil
	c.mu.Unlock()

	for _, f := range due {
		f()
	}
}

func (c *fakeClock) scheduled() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// counter counts how many times the debounced action ran.
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) run() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
}

func (c *counter) value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// A single change must not be held back: there is nothing to coalesce it
// with.
func TestFirstTriggerRunsImmediately(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Second, runs.run, clock.after)

	d.trigger()

	if got := runs.value(); got != 1 {
		t.Errorf("ran %d times, want 1 straight away", got)
	}
}

// The point of the whole thing: a burst produces one run now and one at
// the end, not one per trigger.
func TestABurstCollapsesToTwoRuns(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Second, runs.run, clock.after)

	for i := 0; i < 20; i++ {
		d.trigger()
	}
	if got := runs.value(); got != 1 {
		t.Fatalf("ran %d times during the burst, want 1", got)
	}

	clock.fire()

	if got := runs.value(); got != 2 {
		t.Errorf("ran %d times in total, want 2: one leading and one trailing", got)
	}
}

// With nothing pending, closing the window must not produce a run that
// nobody asked for.
func TestAQuietWindowDoesNotRunAgain(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Second, runs.run, clock.after)

	d.trigger()
	clock.fire()

	if got := runs.value(); got != 1 {
		t.Errorf("ran %d times, want 1; the trailing edge fired with nothing pending", got)
	}
	if got := clock.scheduled(); got != 0 {
		t.Errorf("%d timers are still scheduled, want none once things go quiet", got)
	}
}

// A source that changes without pause would starve a purely trailing
// debounce. Every window has to produce a run.
func TestContinuousChangeKeepsRunning(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Second, runs.run, clock.after)

	for round := 0; round < 5; round++ {
		d.trigger()
		d.trigger()
		clock.fire()
	}

	// One leading run, then one per window.
	if got := runs.value(); got < 5 {
		t.Errorf("ran %d times over 5 windows, want at least 5", got)
	}
}

func TestTriggerAfterQuietStartsAFreshWindow(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Second, runs.run, clock.after)

	d.trigger()
	clock.fire() // window closes with nothing pending

	d.trigger() // a new change much later

	if got := runs.value(); got != 2 {
		t.Errorf("ran %d times, want 2; the second change was not applied at once", got)
	}
}

// Zero means the caller does not want coalescing.
func TestZeroDelayRunsEveryTime(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(0, runs.run, clock.after)

	for i := 0; i < 5; i++ {
		d.trigger()
	}

	if got := runs.value(); got != 5 {
		t.Errorf("ran %d times, want 5", got)
	}
	if got := clock.scheduled(); got != 0 {
		t.Errorf("scheduled %d timers with no delay configured", got)
	}
}

// Shutting down must not have to wait for the window to close.
func TestStopPreventsFurtherRuns(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Second, runs.run, clock.after)

	d.trigger()
	d.trigger() // leaves something pending
	d.stop()

	clock.fire() // the timer fires after the stop

	if got := runs.value(); got != 1 {
		t.Errorf("ran %d times, want only the one before stopping", got)
	}

	d.trigger()
	if got := runs.value(); got != 1 {
		t.Errorf("ran %d times, want triggers after stopping to be ignored", got)
	}
}

func TestStopIsRepeatable(t *testing.T) {
	d := newDebouncer(time.Second, func() {}, (&fakeClock{}).after)

	for i := 0; i < 3; i++ {
		d.stop()
	}
}

// The action can be slow, and a trigger from another goroutine must not
// wait on it.
func TestTriggeringConcurrently(t *testing.T) {
	clock := &fakeClock{}
	runs := &counter{}
	d := newDebouncer(time.Millisecond, runs.run, clock.after)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				d.trigger()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 50; n++ {
			clock.fire()
		}
	}()
	wg.Wait()

	if runs.value() == 0 {
		t.Error("the action never ran")
	}
}

// The real timer has to actually fire, which the fake clock cannot show.
func TestRealTimerFires(t *testing.T) {
	done := make(chan struct{}, 2)
	d := newDebouncer(10*time.Millisecond, func() { done <- struct{}{} }, nil)

	d.trigger() // leading edge
	d.trigger() // leaves something pending for the trailing edge

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 2 runs happened", i)
		}
	}
}
