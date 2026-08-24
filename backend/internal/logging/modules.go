package logging

import "log/slog"

// Attribute keys that carry meaning beyond plain text. The management
// API filters on these, so they are constants rather than string
// literals scattered across the code.
const (
	// AttrModule names the subsystem that emitted a record.
	AttrModule = "module"

	// AttrServer names the upstream MCP server a record concerns. It is
	// what separates one server's log view from another's.
	AttrServer = "server"
)

// The subsystems that log. Keeping these as constants means a log view
// filtered by module cannot go stale against a renamed package.
const (
	ModuleConfig   = "config"
	ModuleUpstream = "upstream"
	ModuleGateway  = "gateway"
	ModuleAPI      = "api"
	ModuleWS       = "ws"
	ModuleCLI      = "cli"
	ModuleServer   = "server"
)

// Modules lists every known module, for validating a filter and for
// showing the options in the UI.
var Modules = []string{
	ModuleConfig,
	ModuleUpstream,
	ModuleGateway,
	ModuleAPI,
	ModuleWS,
	ModuleCLI,
	ModuleServer,
}

// For returns a logger that tags every record with a module.
func (l *Logger) For(module string) *slog.Logger {
	return l.Logger.With(AttrModule, module)
}

// ForServer returns a logger that tags every record with a module and an
// upstream server, so the record reaches that server's log view.
func (l *Logger) ForServer(module, server string) *slog.Logger {
	return l.Logger.With(AttrModule, module, AttrServer, server)
}
