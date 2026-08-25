// Package gateway exposes the tools and resources of every connected
// upstream server through a single MCP endpoint.
package gateway

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// separator joins a server name to a tool name. It has to come from the
// character set clients accept in a tool name, which leaves no character
// that cannot also appear in a server or tool name — so the joined form
// is inherently ambiguous.
//
// That ambiguity is harmless because the gateway never parses an exposed
// name back apart: [NameMap] holds the authoritative mapping. What the
// joining has to guarantee is only that names are unique and stable.
const separator = "_"

// maxToolNameLen bounds an exposed name. Clients display tool names in
// lists and some truncate them, so an unbounded name made of a long
// server name plus a long tool name is not usable.
const maxToolNameLen = 128

// Route is where an exposed tool name came from.
type Route struct {
	Server string
	Tool   string
}

// NameMap assigns an exposed name to every upstream tool and remembers
// where each one came from.
type NameMap struct {
	routes  map[string]Route
	exposed map[Route]string
	order   []string
}

// BuildNames assigns exposed names to the tools of every server.
//
// The assignment is deterministic: the same input always produces the
// same names, regardless of Go's map ordering. That matters because a
// client that has learned a tool name should still find it after the
// gateway restarts.
func BuildNames(tools map[string][]*mcp.Tool) NameMap {
	m := NameMap{
		routes:  map[string]Route{},
		exposed: map[Route]string{},
	}

	// Sorting both levels is what makes the result independent of map
	// iteration order and of the order a server happens to list its
	// tools in.
	for _, server := range slices.Sorted(maps.Keys(tools)) {
		names := make([]string, 0, len(tools[server]))
		for _, tool := range tools[server] {
			if tool != nil && tool.Name != "" {
				names = append(names, tool.Name)
			}
		}
		slices.Sort(names)

		for _, tool := range slices.Compact(names) {
			route := Route{Server: server, Tool: tool}
			if _, already := m.exposed[route]; already {
				continue
			}
			name := m.assign(server, tool)
			m.routes[name] = route
			m.exposed[route] = name
			m.order = append(m.order, name)
		}
	}

	return m
}

// assign produces an unused exposed name for one upstream tool.
func (m *NameMap) assign(server, tool string) string {
	base := truncate(sanitize(server) + separator + sanitize(tool))
	if _, taken := m.routes[base]; !taken {
		return base
	}

	// Two different upstream tools can join to the same string, either
	// because of where the separator falls or because truncation cut
	// them to the same prefix. Numbering the later ones keeps every name
	// distinct without disturbing the first.
	for n := 2; ; n++ {
		suffix := fmt.Sprintf("%s%d", separator, n)
		candidate := truncateTo(base, maxToolNameLen-len(suffix)) + suffix
		if _, taken := m.routes[candidate]; !taken {
			return candidate
		}
	}
}

// Route returns where an exposed name came from.
func (m NameMap) Route(exposed string) (Route, bool) {
	route, ok := m.routes[exposed]
	return route, ok
}

// Exposed returns the name assigned to an upstream tool.
func (m NameMap) Exposed(server, tool string) (string, bool) {
	name, ok := m.exposed[Route{Server: server, Tool: tool}]
	return name, ok
}

// Names lists every exposed name, in assignment order.
func (m NameMap) Names() []string { return slices.Clone(m.order) }

// Len is how many tools are exposed.
func (m NameMap) Len() int { return len(m.routes) }

// sanitize replaces characters clients do not accept in a tool name.
// Anything outside letters, digits, underscore and dash becomes an
// underscore, which can create a duplicate — resolved by [NameMap.assign]
// like any other.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func truncate(s string) string { return truncateTo(s, maxToolNameLen) }

func truncateTo(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}
