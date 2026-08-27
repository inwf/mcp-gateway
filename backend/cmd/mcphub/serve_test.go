package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mcphub/internal/api"
	"mcphub/internal/config"
	"mcphub/internal/testmcp"
)

// running starts the serve command on a port the operating system picks
// and returns its base address.
//
// Port zero is what makes these tests safe to run in parallel with
// anything else: a fixed port would collide with a developer's own
// instance.
func running(t *testing.T, adjust func(*config.Config)) (baseURL string, stop func(), finished <-chan error) {
	t.Helper()

	dir := isolated(t)
	cfg := config.Default()
	cfg.Listen.Port = 0
	cfg.Startup.ConnectDelay = 0
	cfg.Startup.MaxRetries = 0
	cfg.MCPServers = map[string]config.MCPServer{}
	if adjust != nil {
		adjust(&cfg)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save the configuration: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)

	go func() {
		done <- serve(ctx, serveOptions{
			DataDir: dir,
			Ready:   func(addr string) { ready <- addr },
		}, io.Discard, io.Discard)
	}()

	select {
	case addr := <-ready:
		return "http://" + addr, cancel, done
	case err := <-done:
		cancel()
		t.Fatalf("serve returned before it was listening: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("serve never reported that it was listening")
	}
	return "", nil, nil
}

// ===== serving =====

func TestServeAnswersTheHealthEndpoint(t *testing.T) {
	base, stop, done := running(t, nil)
	defer func() { stop(); <-done }()

	resp, err := http.Get(base + "/api/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var health api.Health
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("status = %q, want ok", health.Status)
	}
}

// The MCP endpoint is the point of the whole program, so it has to be
// mounted by the command that serves it and not only in tests that
// assemble the stack by hand.
func TestServeMountsTheMCPEndpoint(t *testing.T) {
	base, stop, done := running(t, nil)
	defer func() { stop(); <-done }()

	// A GET without a session is refused by the transport, but the
	// refusal proves the endpoint is there rather than a 404 from the
	// router.
	resp, err := http.Get(base + api.MCPPath)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Error("the mcp endpoint is not mounted")
	}
}

// A configuration file that does not exist yet has to be created, or the
// settings page would have nothing to write back to.
func TestServeCreatesAMissingConfigurationFile(t *testing.T) {
	dir := isolated(t)

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	anyFreePort := 0
	go func() {
		done <- serve(ctx, serveOptions{
			DataDir: dir,
			// There is no configuration file to put a port in — that is
			// the point of this test — so the port comes from here
			// rather than from the default, which a developer running
			// this program is likely to be using.
			Port:  &anyFreePort,
			Ready: func(addr string) { ready <- addr },
		}, io.Discard, io.Discard)
	}()

	select {
	case <-ready:
	case err := <-done:
		cancel()
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("serve never started")
	}
	cancel()
	<-done

	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Errorf("no configuration file was written: %v", err)
	}
}

// ===== graceful shutdown =====

// An interrupt has to be answered promptly. A shutdown that hangs gets
// killed by whatever supervises the process, which is what leaves
// orphaned child processes behind.
func TestShutdownFinishesPromptly(t *testing.T) {
	_, stop, done := running(t, nil)

	stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned an error on shutdown: %v", err)
		}
	case <-time.After(shutdownGrace + 20*time.Second):
		t.Fatal("serve did not return after its context was cancelled")
	}
}

// This is the case that makes shutdown slow if the order is wrong: an
// open MCP stream never becomes idle, so waiting for it to finish would
// mean waiting out the whole grace period on every shutdown.
func TestShutdownIsPromptWithAStreamOpen(t *testing.T) {
	base, stop, done := running(t, nil)

	// Open the notification stream and leave it open.
	req, _ := http.NewRequest(http.MethodGet, base+api.MCPPath, nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
	}

	started := time.Now()
	stop()

	select {
	case <-done:
		t.Logf("shutdown with a stream open took %v", time.Since(started))
	case <-time.After(shutdownGrace + 20*time.Second):
		t.Fatal("shutdown waited for a stream that would never finish")
	}
}

// Child processes must not outlive the gateway: nothing else knows about
// them, so an orphan would sit there until the machine was restarted.
func TestShutdownStopsTheChildProcesses(t *testing.T) {
	server, err := testmcp.ServerConfig(testmcp.ModeFull)
	if err != nil {
		t.Fatalf("build the test server configuration: %v", err)
	}

	base, stop, done := running(t, func(cfg *config.Config) {
		cfg.MCPServers = map[string]config.MCPServer{"files": server}
	})

	// Wait for the child to be up, which happens in the background.
	var pid int
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && pid == 0 {
		var listed struct {
			Servers []struct {
				Status struct {
					PID   int    `json:"pid"`
					State string `json:"state"`
				} `json:"status"`
			} `json:"servers"`
		}
		resp, err := http.Get(base + "/api/servers")
		if err == nil {
			json.NewDecoder(resp.Body).Decode(&listed)
			resp.Body.Close()
			for _, s := range listed.Servers {
				if s.Status.State == "connected" && s.Status.PID != 0 {
					pid = s.Status.PID
				}
			}
		}
		if pid == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if pid == 0 {
		stop()
		<-done
		t.Fatal("the upstream server never connected")
	}

	stop()
	<-done

	if alive(pid) {
		t.Errorf("the child process %d is still running after shutdown", pid)
	}
}

// Shutting down must not panic, and a panic in a goroutine would take
// the test binary down with it rather than failing one test.
func TestShutdownIsSafeUnderLoad(t *testing.T) {
	base, stop, done := running(t, nil)

	// Keep requests in flight across the shutdown, so it happens while
	// the server is genuinely busy rather than idle.
	var wg sync.WaitGroup
	halt := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-halt:
					return
				default:
				}
				if resp, err := http.Get(base + "/api/health"); err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				} else {
					// Once the listener is gone, requests fail; that is
					// the expected end state, not a failure.
					return
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned an error: %v", err)
		}
	case <-time.After(shutdownGrace + 20*time.Second):
		t.Fatal("serve did not return")
	}

	close(halt)
	wg.Wait()
}

// The listener has to be released, or restarting immediately — which is
// what a supervisor does — fails to bind.
func TestThePortIsReleasedOnShutdown(t *testing.T) {
	base, stop, done := running(t, nil)
	stop()
	<-done

	// Nothing is listening any more.
	client := http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(base + "/api/health"); err == nil {
		resp.Body.Close()
		t.Error("the listener is still accepting after shutdown")
	}
}

// ===== configuration failures =====

// A configuration that cannot be served is a startup failure with an
// explanation, not a process that comes up in a broken state.
func TestServeRefusesAnInvalidConfiguration(t *testing.T) {
	dir := isolated(t)

	broken := "version: 1\nlisten:\n  port: 99999\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(broken), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}

	err := serve(context.Background(), serveOptions{DataDir: dir}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("serve accepted an invalid configuration")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

// alive reports whether a process still exists. Signal 0 performs the
// permission and existence checks without delivering anything, which is
// the usual way to ask.
func alive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
