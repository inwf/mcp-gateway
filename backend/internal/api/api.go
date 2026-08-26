// Package api serves the management HTTP API, the aggregated MCP
// endpoint and, in a later step, the web UI — all on one listener.
//
// Every failure leaves through the same envelope (see errors.go), so a
// client has one shape to handle rather than one per endpoint.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mcphub/internal/config"
)

// Route prefixes. These are constants because the middleware that
// applies to each differs, and because the frontend's API client and the
// static-file fallback both have to agree with them.
const (
	// APIPrefix covers the management API.
	APIPrefix = "/api"

	// MCPPath is the aggregated MCP endpoint that clients connect to.
	MCPPath = "/mcp"
)

// Options configures an [API].
type Options struct {
	// Version identifies this build to clients.
	Version string

	// Logger receives access log lines and rejections.
	Logger *slog.Logger

	// Security carries the allowlist and the concurrency limits.
	Security config.Security

	// MCP serves the aggregated MCP endpoint. It is supplied as a plain
	// handler so that this package does not depend on the gateway.
	MCP http.Handler

	// Now overrides the clock, for tests that assert on uptime.
	Now func() time.Time
}

// API is the HTTP surface of mcphub.
type API struct {
	opts    Options
	log     *slog.Logger
	engine  *gin.Engine
	started time.Time

	// connections is set when a connection limit is in force, so that the
	// health endpoint can report the count.
	connections *limitedListener
}

// New builds the API. It fails if the security configuration cannot be
// compiled, which is checked here rather than per request so that a bad
// allowlist is a startup error instead of a puzzling rejection later.
func New(opts Options) (*API, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(discardHandler{})
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	list, err := newAllowlist(opts.Security.AllowedNetworks)
	if err != nil {
		return nil, fmt.Errorf("compile the allowed networks: %w", err)
	}

	a := &API{opts: opts, log: opts.Logger, started: opts.Now()}
	a.engine = a.buildEngine(list)
	return a, nil
}

// Handler serves every route.
func (a *API) Handler() http.Handler { return a.engine }

// Listen wraps a listener with the configured connection limit.
//
// This is separate from [API.Handler] because a connection limit cannot
// be enforced by a handler: one connection carries many requests under
// keep-alive, and a hijacked connection leaves the HTTP server's
// bookkeeping while still holding a socket.
func (a *API) Listen(inner net.Listener) net.Listener {
	limited := newLimitedListener(inner, a.opts.Security.MaxConnections, a.log)
	if counted, ok := limited.(*limitedListener); ok {
		a.connections = counted
	}
	return limited
}

// buildEngine assembles the router.
func (a *API) buildEngine(list *allowlist) *gin.Engine {
	// gin.New rather than gin.Default: the stock logger writes its own
	// format to stdout and the stock recovery writes a bare 500, neither
	// of which matches this package's log or its error envelope.
	engine := gin.New()

	// Client addresses must come from the connection, never from a
	// forwarding header. gin trusts every proxy by default, which would
	// let any caller present "X-Forwarded-For: 127.0.0.1" and appear to
	// be loopback — in the access log, and to anything else that reads
	// the client address.
	_ = engine.SetTrustedProxies(nil)

	engine.HandleMethodNotAllowed = true
	engine.NoRoute(a.handleNoRoute)
	engine.NoMethod(func(c *gin.Context) {
		fail(c, &Error{
			Code:    CodeNotFound,
			Message: fmt.Sprintf("%s is not allowed on %s", c.Request.Method, c.Request.URL.Path),
		})
	})

	// Order matters. The request id comes first so that everything after
	// it, including a rejection, can be traced. The access log wraps the
	// rest so that it sees the final status. Recovery sits inside the
	// access log so that a panic is still reported as a request.
	engine.Use(
		requestIDMiddleware(),
		accessLogMiddleware(a.log),
		recoveryMiddleware(a.log),
		allowlistMiddleware(list, a.log),
	)

	a.mountMCP(engine)

	// The concurrency limit applies to the management API only; see
	// concurrencyMiddleware for why streams are excluded.
	api := engine.Group(APIPrefix)
	api.Use(concurrencyMiddleware(a.opts.Security.MaxConcurrentRequests, a.log))
	a.registerRoutes(api)

	return engine
}

// mountMCP attaches the aggregated MCP endpoint.
//
// Every method reaches the same handler: the transport uses POST for
// requests, GET to open the notification stream and DELETE to end a
// session, and which of those are allowed depends on the session mode
// the handler resolves per request.
func (a *API) mountMCP(engine *gin.Engine) {
	if a.opts.MCP == nil {
		return
	}
	engine.Any(MCPPath, gin.WrapH(a.opts.MCP))
}

// registerRoutes adds the management API.
func (a *API) registerRoutes(api gin.IRoutes) {
	api.GET("/health", a.handleHealth)
}

// handleNoRoute reports an unmatched path.
//
// Once the web UI is served from this listener, a path outside the API
// will fall through to the single-page app instead. An API path must
// keep reporting JSON, since a client parsing HTML as an error envelope
// gets a confusing parse failure instead of the actual 404.
func (a *API) handleNoRoute(c *gin.Context) {
	fail(c, &Error{
		Code:    CodeNotFound,
		Message: fmt.Sprintf("no route for %s %s", c.Request.Method, c.Request.URL.Path),
	})
}

// Health is what the health endpoint reports.
type Health struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`

	// UptimeSeconds is sent alongside StartedAt so that a client with a
	// skewed clock still gets a usable figure.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	// Connections is how many are open, when a limit is in force.
	Connections int `json:"connections,omitempty"`
}

func (a *API) handleHealth(c *gin.Context) {
	health := Health{
		Status:        "ok",
		Version:       a.opts.Version,
		StartedAt:     a.started,
		UptimeSeconds: int64(a.opts.Now().Sub(a.started).Seconds()),
	}
	if a.connections != nil {
		health.Connections = a.connections.Open()
	}
	c.JSON(http.StatusOK, health)
}

// discardHandler drops every record, standing in for a nil logger.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h discardHandler) WithGroup(string) slog.Handler           { return h }
