package api

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"

	"github.com/gin-gonic/gin"
)

// allowlist decides whether a peer may reach the listener.
//
// The prefixes are parsed once, when the router is built, so that a
// request never pays for parsing and a malformed block is reported at
// startup instead of turning into a puzzling rejection later.
type allowlist struct {
	// prefixes is nil when every client is allowed, which is not the same
	// as an empty slice would be if it were reached by the same path —
	// see [newAllowlist].
	prefixes []netip.Prefix
}

// newAllowlist compiles the configured blocks.
//
// An empty configuration allows every client. That is the documented
// meaning of an empty list, and it differs from the default
// configuration, which permits loopback only: leaving the key out asks
// for the safe default, whereas writing an empty list is an explicit
// decision to accept anyone.
func newAllowlist(networks []string) (*allowlist, error) {
	if len(networks) == 0 {
		return &allowlist{}, nil
	}

	prefixes := make([]netip.Prefix, 0, len(networks))
	for _, network := range networks {
		prefix, err := parseNetwork(network)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}
	return &allowlist{prefixes: prefixes}, nil
}

// parseNetwork accepts a CIDR block or a bare address, the latter
// meaning that single host.
func parseNetwork(network string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(network); err == nil {
		// Masking rejects nothing but makes "10.0.0.7/8" behave as the
		// block the author plainly meant rather than as a host.
		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(network)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("allowed network %q is neither an IP address nor a CIDR block", network)
	}
	return netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()), nil
}

// permitsEveryone reports whether no restriction is in force.
func (a *allowlist) permitsEveryone() bool { return len(a.prefixes) == 0 }

// allows reports whether a peer address is permitted.
func (a *allowlist) allows(addr netip.Addr) bool {
	if a.permitsEveryone() {
		return true
	}

	// A client arriving over IPv6 on a dual-stack listener presents an
	// IPv4-mapped address, and "127.0.0.1/8" has to match it: the
	// operator wrote a rule about a host, not about a wire format.
	addr = addr.Unmap()

	for _, prefix := range a.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// allowlistMiddleware turns away peers outside the configured blocks.
func allowlistMiddleware(list *allowlist, log *slog.Logger) gin.HandlerFunc {
	if list.permitsEveryone() {
		// Nothing to check on any request; skip the frame entirely.
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		addr, err := peerAddr(c.Request.RemoteAddr)
		if err != nil {
			// An unreadable peer address cannot be checked against the
			// rules, and letting it through would defeat them.
			log.Warn("rejected a request from an unreadable address",
				"requestId", RequestID(c), "remoteAddr", c.Request.RemoteAddr, "error", err)
			fail(c, &Error{Code: CodeForbidden, Message: "this client is not permitted"})
			return
		}

		if !list.allows(addr) {
			log.Warn("rejected a request from a network that is not allowed",
				"requestId", RequestID(c), "clientIp", addr.String())
			fail(c, &Error{Code: CodeForbidden, Message: "this client is not permitted"})
			return
		}

		c.Next()
	}
}

// peerAddr extracts the address from a "host:port" remote address.
//
// This deliberately reads the connection's own peer rather than a
// forwarding header. A header is supplied by the client, so honouring
// one here would let anyone past the allowlist by claiming to be
// loopback.
func peerAddr(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// A remote address without a port is unusual but readable.
		host = remoteAddr
	}

	addr, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		return netip.Addr{}, fmt.Errorf("parse peer address %q: %w", remoteAddr, parseErr)
	}
	return addr, nil
}
