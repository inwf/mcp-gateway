package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	Logs *logging.Store
	API  *api.API
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

	return &harness{Server: server, Logs: store, API: built}
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
