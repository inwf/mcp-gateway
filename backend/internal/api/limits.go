package api

import (
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// concurrencyMiddleware caps how many requests are served at once.
//
// This is applied to the management API only, never to the MCP endpoint
// or the event stream. A long-lived stream occupies its slot for as long
// as the client stays connected, so counting streams here would let a
// handful of ordinary MCP clients exhaust the limit and wedge the
// management API — the opposite of the protection intended. Long-lived
// connections are bounded by the connection limit instead.
//
// A request over the limit is refused rather than queued: queueing would
// trade a fast, actionable rejection for a slow timeout, and the caller
// cannot tell the difference between a queue and a hang.
func concurrencyMiddleware(limit int, log *slog.Logger) gin.HandlerFunc {
	if limit <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	slots := make(chan struct{}, limit)

	return func(c *gin.Context) {
		select {
		case slots <- struct{}{}:
		default:
			log.Warn("refused a request because too many are already in flight",
				"requestId", RequestID(c), "limit", limit)
			fail(c, &Error{
				Code:    CodeTooManyRequests,
				Message: "too many requests are in flight; try again shortly",
			})
			return
		}

		// Releasing in a deferred call is what makes this drift-proof: it
		// runs whether the handler returns normally, panics, or is a
		// stream that ended when the client went away. There is no
		// completion callback that could be missed.
		defer func() { <-slots }()

		c.Next()
	}
}

// limitedListener caps how many connections are open at once.
//
// Connections are counted here rather than in a middleware because one
// connection carries many requests under keep-alive, and because a
// hijacked connection — a WebSocket — leaves the HTTP server's
// bookkeeping entirely while still holding a socket. Wrapping the
// listener is the one place that sees every connection for its whole
// life.
type limitedListener struct {
	net.Listener

	limit int
	log   *slog.Logger

	open atomic.Int64
}

// newLimitedListener wraps inner. A limit of zero or less imposes no cap.
func newLimitedListener(inner net.Listener, limit int, log *slog.Logger) net.Listener {
	if limit <= 0 {
		return inner
	}
	return &limitedListener{Listener: inner, limit: limit, log: log}
}

// Accept returns the next connection, refusing it if the listener is at
// its limit.
//
// A connection over the limit is accepted and then closed rather than
// left in the backlog. Leaving it queued would make a client wait for a
// timeout with no way to tell a busy server from an unreachable one,
// whereas closing it is immediate and unambiguous.
func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		if l.open.Add(1) > int64(l.limit) {
			l.open.Add(-1)
			l.log.Warn("refused a connection because the limit is reached",
				"remoteAddr", conn.RemoteAddr().String(), "limit", l.limit)
			conn.Close()
			continue
		}

		return &countedConn{Conn: conn, listener: l}, nil
	}
}

// Open reports how many connections are currently held.
func (l *limitedListener) Open() int { return int(l.open.Load()) }

// countedConn releases its slot when it closes.
type countedConn struct {
	net.Conn
	listener *limitedListener

	// once guarantees the slot is released exactly once. The HTTP server
	// closes a connection it is finished with, and a hijacked connection
	// is closed by whoever took it over; both paths can run, and a double
	// release would let the count drift below zero and raise the
	// effective limit.
	once sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() { c.listener.open.Add(-1) })
	return c.Conn.Close()
}
