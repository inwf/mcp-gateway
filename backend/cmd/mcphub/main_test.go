package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphub/internal/config"
)

// execute runs the command and returns its exit code and streams.
func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// isolated points the data directory at a temporary location and clears
// the environment override, so a test never touches the developer's own
// data directory.
func isolated(t *testing.T) string {
	t.Helper()
	t.Setenv(config.DataDirEnv, "")
	return t.TempDir()
}

// fieldValue pulls "value" out of a "label: value" line of the report.
func fieldValue(t *testing.T, output, label string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(name) != label {
			continue
		}
		return strings.TrimSpace(value)
	}
	t.Fatalf("no %q line in output:\n%s", label, output)
	return ""
}

func TestVersionFlag(t *testing.T) {
	code, stdout, _ := execute(t, "--version")

	if code != exitOK {
		t.Errorf("exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout, version) {
		t.Errorf("output %q does not contain the version %q", stdout, version)
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	code, _, stderr := execute(t, "--not-a-flag")

	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if stderr == "" {
		t.Error("nothing was written to standard error")
	}
}

func TestUnexpectedPositionalArgumentIsAUsageError(t *testing.T) {
	code, _, stderr := execute(t, "serve")

	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "serve") {
		t.Errorf("stderr %q does not name the unexpected argument", stderr)
	}
}

func TestHelpExitsSuccessfully(t *testing.T) {
	code, _, stderr := execute(t, "-h")

	if code != exitOK {
		t.Errorf("exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stderr, "data-dir") {
		t.Errorf("usage text does not mention --data-dir:\n%s", stderr)
	}
}

func TestRunsWithoutAConfigFile(t *testing.T) {
	dir := isolated(t)

	code, stdout, stderr := execute(t, "--data-dir", dir)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "not found, using defaults") {
		t.Errorf("output does not say the configuration was missing:\n%s", stdout)
	}
	if got := fieldValue(t, stdout, "listen"); got != "127.0.0.1:7788" {
		t.Errorf("listen = %q, want the default 127.0.0.1:7788", got)
	}
}

// A missing configuration must not be written on startup: the file
// appears when the user configures something, not before.
func TestNoConfigFileIsCreatedOnStartup(t *testing.T) {
	dir := isolated(t)

	if code, _, stderr := execute(t, "--data-dir", dir); code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err == nil {
		t.Error("a configuration file was created before the user configured anything")
	}
}

func TestReadsAnExistingConfig(t *testing.T) {
	dir := isolated(t)
	cfg := config.Default()
	cfg.Listen.Port = 9123
	cfg.Logging.Level = config.LevelWarn
	cfg.MCPServers = map[string]config.MCPServer{
		"on":  {Transport: config.TransportStdio, Command: "npx", Enabled: true, Timeout: cfg.Security.ConnectionTimeout},
		"off": {Transport: config.TransportStdio, Command: "npx", Enabled: false, Timeout: cfg.Security.ConnectionTimeout},
	}
	if err := config.Save(filepath.Join(dir, "config.yaml"), cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	code, stdout, stderr := execute(t, "--data-dir", dir)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if got := fieldValue(t, stdout, "listen"); got != "127.0.0.1:9123" {
		t.Errorf("listen = %q, want port 9123", got)
	}
	if got := fieldValue(t, stdout, "log level"); got != "warn" {
		t.Errorf("log level = %q, want warn", got)
	}
	if got := fieldValue(t, stdout, "servers"); got != "2 configured, 1 enabled" {
		t.Errorf("servers = %q, want 2 configured and 1 enabled", got)
	}
	if strings.Contains(stdout, "not found") {
		t.Errorf("output claims the configuration is missing:\n%s", stdout)
	}
}

func TestConfigFlagOverridesTheDerivedPath(t *testing.T) {
	dir := isolated(t)
	elsewhere := filepath.Join(t.TempDir(), "custom.yaml")

	cfg := config.Default()
	cfg.Listen.Port = 9124
	if err := config.Save(elsewhere, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	code, stdout, stderr := execute(t, "--data-dir", dir, "--config", elsewhere)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if got := fieldValue(t, stdout, "config"); got != elsewhere {
		t.Errorf("config = %q, want %q", got, elsewhere)
	}
	if got := fieldValue(t, stdout, "listen"); got != "127.0.0.1:9124" {
		t.Errorf("listen = %q, want port 9124; the override was not read", got)
	}
}

func TestDataDirEnvironmentVariableIsUsed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DataDirEnv, dir)

	code, stdout, stderr := execute(t)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if got := fieldValue(t, stdout, "data dir"); got != dir {
		t.Errorf("data dir = %q, want %q", got, dir)
	}
}

func TestDataDirFlagBeatsTheEnvironment(t *testing.T) {
	fromFlag := t.TempDir()
	t.Setenv(config.DataDirEnv, t.TempDir())

	code, stdout, stderr := execute(t, "--data-dir", fromFlag)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if got := fieldValue(t, stdout, "data dir"); got != fromFlag {
		t.Errorf("data dir = %q, want the flag value %q", got, fromFlag)
	}
}

// With nothing specified, everything lands beside the project rather
// than in the user's home directory.
func TestDefaultDataDirIsInTheWorkingDirectory(t *testing.T) {
	t.Setenv(config.DataDirEnv, "")
	wd := t.TempDir()
	t.Chdir(wd)

	code, stdout, stderr := execute(t)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if got, want := fieldValue(t, stdout, "data dir"), filepath.Join(wd, "data"); got != want {
		t.Errorf("data dir = %q, want %q", got, want)
	}
}

// The hard constraint: nothing mcphub writes may escape the data
// directory. This asserts it on the paths the command actually reports,
// not just on the ones a unit test knows to ask about.
func TestEveryReportedPathIsInsideTheDataDir(t *testing.T) {
	dir := isolated(t)

	code, stdout, stderr := execute(t, "--data-dir", dir)
	if code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}

	for _, label := range []string{"config", "log dir", "log file"} {
		path := fieldValue(t, stdout, label)
		// The report may append a note after the path.
		path = strings.Fields(path)[0]

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			t.Errorf("%s (%q) is not relative to the data dir: %v", label, path, err)
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("%s resolves to %q, outside the data dir %q", label, path, dir)
		}
	}
}

