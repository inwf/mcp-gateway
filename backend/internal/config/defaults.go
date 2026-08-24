package config

import "time"

// CurrentVersion is the schema version this build reads and writes.
const CurrentVersion = 1

// Default returns a configuration that is safe to run as-is: it listens
// on loopback only, connects to no upstream servers, and has a timeout
// on every operation that can block.
//
// Every call returns independently owned maps and slices, so a caller
// may freely mutate the result.
func Default() Config {
	return Config{
		Version: CurrentVersion,

		Listen: Listen{
			// Loopback rather than a wildcard address: mcphub's
			// management API can change which commands get executed as
			// child processes, so reaching it must be an explicit
			// choice rather than a side effect of the default.
			Host: "127.0.0.1",
			Port: 7788,
		},

		Logging: Logging{
			Level:        LevelInfo,
			Format:       "console",
			MaxAge:       7 * 24 * time.Hour,
			MaxSizeMB:    50,
			MCPWireDebug: false,
			APIDebug:     false,
		},

		Security: Security{
			// Same reasoning as the listen address. Widening this to a
			// LAN range is a deliberate act, not a default.
			AllowedNetworks:       []string{"127.0.0.1/32", "::1/128"},
			MaxConnections:        50,
			MaxConcurrentRequests: 50,
			ConnectionTimeout:     30 * time.Second,
			// Must exceed ConnectionTimeout: a client holding an idle
			// event stream is expected, not evidence of a stuck peer.
			IdleConnectionTimeout: 5 * time.Minute,
		},

		Gateway: Gateway{
			DefaultSessionMode: SessionModeStateful,
			SessionModeRules: SessionModeRules{
				Stateful:  []string{},
				Stateless: []string{},
			},
			SessionTimeout: 30 * time.Minute,
			// Long enough to absorb a burst of upstream changes into one
			// notification, short enough that clients still feel it as
			// immediate.
			NotifyDebounce:            3 * time.Second,
			KeepAlive:                 30 * time.Second,
			KeepAliveFailureThreshold: 3,
		},

		Startup: Startup{
			ConnectDelay: 3 * time.Second,
			ReadyTimeout: 2 * time.Minute,
			MaxRetries:   3,
			RetryBackoff: 5 * time.Second,
		},

		MCPServers: map[string]MCPServer{},
	}
}

// DefaultMCPServer returns the values an upstream server entry takes for
// the fields it does not specify.
//
// Enabled defaults to true, which is why loading seeds this value before
// decoding an entry: after decoding, an omitted "enabled" and an
// explicit "enabled: false" would be indistinguishable.
func DefaultMCPServer() MCPServer {
	return MCPServer{
		Transport: TransportStdio,
		Enabled:   true,
		Timeout:   60 * time.Second,
	}
}
