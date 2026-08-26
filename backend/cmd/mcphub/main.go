// Command mcphub runs the MCP gateway: it proxies a set of configured
// upstream MCP servers behind a single MCP endpoint, and serves a web
// UI for managing them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"mcphub/internal/config"
	"mcphub/internal/logging"
)

// resolveConfigPath decides which configuration file to read, as an
// absolute path so that a later change of working directory cannot
// silently point at a different file.
func resolveConfigPath(paths config.Paths, configFlag string) (string, error) {
	if configFlag == "" {
		return paths.ConfigFile(), nil
	}
	absolute, err := filepath.Abs(configFlag)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", configFlag, err)
	}
	return absolute, nil
}

// version is the human-readable release version. Plain `go build` already
// stamps the git revision into the binary (readable via runtime/debug), so
// this only needs overriding for tagged release builds, via
// -ldflags "-X main.version=<value>".
var version = "dev"

// Exit codes. Distinguishing usage errors from runtime failures lets a
// wrapper script tell "you typed it wrong" from "it did not work".
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	// The context ends on the first interrupt, which is what starts an
	// orderly shutdown. A second interrupt is left to the default
	// handler, so an operator can always force the point.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run holds everything main does, so that it can be exercised by a test
// without spawning a process or capturing the real standard streams.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// A bare subcommand is peeled off before the flags, which is what
	// lets "mcphub serve --data-dir x" read naturally. The command tree
	// is small enough not to need a library for it yet.
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}

	flags := flag.NewFlagSet("mcphub "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)

	dataDir := flags.String("data-dir", "",
		"directory for configuration, logs and runtime state (default \"./data\", or $"+config.DataDirEnv+")")
	configPath := flags.String("config", "",
		"configuration file to read (default <data-dir>/config.yaml)")
	showVersion := flags.Bool("version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		// Parse has already written the message and the usage text.
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "mcphub: unexpected argument %q\n", flags.Arg(0))
		return exitUsage
	}

	if *showVersion || command == "version" {
		fmt.Fprintf(stdout, "mcphub %s\n", version)
		return exitOK
	}

	switch command {
	case "serve":
		err := serve(ctx, serveOptions{
			DataDir:    *dataDir,
			ConfigPath: *configPath,
			WebUI:      webUI(),
		}, stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "mcphub: %v\n", err)
			return exitFailure
		}
		return exitOK

	case "check":
		if err := selfCheck(*dataDir, *configPath, stdout); err != nil {
			fmt.Fprintf(stderr, "mcphub: %v\n", err)
			return exitFailure
		}
		return exitOK

	default:
		fmt.Fprintf(stderr, "mcphub: unknown command %q; try serve, check or version\n", command)
		return exitUsage
	}
}

// selfCheck performs the startup sequence up to the point of listening:
// resolve where everything lives, load and validate the configuration,
// open the log destinations, and report the result.
//
// Serving traffic is not yet implemented; this is what confirms the
// foundation is wired up correctly.
func selfCheck(dataDirFlag, configFlag string, stdout io.Writer) error {
	paths, err := config.ResolveDataDir(dataDirFlag)
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}

	cfgPath, err := resolveConfigPath(paths, configFlag)
	if err != nil {
		return err
	}

	cfg, err := config.Load(cfgPath)
	usingDefaults := errors.Is(err, fs.ErrNotExist)
	switch {
	case usingDefaults:
		cfg = config.Default()
	case err != nil:
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}

	store := logging.NewStore(logging.DefaultCapacity)
	opts, err := logging.OptionsFrom(cfg.Logging, paths, store)
	if err != nil {
		return err
	}
	opts.Stdout = nil // the report below is this command's only output
	log, err := logging.New(opts)
	if err != nil {
		return err
	}
	defer log.Close()

	log.For(logging.ModuleCLI).Info("startup self-check", "version", version)

	report(stdout, paths, cfgPath, usingDefaults, cfg)
	return nil
}

func report(w io.Writer, paths config.Paths, cfgPath string, usingDefaults bool, cfg config.Config) {
	configNote := ""
	if usingDefaults {
		configNote = "  (not found, using defaults)"
	}

	enabled := 0
	for _, server := range cfg.MCPServers {
		if server.Enabled {
			enabled++
		}
	}

	fmt.Fprintf(w, "mcphub %s\n\n", version)

	fmt.Fprintf(w, "data dir:   %s\n", paths.Root())
	fmt.Fprintf(w, "config:     %s%s\n", cfgPath, configNote)
	fmt.Fprintf(w, "log dir:    %s\n", paths.LogDir())
	fmt.Fprintf(w, "log file:   %s\n", paths.LogFile())

	fmt.Fprintf(w, "\nlisten:     %s:%d\n", cfg.Listen.Host, cfg.Listen.Port)
	fmt.Fprintf(w, "log level:  %s\n", cfg.Logging.Level)
	fmt.Fprintf(w, "session:    %s\n", cfg.Gateway.DefaultSessionMode)
	fmt.Fprintf(w, "servers:    %d configured, %d enabled\n", len(cfg.MCPServers), enabled)
}
