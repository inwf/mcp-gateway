package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
	"mcphub/internal/events"
)

// Options configures a [Gateway].
type Options struct {
	Version   string
	Upstreams Upstreams
	Configs   Configs
	Logger    *slog.Logger

	// Gateway carries the session, keep-alive and debounce settings.
	Gateway config.Gateway

	// WireDebug logs every MCP message in both directions. Verbose;
	// intended for diagnosing protocol-level problems.
	WireDebug bool

	// After overrides the timer used to coalesce notifications, for
	// tests that cannot wait for it.
	After func(time.Duration, func())
}

// Gateway is the single MCP endpoint clients connect to. It exposes the
// gateway's own tools plus every tool of every connected upstream
// server, and forwards calls to whichever server a tool came from.
type Gateway struct {
	opts Options
	log  *slog.Logger

	server    *mcp.Server
	stateful  http.Handler
	stateless http.Handler

	mu sync.RWMutex
	// names maps an exposed tool name to the upstream tool it forwards
	// to. Handlers consult it at call time rather than capturing it,
	// because it is replaced whenever upstreams change.
	names NameMap
	// registered records what is currently on the server, and a
	// fingerprint of each entry, so that a resync only touches what
	// actually changed.
	registered map[string]string
	// publishedResources records the resource URIs currently on the
	// server, for the same reason.
	publishedResources map[string]string

	resync *debouncer
}

// New builds a gateway. Call [Gateway.Sync] to publish the current set
// of upstream tools, and [Gateway.Watch] to keep it current.
func New(opts Options) *Gateway {
	log := opts.Logger
	if log == nil {
		log = slog.New(discardHandler{})
	}

	g := &Gateway{
		opts:               opts,
		log:                log,
		registered:         map[string]string{},
		publishedResources: map[string]string{},
	}

	g.server = mcp.NewServer(
		&mcp.Implementation{
			Name:    "mcphub",
			Version: opts.Version,
			Title:   "MCP Hub",
		},
		&mcp.ServerOptions{
			Logger: log,
			Instructions: "This gateway proxies several MCP servers. Call " +
				ToolListServers + " to see what is available, " + ToolSearchTools +
				" to find a tool, and " + ToolGetTool + " for a tool's full schema.",

			// The SDK runs the liveness probe, which is what notices a
			// client that vanished without closing its session.
			KeepAlive:                 opts.Gateway.KeepAlive,
			KeepAliveFailureThreshold: opts.Gateway.KeepAliveFailureThreshold,

			HasTools:     true,
			HasResources: true,
		})

	RegisterSystemTools(g.server, opts.Upstreams, opts.Configs)

	if opts.WireDebug {
		g.installWireLogging()
	}

	g.resync = newDebouncer(opts.Gateway.NotifyDebounce, g.Sync, opts.After)

	// Two handlers over one server: the session behaviour differs, but
	// the tools and resources on offer do not.
	getServer := func(*http.Request) *mcp.Server { return g.server }
	g.stateful = mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		Logger:         log,
		SessionTimeout: opts.Gateway.SessionTimeout,
	})
	g.stateless = mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		Logger:    log,
		Stateless: true,
	})

	return g
}

// Server is the underlying MCP server, for callers that need to inspect
// its sessions.
func (g *Gateway) Server() *mcp.Server { return g.server }

// Handler serves the MCP endpoint, choosing a session mode per request.
func (g *Gateway) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := ResolveSessionMode(r.Header.Get(SessionModeHeader), r.UserAgent(), g.opts.Gateway)
		if mode == config.SessionModeStateless {
			g.stateless.ServeHTTP(w, r)
			return
		}
		g.stateful.ServeHTTP(w, r)
	})
}

// Sessions lists the clients currently connected.
func (g *Gateway) Sessions() []SessionInfo { return sessionsOf(g.server) }

// Watch keeps the exposed tools in step with the upstream servers until
// the context ends.
func (g *Gateway) Watch(ctx context.Context, bus *events.Bus) {
	if bus == nil {
		return
	}
	updates, cancel := bus.Subscribe(
		events.ToolsChanged,
		events.ResourcesChanged,
		events.ServerConnected,
		events.ServerDisconnected,
		events.ServerFailed,
	)

	go func() {
		defer cancel()
		defer g.resync.stop()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-updates:
				if !ok {
					return
				}
				g.resync.trigger()
			}
		}
	}()
}