func TestLogDirectoryIsCreated(t *testing.T) {
	dir := isolated(t)

	if code, _, stderr := execute(t, "--data-dir", dir); code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}

	info, err := os.Stat(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatalf("log directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("the log path is not a directory")
	}
}

// The self-check writes a record, which proves the file destination is
// actually wired up rather than merely constructed.
func TestStartupIsRecordedInTheLogFile(t *testing.T) {
	dir := isolated(t)

	if code, _, stderr := execute(t, "--data-dir", dir); code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, "logs", "mcphub.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "startup self-check") {
		t.Errorf("the log file does not record the startup:\n%s", data)
	}
}

// Log records must not be mixed into the report, which is what a caller
// would parse or read.
func TestReportIsNotPollutedByLogOutput(t *testing.T) {
	dir := isolated(t)

	code, stdout, _ := execute(t, "--data-dir", dir)
	if code != exitOK {
		t.Fatalf("exit code = %d", code)
	}
	if strings.Contains(stdout, "level=") || strings.Contains(stdout, "startup self-check") {
		t.Errorf("log records leaked into the report:\n%s", stdout)
	}
}

func TestInvalidConfigIsRejectedWithTheFieldNamed(t *testing.T) {
	dir := isolated(t)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nlisten:\n  port: 70000\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	code, _, stderr := execute(t, "--data-dir", dir)

	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "listen.port") {
		t.Errorf("stderr %q does not name the offending field", stderr)
	}
}

func TestMalformedConfigIsRejectedWithTheLineNumber(t *testing.T) {
	dir := isolated(t)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen:\n  port: not-a-number\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	code, _, stderr := execute(t, "--data-dir", dir)

	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "2") {
		t.Errorf("stderr %q does not point at the offending line", stderr)
	}
}

// An explicit --config that does not exist is a mistake worth reporting,
// unlike the derived path simply not being there yet.
func TestMissingExplicitConfigStillFallsBackToDefaults(t *testing.T) {
	dir := isolated(t)
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	code, stdout, stderr := execute(t, "--data-dir", dir, "--config", missing)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "not found, using defaults") {
		t.Errorf("output does not report the missing file:\n%s", stdout)
	}
}
