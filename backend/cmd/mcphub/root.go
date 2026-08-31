package main

import (
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	"mcphub/internal/config"
)

// globalOptions are the settings every command shares: where mcphub keeps
// its files, and which configuration to read.
type globalOptions struct {
	DataDir    string
	ConfigPath string
}

// usageError marks a failure caused by how the command was typed rather
// than by what it went on to do.
//
// Cobra reports both as a plain error, but the two deserve different exit
// codes: a wrapper script should be able to tell "you typed it wrong"
// from "it ran and did not work".
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, args ...any) error {
	return &usageError{err: fmt.Errorf(format, args...)}
}

// newRootCommand builds the command tree.
//
// With no subcommand mcphub serves, because that is what it is for; the
// subcommands are the things you occasionally want instead of that.
func newRootCommand(stdout, stderr io.Writer, web fs.FS) *cobra.Command {
	var global globalOptions

	root := &cobra.Command{
		Use:   "mcphub",
		Short: "An MCP gateway: several upstream servers behind one endpoint",
		Long: "mcphub proxies a set of configured upstream MCP servers behind a single\n" +
			"MCP endpoint, and serves a web interface for managing them.\n\n" +
			"With no subcommand it starts the gateway.",
		Version: version,

		// This package prints the errors and picks the exit code, so cobra
		// is told to do neither. Left to itself it would print every
		// failure identically and always exit 1.
		SilenceUsage:  true,
		SilenceErrors: true,

		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			// Anything left here is a word that matched no subcommand.
			// Naming the alternatives saves a trip to the help text.
			return usagef("unknown command %q; try one of %s",
				args[0], availableCommands(cmd))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, &global, web, stdout, stderr)
		},
	}

	// Help and the version were asked for, so they are output rather than
	// diagnostics and belong on standard output. Everything the user did
	// not ask for goes to standard error.
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("mcphub {{.Version}}\n")

	// A flag the parser rejects is a usage problem like any other, and has
	// to be wrapped to be recognised as one.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	flags := root.PersistentFlags()
	flags.StringVar(&global.DataDir, "data-dir", "",
		"directory for configuration, logs and runtime state "+
			"(default \"./data\", or $"+config.DataDirEnv+")")
	flags.StringVar(&global.ConfigPath, "config", "",
		"configuration file to read (default <data-dir>/config.yaml)")

	root.AddCommand(
		newServeCommand(&global, web, stdout, stderr),
		newCheckCommand(&global, stdout),
		newConfigCommand(&global, stdout),
		newGuideCommand(stdout),
		newServersCommand(&global, stdout),
		newStatusCommand(&global, stdout),
		newTagsCommand(&global, stdout),
		newToolsCommand(&global, stdout),
		newUICommand(&global, stdout, openInBrowser()),
		newVersionCommand(stdout),
	)
	return root
}

func newServeCommand(global *globalOptions, web fs.FS, stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the gateway until interrupted",
		Long: "Run the gateway: connect to the configured upstream servers, serve the\n" +
			"aggregated MCP endpoint, the management API and the web interface.\n\n" +
			"This is what mcphub does with no subcommand, so `mcphub` and\n" +
			"`mcphub serve` are the same thing.",
		Args: noPositionalArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, global, web, stdout, stderr)
		},
	}
}

func newVersionCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version and exit",
		Args:  noPositionalArgs,
		RunE: func(*cobra.Command, []string) error {
			fmt.Fprintf(stdout, "mcphub %s\n", version)
			return nil
		},
	}
}

// runServe is shared by the serve subcommand and the root command, which
// do the same thing.
func runServe(cmd *cobra.Command, global *globalOptions, web fs.FS, stdout, stderr io.Writer) error {
	return serve(cmd.Context(), serveOptions{
		DataDir:    global.DataDir,
		ConfigPath: global.ConfigPath,
		WebUI:      web,
	}, stdout, stderr)
}

// noPositionalArgs rejects leftover words. None of these commands takes
// an argument, and silently ignoring one hides a typo.
func noPositionalArgs(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return usagef("unexpected argument %q", args[0])
}

func availableCommands(cmd *cobra.Command) string {
	var names []string
	for _, child := range cmd.Commands() {
		if child.IsAvailableCommand() {
			names = append(names, child.Name())
		}
	}
	return strings.Join(names, ", ")
}
