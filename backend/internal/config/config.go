// Package config defines the on-disk configuration of mcphub and the
// operations for loading, validating and persisting it.
//
// The configuration is a single YAML file. Every duration is written in
// Go's duration syntax ("30s", "5m", "2m30s") rather than a bare number,
// so units are never ambiguous.
package config

import "time"

// Transport identifies how mcphub reaches an upstream MCP server.
type Transport string

const (
	// TransportStdio runs the server as a child process and speaks
	// newline-delimited JSON-RPC over its stdin/stdout.
	TransportStdio Transport = "stdio"

	// TransportStreamableHTTP connects to an already-running server over
	// MCP's streamable HTTP transport. The server is someone else's to
	// run; mcphub only dials it.
	TransportStreamableHTTP Transport = "streamable-http"
)

// SessionMode selects how the gateway handles MCP client sessions.
type SessionMode string

const (
	// SessionModeStateful keeps a session per client, supports the SSE
	// stream and pushes list-changed notifications.
	SessionModeStateful SessionMode = "stateful"

	// SessionModeStateless serves each request in isolation. GET and
	// DELETE on the MCP endpoint are rejected, and no notifications are
	// delivered.
	SessionModeStateless SessionMode = "stateless"
)

// LogLevel is the minimum severity that reaches the log output. It also
// doubles as the filter accepted by the log query API.
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// Config is the root of the configuration file.
type Config struct {
	// Version is the configuration schema version. It exists so that a
	// future incompatible change can be detected rather than silently
	// misread.
	Version int `yaml:"version"`

	Listen   Listen   `yaml:"listen"`
	Logging  Logging  `yaml:"logging"`
	Security Security `yaml:"security"`
	Gateway  Gateway  `yaml:"gateway"`
	Startup  Startup  `yaml:"startup"`

	// MCPServers holds the upstream servers, keyed by the name they are
	// known by throughout the API, the web UI and the aggregated tool
	// names. The key "mcpServers" matches the convention used by other
	// MCP clients, so configurations are recognisable at a glance.
	MCPServers map[string]MCPServer `yaml:"mcpServers"`
}

// Listen configures the single HTTP listener that serves the MCP
// endpoint, the management API and the web UI.
type Listen struct {
	Host string `yaml:"host"`

	// Port is the TCP port to bind. Zero asks the operating system for
	// any free port, which is reported on startup.
	Port int `yaml:"port"`
}

// Logging configures log output and retention. Log files are written
// under the data directory, which is resolved independently of this
// file (see the data directory resolution in this package).
type Logging struct {
	Level LogLevel `yaml:"level"`

	// Format is "console" for human-readable output or "json" for
	// machine-readable output.
	Format string `yaml:"format"`

	// MaxAge is how long rotated log files are kept before deletion.
	MaxAge time.Duration `yaml:"maxAge"`

	// MaxSizeMB is the size at which a log file is rotated.
	MaxSizeMB int `yaml:"maxSizeMB"`

	// MCPWireDebug logs raw MCP protocol traffic in both directions.
	// Verbose; intended for diagnosing protocol-level problems.
	MCPWireDebug bool `yaml:"mcpWireDebug"`

	// APIDebug logs management API request and response bodies.
	APIDebug bool `yaml:"apiDebug"`
}

// Security limits who may reach the listener and how much concurrent
// load it accepts.
type Security struct {
	// AllowedNetworks is a list of CIDR blocks permitted to connect.
	// An empty list allows every client.
	AllowedNetworks []string `yaml:"allowedNetworks"`

	// AllowedOrigins names browser origins permitted to open the event
	// stream, in addition to pages served from this listener.
	//
	// A WebSocket handshake is not subject to the cross-origin checks
	// that guard an ordinary request, so a page on any site the user
	// visits could otherwise open a connection to their own machine and
	// read every event. An empty list permits same-origin pages only,
	// which is what a deployed instance serving its own web UI needs.
	// Running the frontend from a separate development server is the
	// case that needs an entry here.
	AllowedOrigins []string `yaml:"allowedOrigins,omitempty"`

	// MaxConnections caps simultaneously open TCP connections.
	MaxConnections int `yaml:"maxConnections"`

	// MaxConcurrentRequests caps requests being served at once.
	MaxConcurrentRequests int `yaml:"maxConcurrentRequests"`

	// ConnectionTimeout is the per-connection inactivity timeout.
	ConnectionTimeout time.Duration `yaml:"connectionTimeout"`

	// IdleConnectionTimeout applies to long-lived streams, which must
	// outlive ConnectionTimeout to avoid severing an idle SSE client.
	IdleConnectionTimeout time.Duration `yaml:"idleConnectionTimeout"`
}

