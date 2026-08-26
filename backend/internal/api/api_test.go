package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mcphub/internal/api"
	"mcphub/internal/config"
	"mcphub/internal/logging"
)

func TestMain(m *testing.M) {
	// gin's debug mode writes route tables and warnings to stdout, which
	// buries the test output.
	gin.SetMode(gin.TestMode)
	m.Run()
}

// harness is an API under test together with the log it wrote.
type harness struct {
	*httptest.Server
	Logs    *logging.Store
	API     *api.API
	Configs *config.Manager
}

// configs writes a configuration to a temporary file and opens it.
//
// A real manager rather than a stand-in: it is what makes the write
// endpoints exercise validation, atomic saving and the change log the
// way a running instance does.
func configs(t *testing.T, adjust func(*config.Config)) *config.Manager {
	t.Helper()

	cfg := config.Default()
	cfg.MCPServers = map[string]config.MCPServer{}
	if adjust != nil {
		adjust(&cfg)
	}

	path := t.TempDir() + "/config.yaml"
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save the configuration: %v", err)
	}
	manager, err := config.NewManager(path)
	if err != nil {
		t.Fatalf("open the configuration: %v", err)
	}
	return manager
}

// start builds an API and serves it, adjusting the options first.
func start(t *testing.T, adjust func(*api.Options)) *harness {
	t.Helper()

	store := logging.NewStore(500)
	log, err := logging.New(logging.Options{Level: slog.LevelDebug, Store: store})
	if err != nil {
		t.Fatalf("build the logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })

	opts := api.Options{
		Version:  "test",
		Logger:   log.For(logging.ModuleAPI),
		Security: config.Default().Security,
		Configs:  configs(t, nil),
		Logs:     store,
	}
	// The default permits loopback only, which is where tests connect
	// from; a test that cares sets its own.
	if adjust != nil {
		adjust(&opts)
	}

	built, err := api.New(opts)
	if err != nil {
		t.Fatalf("build the api: %v", err)
	}

	server := httptest.NewUnstartedServer(built.Handler())
	server.Listener = built.Listen(server.Listener)
	server.Start()
	t.Cleanup(server.Close)

	return &harness{Server: server, Logs: store, API: built, Configs: opts.Configs}
}

// get performs a GET and returns the response.
func (h *harness) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := h.Client().Get(h.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// do performs a request with an optional JSON body.
func (h *harness) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode the body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, h.URL+path, reader)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// raw performs a request with a literal body, for malformed input that
// could not be produced by encoding a value.
func (h *harness) raw(t *testing.T, method, path, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, h.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// decode reads a JSON response into target, failing on an unexpected
// status.
func decode(t *testing.T, resp *http.Response, want int, target any) {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, body)
	}
	if target == nil {
		return
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

// envelopeOf decodes a failed response.
func envelopeOf(t *testing.T, resp *http.Response) api.Envelope {
	t.Helper()

	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content type = %q, want JSON", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	var envelope api.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return envelope
}

func logText(store *logging.Store) string {
	var text string
	for _, entry := range store.Query(logging.Query{}) {
		text += entry.Message + " "
		for key, value := range entry.Attrs {
			text += key + "=" + value + " "
		}
		text += "\n"
	}
	return text
}

// ===== health =====

func TestHealthReportsTheBuild(t *testing.T) {
	h := start(t, nil)

	resp := h.get(t, "/api/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var health api.Health
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if health.Status != "ok" {
		t.Errorf("status = %q, want ok", health.Status)
	}
	if health.Version != "test" {
		t.Errorf("version = %q, want the version it was built with", health.Version)
	}
	if health.StartedAt.IsZero() {
		t.Error("startedAt is zero")
	}
}

// Uptime is what an operator reads to tell a long-running process from
// one that is crash-looping, so it has to advance.
func TestHealthReportsUptime(t *testing.T) {
	now := time.Now()
	h := start(t, func(o *api.Options) {
		o.Now = func() time.Time { return now }
	})

	// Move the clock forward without waiting for it.
	now = now.Add(90 * time.Second)

	var health api.Health
	if err := json.NewDecoder(h.get(t, "/api/health").Body).Decode(&health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if health.UptimeSeconds != 90 {
		t.Errorf("uptimeSeconds = %d, want 90", health.UptimeSeconds)
	}
}

// ===== routing =====

// A client parsing HTML where it expected an error envelope gets a
// confusing parse failure rather than the 404 that actually happened.
func TestUnknownAPIPathReturnsTheEnvelope(t *testing.T) {
	h := start(t, nil)

	resp := h.get(t, "/api/nothing-here")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	envelope := envelopeOf(t, resp)
	if envelope.Error.Code != api.CodeNotFound {
		t.Errorf("code = %q, want %q", envelope.Error.Code, api.CodeNotFound)
	}
	if envelope.Error.Message == "" {
		t.Error("the failure carries no message")
	}
}

func TestWrongMethodIsReported(t *testing.T) {
	h := start(t, nil)

	req, _ := http.NewRequest(http.MethodDelete, h.URL+"/api/health", nil)
	resp, err := h.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("DELETE on a GET-only route succeeded")
	}
	if envelope := envelopeOf(t, resp); envelope.Error.Message == "" {
		t.Error("the failure carries no message")
	}
}
