package upstream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
)

// clientName identifies mcphub to the servers it connects to.
const clientName = "mcphub"

// ErrNotConnected is returned by operations that need a live session.
var ErrNotConnected = errors.New("server is not connected")

// Conn is mcphub's connection to a single upstream MCP server: the
// session, the cached view of what the server offers, and the status
// reported to the API.
//
// It is safe for concurrent use.
type Conn struct {
	name    string
	version string
	deps    Deps

	mu        sync.RWMutex
	cfg       config.MCPServer
	session   *mcp.ClientSession
	cmd       *exec.Cmd
	stderr    *stderrWriter
	status    Status
	tools     []*mcp.Tool
	resources []*mcp.Resource

	// lifetime is cancelled by Close. Work started in response to an
	// upstream notification runs under it, so nothing outlives the
	// connection.
	lifetime context.Context
	endLife  context.CancelFunc
}

// New returns a connection that has not been established yet.
func New(name string, cfg config.MCPServer, version string, deps Deps) *Conn {
	return &Conn{
		name:    name,
		version: version,
		deps:    deps,
		cfg:     cfg,
		status: Status{
			Name:      name,
			State:     StateDisconnected,
			LastCheck: deps.now(),
		},
	}
}

// Name is the configured name of this server.
func (c *Conn) Name() string { return c.name }

// Status returns the current view of the server.
func (c *Conn) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Tools returns the cached tool list. The slice must not be modified.
func (c *Conn) Tools() []*mcp.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tools
}

// Resources returns the cached resource list. The slice must not be
// modified.
func (c *Conn) Resources() []*mcp.Resource {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resources
}

// Connect establishes the session and populates the caches.
//
// The passed context bounds the connection attempt only; the resulting
// session outlives it and is torn down by [Conn.Close].
func (c *Conn) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.session != nil {
		c.mu.Unlock()
		return nil // already connected; connecting twice is a no-op
	}
	cfg := c.cfg
	c.setStateLocked(StateConnecting, nil)
	c.mu.Unlock()
	c.deps.notify(c.name, ChangeStatus)

	session, cmd, stderr, err := c.dial(ctx, cfg)
	if err != nil {
		c.fail(err)
		return err
	}

	lifetime, endLife := context.WithCancel(context.WithoutCancel(ctx))

	c.mu.Lock()
	c.session = session
	c.cmd = cmd
	c.stderr = stderr
	c.lifetime = lifetime
	c.endLife = endLife

	init := session.InitializeResult()
	c.applyHandshakeLocked(init, cmd)
	caps := capabilities(init)
	c.mu.Unlock()

	// Only ask for what the server said it has. Requesting a method a
	// server never advertised earns a "method not found" that looks like
	// a real failure in the logs.
	if caps.tools {
		if err := c.RefreshTools(ctx); err != nil {
			c.deps.logger().Warn("could not list tools", "error", err)
		}
	}
	if caps.resources {
		if err := c.RefreshResources(ctx); err != nil {
			c.deps.logger().Warn("could not list resources", "error", err)
		}
	}
	if caps.logging {
		if err := session.SetLoggingLevel(ctx, &mcp.SetLoggingLevelParams{Level: "info"}); err != nil {
			c.deps.logger().Warn("could not set the server's logging level", "error", err)
		}
	}

	c.mu.Lock()
	c.setStateLocked(StateConnected, nil)
	c.mu.Unlock()
	c.deps.notify(c.name, ChangeStatus)

	c.deps.logger().Info("connected",
		"transport", cfg.Transport,
		"serverName", c.status.ServerName,
		"serverVersion", c.status.ServerVersion,
		"tools", len(c.Tools()),
		"resources", len(c.Resources()))
	return nil
}

// Close tears down the session. It is safe to call on a connection that
// was never established, and safe to call twice.
//
// A child process that exits non-zero once its input is closed is
// reported to the log but not returned as an error: the teardown did
// what it was asked to, and a user disconnecting a server should not be
// told it failed because the server was untidy on the way out.
func (c *Conn) Close() error {
	c.mu.Lock()
	session, endLife, stderr := c.session, c.endLife, c.stderr
	c.session, c.endLife, c.cmd, c.stderr = nil, nil, nil, nil
	c.tools, c.resources = nil, nil
	c.setStateLocked(StateDisconnected, nil)
	c.mu.Unlock()

	if endLife != nil {
		endLife()
	}
	if session != nil {
		if err := session.Close(); err != nil {
			c.deps.logger().Debug("the server did not exit cleanly", "error", err)
		}
	}
	// Anything the child said on its way out is worth keeping.
	if stderr != nil {
		stderr.Flush()
	}

	if session != nil {
		c.deps.notify(c.name, ChangeStatus)
	}
	return nil
}

// CallTool invokes a tool on the server, bounded by the server's
// configured timeout.
func (c *Conn) CallTool(ctx context.Context, tool string, args any) (*mcp.CallToolResult, error) {
	session, timeout, err := c.sessionAndTimeout()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("call tool %s on %s: %w", tool, c.name, err)
	}
	return result, nil
}

// ReadResource fetches a resource from the server.
func (c *Conn) ReadResource(ctx context.Context, uri string) (*mcp.ReadResourceResult, error) {
	session, timeout, err := c.sessionAndTimeout()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, fmt.Errorf("read resource %s from %s: %w", uri, c.name, err)
	}
	return result, nil
}

