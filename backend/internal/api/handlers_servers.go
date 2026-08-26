package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
	"mcphub/internal/upstream"
)

// ServerView is one server as the API reports it: what it is configured
// to be, and what it is currently doing.
type ServerView struct {
	Name   string           `json:"name"`
	Config config.MCPServer `json:"config"`
	Status upstream.Status  `json:"status"`
}

// ===== step 60: server CRUD =====

func (a *API) handleListServers(c *gin.Context) {
	cfg := a.opts.Configs.Get()

	statuses := map[string]upstream.Status{}
	if a.opts.Upstreams != nil {
		for _, status := range a.opts.Upstreams.Statuses() {
			statuses[status.Name] = status
		}
	}

	views := make([]ServerView, 0, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
		views = append(views, a.viewOf(name, server, statuses))
	}
	// A stable order keeps the UI's list from reshuffling on every poll.
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })

	c.JSON(http.StatusOK, gin.H{"servers": views})
}

func (a *API) viewOf(name string, server config.MCPServer, statuses map[string]upstream.Status) ServerView {
	status, known := statuses[name]
	if !known {
		// A server that has been configured but not yet taken up by the
		// connection manager still has a truthful state to report.
		status = upstream.Status{Name: name, State: upstream.StateDisconnected}
	}
	return ServerView{Name: name, Config: server.Redact(), Status: status}
}

func (a *API) handleGetServer(c *gin.Context) {
	name, server, ok := a.requireServer(c)
	if !ok {
		return
	}

	statuses := map[string]upstream.Status{}
	if a.opts.Upstreams != nil {
		for _, status := range a.opts.Upstreams.Statuses() {
			statuses[status.Name] = status
		}
	}
	c.JSON(http.StatusOK, a.viewOf(name, server, statuses))
}

func (a *API) handleCreateServer(c *gin.Context) {
	var body struct {
		Name   string           `json:"name"`
		Server config.MCPServer `json:"server"`
	}
	if err := bindJSON(c, &body); err != nil {
		fail(c, err)
		return
	}
	if body.Name == "" {
		fail(c, Invalid("the server needs a name",
			FieldError{Field: "name", Message: "is required"}))
		return
	}

	var conflict bool
	_, err := a.opts.Configs.Update(func(current *config.Config) error {
		if _, taken := current.MCPServers[body.Name]; taken {
			conflict = true
			return fmt.Errorf("a server named %q already exists", body.Name)
		}
		if current.MCPServers == nil {
			current.MCPServers = map[string]config.MCPServer{}
		}
		current.MCPServers[body.Name] = body.Server
		return current.Validate()
	})
	if err != nil {
		if conflict {
			// Replacing an existing server through the create endpoint
			// would silently discard whatever was there.
			fail(c, Conflict(err.Error()))
			return
		}
		fail(c, FromValidation("the server is not valid", err))
		return
	}

	a.log.Info("a server was added", "requestId", RequestID(c), "server", body.Name)
	a.applyConfig(c)

	c.JSON(http.StatusCreated, ServerView{
		Name:   body.Name,
		Config: body.Server.Redact(),
		Status: upstream.Status{Name: body.Name, State: upstream.StateDisconnected},
	})
}

func (a *API) handleUpdateServer(c *gin.Context) {
	name, existing, ok := a.requireServer(c)
	if !ok {
		return
	}

	var body struct {
		Server config.MCPServer `json:"server"`
	}
	if err := bindJSON(c, &body); err != nil {
		fail(c, err)
		return
	}

	updated := restoreServerSecrets(body.Server, existing)
	if _, err := a.opts.Configs.Update(func(current *config.Config) error {
		current.MCPServers[name] = updated
		return current.Validate()
	}); err != nil {
		fail(c, FromValidation("the server is not valid", err))
		return
	}

	a.log.Info("a server was changed", "requestId", RequestID(c), "server", name)
	a.applyConfig(c)

	c.JSON(http.StatusOK, ServerView{Name: name, Config: updated.Redact()})
}

func (a *API) handleDeleteServer(c *gin.Context) {
	name, _, ok := a.requireServer(c)
	if !ok {
		return
	}

	if _, err := a.opts.Configs.Update(func(current *config.Config) error {
		delete(current.MCPServers, name)
		return nil
	}); err != nil {
		fail(c, Internal("the server could not be removed", err))
		return
	}

	// Applying the new configuration is what closes the connection and
	// stops its child process; leaving it running would orphan it.
	a.log.Info("a server was removed", "requestId", RequestID(c), "server", name)
	a.applyConfig(c)

	c.Status(http.StatusNoContent)
}

// ===== step 61: connection control =====

