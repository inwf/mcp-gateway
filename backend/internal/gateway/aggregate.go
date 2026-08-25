package gateway

import (
	"maps"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Aggregate is what the gateway exposes to its clients: the upstream
// tools under their assigned names, plus the mapping back.
type Aggregate struct {
	Names NameMap
	Tools []*mcp.Tool
}

// BuildAggregate renames every upstream tool and returns the combined
// list.
//
// Input schemas are carried across untouched. They are arbitrary JSON
// Schema written by the upstream author, and a client needs the original
// to build a valid call.
func BuildAggregate(tools map[string][]*mcp.Tool) Aggregate {
	names := BuildNames(tools)

	out := make([]*mcp.Tool, 0, names.Len())
	for _, server := range slices.Sorted(maps.Keys(tools)) {
		for _, tool := range tools[server] {
			if tool == nil || tool.Name == "" {
				continue
			}
			exposed, ok := names.Exposed(server, tool.Name)
			if !ok {
				continue
			}
			// A tool already emitted under this name is a duplicate
			// listing from the same server.
			if slices.ContainsFunc(out, func(t *mcp.Tool) bool { return t.Name == exposed }) {
				continue
			}
			out = append(out, renameTool(tool, exposed, server))
		}
	}

	slices.SortFunc(out, func(a, b *mcp.Tool) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	return Aggregate{Names: names, Tools: out}
}

// renameTool returns a copy under the exposed name.
//
// It has to be a copy: the original belongs to the upstream connection's
// cache, and mutating it would corrupt what that connection reports.
func renameTool(tool *mcp.Tool, exposed, server string) *mcp.Tool {
	copied := *tool
	copied.Name = exposed

	// Saying which server a tool came from is what lets a model choose
	// between three tools that all claim to search something.
	if copied.Description == "" {
		copied.Description = "(from " + server + ")"
	} else {
		copied.Description = "[" + server + "] " + copied.Description
	}

	return &copied
}

// FilterTools drops the tools a server's configuration does not expose.
// An empty allow list exposes everything, which is the default.
func FilterTools(tools []*mcp.Tool, allowed []string) []*mcp.Tool {
	if len(allowed) == 0 {
		return tools
	}

	permitted := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		permitted[name] = true
	}

	out := make([]*mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool != nil && permitted[tool.Name] {
			out = append(out, tool)
		}
	}
	return out
}