// RefreshTools replaces the cached tool list with the server's current
// one, following pagination.
func (c *Conn) RefreshTools(ctx context.Context) error {
	session, timeout, err := c.sessionAndTimeout()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list tools on %s: %w", c.name, err)
		}
		tools = append(tools, tool)
	}

	c.mu.Lock()
	c.tools = tools
	c.status.ToolCount = len(tools)
	c.status.LastCheck = c.deps.now()
	c.mu.Unlock()

	c.deps.notify(c.name, ChangeTools)
	return nil
}

// RefreshResources replaces the cached resource list with the server's
// current one, following pagination.
func (c *Conn) RefreshResources(ctx context.Context) error {
	session, timeout, err := c.sessionAndTimeout()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var resources []*mcp.Resource
	for resource, err := range session.Resources(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list resources on %s: %w", c.name, err)
		}
		resources = append(resources, resource)
	}

	c.mu.Lock()
	c.resources = resources
	c.status.ResourceCount = len(resources)
	c.status.LastCheck = c.deps.now()
	c.mu.Unlock()

	c.deps.notify(c.name, ChangeResources)
	return nil
}

func (c *Conn) sessionAndTimeout() (*mcp.ClientSession, time.Duration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.session == nil {
		return nil, 0, fmt.Errorf("%s: %w", c.name, ErrNotConnected)
	}
	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = config.DefaultMCPServer().Timeout
	}
	return c.session, timeout, nil
}

// dial builds the transport for the configured protocol and completes
// the handshake.
func (c *Conn) dial(ctx context.Context, cfg config.MCPServer) (*mcp.ClientSession, *exec.Cmd, *stderrWriter, error) {
	switch cfg.Transport {
	case config.TransportStdio:
		return c.dialStdio(ctx, cfg)
	default:
		return nil, nil, nil, fmt.Errorf("%s: transport %q is not supported yet", c.name, cfg.Transport)
	}
}

func (c *Conn) dialStdio(ctx context.Context, cfg config.MCPServer) (*mcp.ClientSession, *exec.Cmd, *stderrWriter, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)

	// The configured environment is an overlay, not a replacement: a
	// server launched through npx or uvx needs PATH and HOME to work at
	// all.
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	stderr := newStderrWriter(c.deps.logger())
	cmd.Stderr = stderr

	session, err := c.newClient().Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		// The child's own complaint explains the failure far better than
		// a transport error does.
		stderr.Flush()
		return nil, nil, nil, fmt.Errorf("start %s (%s): %w", c.name, cfg.Command, err)
	}
	return session, cmd, stderr, nil
}

func (c *Conn) newClient() *mcp.Client {
	return mcp.NewClient(
		&mcp.Implementation{Name: clientName, Version: c.version},
		&mcp.ClientOptions{
			Logger: c.deps.logger(),

			// The server tells us when its lists change, so there is no
			// need to poll for it.
			ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
				c.refreshInBackground(c.RefreshTools, "tools")
			},
			ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) {
				c.refreshInBackground(c.RefreshResources, "resources")
			},

			// Server-side log records belong in this server's log view.
			LoggingMessageHandler: func(_ context.Context, req *mcp.LoggingMessageRequest) {
				c.deps.logger().Info("server message",
					"level", string(req.Params.Level),
					"data", fmt.Sprintf("%v", req.Params.Data))
			},
		})
}

// refreshInBackground runs a refresh off the session's own goroutine. A
// notification handler that blocks would stall the session it arrived
// on, including the very request that triggered the change.
func (c *Conn) refreshInBackground(refresh func(context.Context) error, what string) {
	c.mu.RLock()
	lifetime := c.lifetime
	c.mu.RUnlock()

	if lifetime == nil {
		return
	}
	go func() {
		if err := refresh(lifetime); err != nil && !errors.Is(err, ErrNotConnected) {
			c.deps.logger().Warn("could not refresh after the server reported a change",
				"what", what, "error", err)
		}
	}()
}

func (c *Conn) applyHandshakeLocked(init *mcp.InitializeResult, cmd *exec.Cmd) {
	if init != nil {
		c.status.ProtocolVersion = init.ProtocolVersion
		if init.ServerInfo != nil {
			c.status.ServerName = init.ServerInfo.Name
			c.status.ServerVersion = init.ServerInfo.Version
		}
		caps := capabilities(init)
		c.status.HasTools = caps.tools
		c.status.HasResources = caps.resources
		c.status.HasPrompts = caps.prompts
		c.status.HasLogging = caps.logging
	}
	if cmd != nil && cmd.Process != nil {
		c.status.PID = cmd.Process.Pid
		c.status.StartedAt = c.deps.now()
	}
}

func (c *Conn) setStateLocked(state State, err error) {
	c.status.State = state
	c.status.LastCheck = c.deps.now()
	if err != nil {
		c.status.Error = err.Error()
	} else {
		c.status.Error = ""
	}
	if state != StateConnected {
		c.status.ToolCount, c.status.ResourceCount = 0, 0
	}
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	c.setStateLocked(StateFailed, err)
	c.tools, c.resources = nil, nil
	c.mu.Unlock()

	c.deps.logger().Error("connection failed", "error", err)
	c.deps.notify(c.name, ChangeStatus)
}

// declaredCapabilities is the subset of the handshake this package acts
// on.
type declaredCapabilities struct {
	tools     bool
	resources bool
	prompts   bool
	logging   bool
}

func capabilities(init *mcp.InitializeResult) declaredCapabilities {
	if init == nil || init.Capabilities == nil {
		return declaredCapabilities{}
	}
	return declaredCapabilities{
		tools:     init.Capabilities.Tools != nil,
		resources: init.Capabilities.Resources != nil,
		prompts:   init.Capabilities.Prompts != nil,
		logging:   init.Capabilities.Logging != nil,
	}
}