// Gateway configures the aggregated MCP endpoint that clients connect to.
type Gateway struct {
	// DefaultSessionMode applies when no rule below matches.
	DefaultSessionMode SessionMode `yaml:"defaultSessionMode"`

	// SessionModeRules selects a mode from the client's User-Agent. An
	// explicit request header outranks these rules, and these rules
	// outrank DefaultSessionMode.
	SessionModeRules SessionModeRules `yaml:"sessionModeRules"`

	// SessionTimeout expires sessions that have been idle this long.
	SessionTimeout time.Duration `yaml:"sessionTimeout"`

	// NotifyDebounce coalesces bursts of upstream tool and resource
	// changes into a single notification to clients.
	NotifyDebounce time.Duration `yaml:"notifyDebounce"`

	// KeepAlive is the interval between liveness pings to connected
	// clients. Zero disables the probe.
	KeepAlive time.Duration `yaml:"keepAlive"`

	// KeepAliveFailureThreshold is how many consecutive failed pings
	// mark a session dead.
	KeepAliveFailureThreshold int `yaml:"keepAliveFailureThreshold"`
}

// SessionModeRules maps User-Agent substrings to a session mode. Matching
// is case-insensitive.
//
// An empty list and an absent list both mean "no rules for this mode",
// so these may be omitted from a saved file without changing meaning.
// That is not true of every empty value in this schema: an empty
// [Security.AllowedNetworks] means "allow every client", which is the
// opposite of what the default produces when the key is absent.
type SessionModeRules struct {
	Stateful  []string `yaml:"stateful,omitempty"`
	Stateless []string `yaml:"stateless,omitempty"`
}

// Startup configures how upstream servers are brought up at boot.
type Startup struct {
	// ConnectDelay staggers the initial connection to each upstream so
	// that many child processes are not spawned at once.
	ConnectDelay time.Duration `yaml:"connectDelay"`

	// MaxRetries is how many times a failed connection is retried.
	MaxRetries int `yaml:"maxRetries"`

	// RetryBackoff is the base delay for exponential backoff between
	// connection attempts.
	RetryBackoff time.Duration `yaml:"retryBackoff"`
}

// MCPServer is one upstream MCP server. Which fields apply depends on
// Transport; the validation rules in this package enforce that.
type MCPServer struct {
	Transport Transport `yaml:"transport"`

	// Enabled controls whether mcphub connects to this server at
	// startup. A disabled server stays in the configuration and remains
	// visible in the web UI.
	Enabled bool `yaml:"enabled"`

	// Description is shown in the UI and returned by the system tool
	// that lists servers, to help a model pick the right one.
	Description string `yaml:"description,omitempty"`

	// Tags are free-form key/value metadata used for grouping and
	// filtering in the UI and in tool search. They carry no routing
	// meaning.
	Tags map[string]string `yaml:"tags,omitempty"`

	// Timeout bounds a single request to this server.
	Timeout time.Duration `yaml:"timeout"`

	// Command, Args and Env apply to the stdio transport, which runs the
	// server as a child process.
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`

	// URL, Headers and Proxy apply to the streamable-http transport.
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Proxy   string            `yaml:"proxy,omitempty"`

	// ExposedTools restricts which of this server's tools the gateway
	// re-exposes. An empty list exposes all of them.
	ExposedTools []string `yaml:"exposedTools,omitempty"`
}
