package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mcphub/internal/config"
)

// maxRequestBody bounds a request body. Without a bound, one request
// could exhaust memory, and no legitimate configuration comes close.
const maxRequestBody = 4 << 20 // 4 MiB

// bindJSON decodes the request body, reporting a failure the client can
// act on rather than a decoder's wording.
func bindJSON(c *gin.Context, target any) error {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBody)

	decoder := json.NewDecoder(body)
	// An unknown field is almost always a typo or a client built against
	// a different version. Accepting it silently would leave the caller
	// believing a setting took effect when it was discarded.
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return BadRequest(fmt.Sprintf("the request body is larger than %d bytes", maxRequestBody))
		}
		return BadRequest("the request body is not valid JSON: " + err.Error())
	}

	// A second value means the caller sent more than one document, and
	// only the first would have been used.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return BadRequest("the request body carries more than one JSON document")
	}
	return nil
}

// queryInt reads an integer query parameter, falling back to a default.
func queryInt(c *gin.Context, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, BadRequest(fmt.Sprintf("%s must be a whole number, not %q", name, raw))
	}
	return value, nil
}

// queryTime reads an RFC 3339 timestamp query parameter.
func queryTime(c *gin.Context, name string) (time.Time, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, BadRequest(fmt.Sprintf(
			"%s must be an RFC 3339 timestamp such as %q, not %q",
			name, "2006-01-02T15:04:05Z", raw))
	}
	return value, nil
}

// applyConfig brings the running upstreams into line with the
// configuration and republishes the gateway's tools.
//
// Removing and reconfiguring servers happens before the response is
// sent, so a client that reloads immediately sees a consistent picture.
// Connecting does not: a new server can take seconds to start, and
// holding the request open for it would make an edit look like a hang.
func (a *API) applyConfig(c *gin.Context) {
	if a.opts.Upstreams == nil {
		return
	}

	cfg := a.opts.Configs.Get()
	added, removed, changed := a.opts.Upstreams.Apply(cfg)

	if len(added) > 0 || len(removed) > 0 || len(changed) > 0 {
		a.log.Info("the running servers were brought into line with the configuration",
			"requestId", RequestID(c),
			"added", added, "removed", removed, "changed", changed)
	}

	// Reconnecting is what a changed server needs; a new one has never
	// been connected at all.
	toConnect := append(append([]string{}, added...), changed...)
	if len(toConnect) > 0 {
		go a.connectInBackground(toConnect, cfg.Startup)
	}

	a.syncGateway()
}

// connectInBackground connects servers without holding a request open.
func (a *API) connectInBackground(names []string, startup config.Startup) {
	ctx, cancel := context.WithTimeout(context.Background(), backgroundConnectTimeout)
	defer cancel()

	for _, name := range names {
		if err := a.opts.Upstreams.Connect(ctx, name, startup); err != nil {
			a.log.Warn("could not connect a server after a configuration change",
				"server", name, "error", err)
		}
	}
	a.syncGateway()
}

// backgroundConnectTimeout bounds the connection attempts started by a
// configuration change, so a server that never answers cannot leave a
// goroutine running for the life of the process.
const backgroundConnectTimeout = 5 * time.Minute

func (a *API) syncGateway() {
	if a.opts.Gateway != nil {
		a.opts.Gateway.Sync()
	}
}

// requireServer resolves a server name from the path, reporting a 404
// for one that is not configured.
func (a *API) requireServer(c *gin.Context) (string, config.MCPServer, bool) {
	name := c.Param("name")

	server, known := a.opts.Configs.Get().MCPServers[name]
	if !known {
		fail(c, NotFound(fmt.Sprintf("no server named %q is configured", name)))
		return "", config.MCPServer{}, false
	}
	return name, server, true
}
