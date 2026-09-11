package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"mcphub/internal/api"
)

func TestServerTagsAreRejectedThroughEveryConfigurationWritePath(t *testing.T) {
	h := start(t, func(o *api.Options) { o.Configs = configs(t, twoServers) })
	before := h.Configs.Get()
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/api/servers", `{"name":"legacy","server":{"command":"echo","tags":{}}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/servers/files", `{"server":{"command":"echo","tags":{}}}`, http.StatusBadRequest},
		{http.MethodPut, "/api/config", `{"config":{"mcpServers":{"files":{"command":"echo","tags":{}}}}}`, http.StatusBadRequest},
		{http.MethodPost, "/api/servers/import", `{"mcpServers":{"legacy":{"command":"echo","tags":{}}}}`, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := h.raw(t, tc.method, tc.path, tc.body)
			if response.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, tc.status)
			}
			envelope := envelopeOf(t, response)
			encoded, _ := json.Marshal(envelope)
			if !strings.Contains(string(encoded), "tags") {
				t.Errorf("response does not name the obsolete field: %s", encoded)
			}
			if !reflect.DeepEqual(before, h.Configs.Get()) {
				t.Error("rejected legacy configuration changed the saved servers")
			}
		})
	}
}

func TestEmptyDirectoriesStillRejectLegacyTagFilters(t *testing.T) {
	h := start(t, nil)
	for _, path := range []string{"/api/tools?tag=", "/api/resources?tag="} {
		decode(t, h.get(t, path), http.StatusBadRequest, nil)
	}
}
