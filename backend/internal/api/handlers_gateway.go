package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
	"mcphub/internal/gateway"
	"mcphub/internal/upstream"
)

// defaultSearchLimit bounds a tool search that does not ask for a size.
const defaultSearchLimit = 50

// AggregatedTool is one upstream tool as the aggregated view reports it.
type AggregatedTool struct {
	// Server is where the tool came from, and Tool is its name there.
	Server string `json:"server"`
	Tool   string `json:"tool"`

	// Exposed is the name the gateway offers it under.
	Exposed string `json:"exposed"`

	Description string            `json:"description,omitempty"`
	InputSchema any               `json:"inputSchema,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`

	// Score is set when the list came from a search, and orders it.
	Score int `json:"score,omitempty"`
}

// ===== step 64: aggregated tools and resources =====

func (a *API) handleAggregatedTools(c *gin.Context) {
	if a.opts.Upstreams == nil {
		c.JSON(http.StatusOK, gin.H{"tools": []AggregatedTool{}})
		return
	}

	limit, err := queryInt(c, "limit", defaultSearchLimit)
	if err != nil {
		fail(c, err)
		return
	}

	cfg := a.opts.Configs.Get()
	byServer := a.opts.Upstreams.Tools()
	names := gateway.BuildNames(byServer)

	// Tag filters are applied to the servers first, because a tag
	// belongs to a server rather than to a tool.
	wanted := parseTagFilter(c.QueryArray("tag"))
	tools := make([]AggregatedTool, 0, names.Len())

	for server, list := range byServer {
		serverCfg := cfg.MCPServers[server]
		if !matchesTags(serverCfg.Tags, wanted) {
			continue
		}
		// A tool the configuration does not expose is not on offer
		// through the gateway, so listing it here would be misleading.
		for _, tool := range gateway.FilterTools(list, serverCfg.ExposedTools) {
			exposed, _ := names.Exposed(server, tool.Name)
			tools = append(tools, AggregatedTool{
				Server:      server,
				Tool:        tool.Name,
				Exposed:     exposed,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Tags:        serverCfg.Tags,
			})
		}
	}

	if query := strings.TrimSpace(c.Query("q")); query != "" {
		tools = searchWithin(query, tools, limit)
	} else {
		sort.Slice(tools, func(i, j int) bool { return tools[i].Exposed < tools[j].Exposed })
		if len(tools) > limit {
			tools = tools[:limit]
		}
	}

	c.JSON(http.StatusOK, gin.H{"tools": tools, "total": len(tools)})
}

// searchWithin ranks the already-filtered tools, so that a search and a
// tag filter compose rather than one overriding the other.
func searchWithin(query string, tools []AggregatedTool, limit int) []AggregatedTool {
	candidates := make([]gateway.Searchable, 0, len(tools))
	for _, tool := range tools {
		candidates = append(candidates, gateway.Searchable{
			Server:      tool.Server,
			Tool:        tool.Tool,
			Exposed:     tool.Exposed,
			Description: tool.Description,
		})
	}

	byExposed := make(map[string]AggregatedTool, len(tools))
	for _, tool := range tools {
		byExposed[tool.Exposed] = tool
	}

	hits := gateway.SearchTools(query, candidates, limit)
	out := make([]AggregatedTool, 0, len(hits))
	for _, hit := range hits {
		tool := byExposed[hit.Exposed]
		tool.Score = hit.Score
		out = append(out, tool)
	}
	return out
}

// parseTagFilter reads repeated tag parameters, each "key" or
// "key=value". A bare key matches a server carrying that key whatever
// its value.
func parseTagFilter(raw []string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	wanted := make(map[string]string, len(raw))
	for _, entry := range raw {
		key, value, hasValue := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if hasValue {
			wanted[key] = strings.TrimSpace(value)
		} else {
			wanted[key] = ""
		}
	}
	return wanted
}

// matchesTags reports whether a server's tags satisfy every filter.
// Filters combine with "and": each one narrows the result further, which
// is what a user adding a second filter expects.
func matchesTags(tags, wanted map[string]string) bool {
	for key, value := range wanted {
		have, present := tags[key]
		if !present {
			return false
		}
		if value != "" && have != value {
			return false
		}
	}
	return true
}

// AggregatedResource is one upstream resource with its origin.
type AggregatedResource struct {
	Server string `json:"server"`

	// URI is the resource's own URI on its server.
	URI string `json:"uri"`

	// Exposed is the URI the gateway offers it under.
	Exposed string `json:"exposed"`

	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

func (a *API) handleAggregatedResources(c *gin.Context) {
	if a.opts.Upstreams == nil {
		c.JSON(http.StatusOK, gin.H{"resources": []AggregatedResource{}})
		return
	}

	cfg := a.opts.Configs.Get()
	wanted := parseTagFilter(c.QueryArray("tag"))

	out := []AggregatedResource{}
	for server, resources := range a.opts.Upstreams.Resources() {
		if !matchesTags(cfg.MCPServers[server].Tags, wanted) {
			continue
		}
		for _, resource := range resources {
			out = append(out, AggregatedResource{
				Server:      server,
				URI:         resource.URI,
				Exposed:     gateway.ForwardedResourceURI(server, resource.URI),
				Name:        resource.Name,
				Description: resource.Description,
				MIMEType:    resource.MIMEType,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Server != out[j].Server {
			return out[i].Server < out[j].Server
		}
		return out[i].URI < out[j].URI
	})

	c.JSON(http.StatusOK, gin.H{"resources": out, "total": len(out)})
}

// ===== step 65: gateway state =====

func (a *API) handleGatewaySessions(c *gin.Context) {
	sessions := []gateway.SessionInfo{}
	if a.opts.Gateway != nil {
		sessions = append(sessions, a.opts.Gateway.Sessions()...)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })

	c.JSON(http.StatusOK, gin.H{"sessions": sessions, "total": len(sessions)})
}

// handleGatewayTools reports what the gateway currently offers its own
// clients, which is not the same as what the upstreams offer: the
// gateway adds its own tools and applies each server's exposure rules.
func (a *API) handleGatewayTools(c *gin.Context) {
	tools := []*mcp.Tool{}
	if a.opts.Gateway != nil {
		tools = append(tools, a.opts.Gateway.PublishedTools()...)
	}

	c.JSON(http.StatusOK, gin.H{
		"tools":       tools,
		"total":       len(tools),
		"systemTools": gateway.SystemToolNames,
	})
}

// handleGatewayStatus summarises the gateway in one request, which is
// what the dashboard needs rather than four.
func (a *API) handleGatewayStatus(c *gin.Context) {
	cfg := a.opts.Configs.Get()

	connected, failed := 0, 0
	if a.opts.Upstreams != nil {
		for _, status := range a.opts.Upstreams.Statuses() {
			switch status.State {
			case upstream.StateConnected:
				connected++
			case upstream.StateFailed:
				failed++
			}
		}
	}

	published, sessions := 0, 0
	if a.opts.Gateway != nil {
		published = len(a.opts.Gateway.PublishedTools())
		sessions = len(a.opts.Gateway.Sessions())
	}

	c.JSON(http.StatusOK, gin.H{
		"servers": gin.H{
			"configured": len(cfg.MCPServers),
			"connected":  connected,
			"failed":     failed,
		},
		"tools":       published,
		"sessions":    sessions,
		"sessionMode": defaultSessionMode(cfg),
	})
}

func defaultSessionMode(cfg config.Config) config.SessionMode {
	if cfg.Gateway.DefaultSessionMode == "" {
		return config.SessionModeStateful
	}
	return cfg.Gateway.DefaultSessionMode
}