func (a *API) handleConnectServer(c *gin.Context) {
	name, _, ok := a.requireServer(c)
	if !ok {
		return
	}
	if a.opts.Upstreams == nil {
		fail(c, Unavailable("no connection manager is running"))
		return
	}

	// Connecting a server that is already connected is a no-op in the
	// connection manager, which is what makes this safe to retry.
	if err := a.opts.Upstreams.Connect(c.Request.Context(), name, a.opts.Configs.Get().Startup); err != nil {
		// The server is reachable as a concept but not as a process, which
		// is a state of the world rather than a bad request.
		fail(c, &Error{
			Code:    CodeUnavailable,
			Message: fmt.Sprintf("could not connect to %q: %s", name, err),
			cause:   err,
		})
		return
	}

	a.syncGateway()
	c.JSON(http.StatusOK, a.statusOfServer(name))
}

func (a *API) handleDisconnectServer(c *gin.Context) {
	name, _, ok := a.requireServer(c)
	if !ok {
		return
	}
	if a.opts.Upstreams == nil {
		fail(c, Unavailable("no connection manager is running"))
		return
	}

	// Disconnecting something already disconnected is not a failure: the
	// caller asked for a state, and that state already holds.
	if err := a.opts.Upstreams.Disconnect(name); err != nil && !errors.Is(err, upstream.ErrNotConnected) {
		fail(c, Internal(fmt.Sprintf("could not disconnect %q", name), err))
		return
	}

	a.syncGateway()
	c.JSON(http.StatusOK, a.statusOfServer(name))
}

func (a *API) statusOfServer(name string) upstream.Status {
	if a.opts.Upstreams != nil {
		for _, status := range a.opts.Upstreams.Statuses() {
			if status.Name == name {
				return status
			}
		}
	}
	return upstream.Status{Name: name, State: upstream.StateDisconnected}
}

// ===== step 62: one server's tools and resources =====

// requireConnection resolves a configured server that is also connected.
//
// A server that is merely not connected is reported as such rather than
// as an empty list: an empty list says "this server offers nothing",
// which would send a caller looking for the wrong problem.
func (a *API) requireConnection(c *gin.Context) (*upstream.Conn, bool) {
	name, _, ok := a.requireServer(c)
	if !ok {
		return nil, false
	}
	if a.opts.Upstreams == nil {
		fail(c, Unavailable("no connection manager is running"))
		return nil, false
	}

	conn, running := a.opts.Upstreams.Get(name)
	if !running || !conn.Status().Connected() {
		fail(c, Unavailable(fmt.Sprintf(
			"%q is not connected; connect it before listing or calling what it offers", name)))
		return nil, false
	}
	return conn, true
}

func (a *API) handleServerTools(c *gin.Context) {
	conn, ok := a.requireConnection(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"tools": nonNilTools(conn.Tools())})
}

func (a *API) handleServerResources(c *gin.Context) {
	conn, ok := a.requireConnection(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"resources": nonNilResources(conn.Resources())})
}

func (a *API) handleCallServerTool(c *gin.Context) {
	conn, ok := a.requireConnection(c)
	if !ok {
		return
	}

	var body struct {
		Arguments map[string]any `json:"arguments"`
	}
	if err := bindJSON(c, &body); err != nil {
		fail(c, err)
		return
	}

	tool := c.Param("tool")
	result, err := conn.CallTool(c.Request.Context(), tool, body.Arguments)
	if err != nil {
		fail(c, &Error{
			Code:    CodeUnavailable,
			Message: fmt.Sprintf("calling %q failed: %s", tool, err),
			cause:   err,
		})
		return
	}

	// A tool that ran and reported a problem is a successful call with a
	// failed result, and the caller needs to see what it said.
	c.JSON(http.StatusOK, gin.H{
		"isError":           result.IsError,
		"content":           result.Content,
		"structuredContent": result.StructuredContent,
	})
}

func (a *API) handleReadServerResource(c *gin.Context) {
	conn, ok := a.requireConnection(c)
	if !ok {
		return
	}

	uri := c.Query("uri")
	if uri == "" {
		fail(c, BadRequest("uri is required"))
		return
	}

	result, err := conn.ReadResource(c.Request.Context(), uri)
	if err != nil {
		fail(c, &Error{
			Code:    CodeUnavailable,
			Message: fmt.Sprintf("reading %q failed: %s", uri, err),
			cause:   err,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"contents": result.Contents})
}

// nonNilTools makes an empty list encode as [] rather than null, which
// a client would otherwise have to guard against on every use.
func nonNilTools(tools []*mcp.Tool) []*mcp.Tool {
	if tools == nil {
		return []*mcp.Tool{}
	}
	return tools
}

func nonNilResources(resources []*mcp.Resource) []*mcp.Resource {
	if resources == nil {
		return []*mcp.Resource{}
	}
	return resources
}
