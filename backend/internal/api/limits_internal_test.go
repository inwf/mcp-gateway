package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// limited builds an engine shaped the way the router shapes it: the
// concurrency middleware in front of a handler under test.
func limited(limit int, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(requestIDMiddleware(), concurrencyMiddleware(limit, slog.New(discardHandler{})))
	engine.GET("/api/thing", handler)
	return engine
}

// call performs one request against engine.
func call(engine *gin.Engine) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/thing", nil))
	return recorder
}

func TestRequestsUnderTheLimitAreServed(t *testing.T) {
	engine := limited(2, func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for i := 0; i < 5; i++ {
		if got := call(engine).Code; got != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, got)
		}
	}
}

// A request over the limit is refused rather than queued: a caller
// cannot tell a queue from a hang, and a fast rejection is actionable.
func TestARequestOverTheLimitIsRefused(t *testing.T) {
	release := make(chan struct{})
	occupied := make(chan struct{}, 1)

	engine := limited(1, func(c *gin.Context) {
		occupied <- struct{}{}
		<-release
		c.String(http.StatusOK, "ok")
	})

	go call(engine)
	<-occupied // the only slot is taken

	recorder := call(engine)
	close(release)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}

	var envelope Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.Bytes(), err)
	}
	if envelope.Error.Code != CodeTooManyRequests {
		t.Errorf("code = %q, want %q", envelope.Error.Code, CodeTooManyRequests)
	}
}

// The slot has to come back, or the limit ratchets down to zero and the
// API stops answering entirely after enough requests.
func TestASlotIsReleasedWhenTheHandlerReturns(t *testing.T) {
	engine := limited(1, func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for i := 0; i < 50; i++ {
		if got := call(engine).Code; got != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200; the slot was not released", i, got)
		}
	}
}

// A panicking handler must not keep its slot: a handler with a bug would
// otherwise take the API down one request at a time.
func TestASlotIsReleasedAfterAPanic(t *testing.T) {
	engine := limited(1, func(*gin.Context) { panic("boom") })

	for i := 0; i < 3; i++ {
		func() {
			defer func() { recover() }()
			call(engine)
		}()
	}

	// The slot is free, so a request that does not panic is served.
	engine2 := limited(1, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	if got := call(engine2).Code; got != http.StatusOK {
		t.Errorf("status = %d, want 200", got)
	}
}

// This is the case the release has to survive: a long-lived stream holds
// its slot for as long as the client stays connected, and the slot has
// to come back when the stream ends rather than being lost.
//
// A completion callback could be missed here, which is what makes a
// deferred release the right mechanism: the middleware's frame is still
// on the stack for the whole life of the stream.
func TestASlotIsReleasedWhenALongLivedStreamEnds(t *testing.T) {
	release := make(chan struct{})
	occupied := make(chan struct{}, 1)

	engine := limited(1, func(c *gin.Context) {
		c.Writer.WriteHeader(http.StatusOK)
		c.Writer.Flush()
		occupied <- struct{}{}
		<-release // the stream stays open
	})

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		call(engine)
	}()
	<-occupied

	// While the stream is open the limit is in force.
	if got := call(engine).Code; got != http.StatusTooManyRequests {
		t.Fatalf("status = %d while a stream held the only slot, want 429", got)
	}

	// The stream ends, as it would when the client goes away or the idle
	// timeout closes it.
	close(release)
	<-streamDone

	if got := call(engine).Code; got != http.StatusOK {
		t.Errorf("status = %d after the stream ended, want 200; the slot was lost", got)
	}
}

// A limit of zero or less means no limit, which is what a configuration
// that does not set one should produce.
func TestNoLimitMeansNoLimit(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	var inFlight sync.WaitGroup
	engine := limited(0, func(c *gin.Context) {
		inFlight.Done()
		<-release
		c.String(http.StatusOK, "ok")
	})

	inFlight.Add(20)
	for i := 0; i < 20; i++ {
		go call(engine)
	}

	waited := make(chan struct{})
	go func() { inFlight.Wait(); close(waited) }()

	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("requests were held back although no limit was configured")
	}
}

// Slots are taken and released from many goroutines at once, which is
// the normal condition rather than an edge case.
func TestTheLimiterIsSafeUnderConcurrency(t *testing.T) {
	engine := limited(4, func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	var wg sync.WaitGroup
	served, refused := make(chan struct{}, 200), make(chan struct{}, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if call(engine).Code == http.StatusOK {
				served <- struct{}{}
			} else {
				refused <- struct{}{}
			}
		}()
	}
	wg.Wait()

	// Every request is accounted for, and nothing deadlocked.
	if total := len(served) + len(refused); total != 200 {
		t.Errorf("accounted for %d of 200 requests", total)
	}
	if len(served) == 0 {
		t.Error("nothing was served at all")
	}
}