// Sync publishes the current set of upstream tools.
//
// Only what changed is touched. The SDK sends a list-changed
// notification for every individual tool added, so re-adding an
// unchanged tool would tell every connected client to refetch for
// nothing.
func (g *Gateway) Sync() {
	aggregate := BuildAggregate(g.collectTools())

	current := make(map[string]string, len(aggregate.Tools))
	for _, tool := range aggregate.Tools {
		current[tool.Name] = fingerprint(tool)
	}

	g.mu.Lock()
	previous := g.registered
	g.names = aggregate.Names
	g.registered = current
	g.mu.Unlock()

	var removed []string
	for name := range previous {
		if _, still := current[name]; !still {
			removed = append(removed, name)
		}
	}
	if len(removed) > 0 {
		slices.Sort(removed)
		// One call for all of them, so one notification.
		g.server.RemoveTools(removed...)
	}

	added := 0
	for _, tool := range aggregate.Tools {
		if was, existed := previous[tool.Name]; existed && was == current[tool.Name] {
			continue
		}
		g.server.AddTool(tool, g.forward)
		added++
	}

	if added > 0 || len(removed) > 0 {
		g.log.Info("published the upstream tools",
			"added", added, "removed", len(removed), "total", len(current))
	}

	g.syncResources()
}

// collectTools gathers the upstream tools each server's configuration
// allows to be exposed.
func (g *Gateway) collectTools() map[string][]*mcp.Tool {
	all := g.opts.Upstreams.Tools()
	cfg := g.opts.Configs.Get()

	out := make(map[string][]*mcp.Tool, len(all))
	for server, tools := range all {
		allowed := FilterTools(tools, cfg.MCPServers[server].ExposedTools)
		if len(allowed) > 0 {
			out[server] = allowed
		}
	}
	return out
}

// forward sends a call to the upstream server the tool came from.
func (g *Gateway) forward(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := ""
	if req != nil && req.Params != nil {
		name = req.Params.Name
	}

	g.mu.RLock()
	route, ok := g.names.Route(name)
	g.mu.RUnlock()

	if !ok {
		// The tool existed when the client listed it and does not now,
		// which happens when a server disconnects mid-conversation.
		return toolError(fmt.Errorf("tool %q is no longer available; list the tools again", name)), nil
	}

	result, err := g.opts.Upstreams.CallTool(ctx, route.Server, route.Tool, req.Params.Arguments)
	if err != nil {
		// An upstream failure is something the model can read and react
		// to, so it belongs in the result rather than in a protocol
		// error it never sees.
		return toolError(err), nil
	}
	return result, nil
}

// syncResources publishes one resource per server plus one standing in
// for each upstream resource, touching only what changed.
func (g *Gateway) syncResources() {
	resources := BuildResources(g.opts.Upstreams.Statuses(), g.opts.Upstreams.Resources())

	current := make(map[string]string, len(resources))
	for _, resource := range resources {
		current[resource.URI] = resource.Name + "\x00" + resource.Description
	}

	g.mu.Lock()
	previous := g.publishedResources
	g.publishedResources = current
	g.mu.Unlock()

	var removed []string
	for uri := range previous {
		if _, still := current[uri]; !still {
			removed = append(removed, uri)
		}
	}
	if len(removed) > 0 {
		slices.Sort(removed)
		g.server.RemoveResources(removed...)
	}

	for _, resource := range resources {
		if was, existed := previous[resource.URI]; existed && was == current[resource.URI] {
			continue
		}
		g.server.AddResource(resource, g.readResource)
	}
}

func (g *Gateway) readResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := ""
	if req != nil && req.Params != nil {
		uri = req.Params.URI
	}

	server, upstreamURI, ok := ParseResourceURI(uri)
	if !ok {
		return nil, fmt.Errorf("resource %q does not belong to this gateway", uri)
	}

	// No upstream URI means the caller asked about the server itself.
	if upstreamURI == "" {
		return g.describeServer(server, uri)
	}
	return g.opts.Upstreams.ReadResource(ctx, server, upstreamURI)
}

func (g *Gateway) describeServer(server, uri string) (*mcp.ReadResourceResult, error) {
	for _, status := range g.opts.Upstreams.Statuses() {
		if status.Name != server {
			continue
		}
		encoded, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("describe server %s: %w", server, err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: uri, MIMEType: "application/json", Text: string(encoded)},
			},
		}, nil
	}
	return nil, fmt.Errorf("no server named %q", server)
}

// installWireLogging records every message in both directions.
func (g *Gateway) installWireLogging() {
	log := g.log.With("wire", true)

	g.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			started := time.Now()
			result, err := next(ctx, method, req)
			log.Debug("received", "method", method,
				"session", sessionID(req), "took", time.Since(started), "error", err)
			return result, err
		}
	})

	g.server.AddSendingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			log.Debug("sent", "method", method, "session", sessionID(req), "error", err)
			return result, err
		}
	})
}

func sessionID(req mcp.Request) string {
	if req == nil {
		return ""
	}
	if session := req.GetSession(); session != nil {
		return session.ID()
	}
	return ""
}

// fingerprint changes whenever anything a client would notice about a
// tool changes, and not otherwise.
func fingerprint(tool *mcp.Tool) string {
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		schema = []byte("?")
	}
	return tool.Description + "\x00" + string(schema)
}

// discardHandler drops every record, standing in for a nil logger.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h discardHandler) WithGroup(string) slog.Handler           { return h }
