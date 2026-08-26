// Package logging builds the loggers mcphub writes through.
//
// A single logger fans out to up to three destinations: standard output,
// a rotating file under the data directory, and an in-memory store that
// the management API queries. The in-memory store is why this package
// exists at all rather than calling [log/slog] directly.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"mcphub/internal/config"
)

// Format values accepted by [Options.Format].
const (
	FormatConsole = "console"
	FormatJSON    = "json"
)

// Options describes one logger. It deliberately mirrors the relevant
// configuration rather than taking [config.Config], so that tests can
// build a logger without a whole configuration and can inject a clock.
type Options struct {
	Level  slog.Level
	Format string

	// FilePath is the log file to write. Empty means do not write to a
	// file at all, which is what tests and one-shot commands want.
	FilePath  string
	MaxAge    time.Duration
	MaxSizeMB int

	// Stdout receives a copy of every record. Nil means do not write to
	// standard output.
	Stdout io.Writer

	// Store receives every record for later querying by the management
	// API. Nil means do not retain records in memory.
	Store *Store

	// Now overrides the clock, which is how rotation and retention are
	// tested without waiting.
	Now func() time.Time
}

// OptionsFrom derives logger options from the configuration.
func OptionsFrom(cfg config.Logging, paths config.Paths, store *Store) (Options, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return Options{}, err
	}
	return Options{
		Level:     level,
		Format:    cfg.Format,
		FilePath:  paths.LogFile(),
		MaxAge:    cfg.MaxAge,
		MaxSizeMB: cfg.MaxSizeMB,
		Stdout:    os.Stdout,
		Store:     store,
	}, nil
}

// ParseLevel converts a configured level name to a slog level.
func ParseLevel(level config.LogLevel) (slog.Level, error) {
	switch level {
	case config.LevelDebug:
		return slog.LevelDebug, nil
	case config.LevelInfo:
		return slog.LevelInfo, nil
	case config.LevelWarn:
		return slog.LevelWarn, nil
	case config.LevelError:
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", level)
	}
}

// LevelName converts a slog level to a configured level name. It is the
// inverse of [ParseLevel], for reporting a stored record to a client
// that should not have to know slog's numbering.
//
// Levels between the named ones are reported as the nearest named level
// at or below them, which is how slog itself describes them.
func LevelName(level slog.Level) config.LogLevel {
	switch {
	case level < slog.LevelInfo:
		return config.LevelDebug
	case level < slog.LevelWarn:
		return config.LevelInfo
	case level < slog.LevelError:
		return config.LevelWarn
	default:
		return config.LevelError
	}
}

// Logger is a configured logger together with the resources it owns.
type Logger struct {
	*slog.Logger

	file io.Closer
}

// New builds a logger from opts. Call [Logger.Close] to release the log
// file when shutting down.
func New(opts Options) (*Logger, error) {
	if opts.Format == "" {
		opts.Format = FormatConsole
	}

	var handlers []slog.Handler
	var file io.Closer

	if opts.Stdout != nil {
		handlers = append(handlers, newTextOrJSON(opts.Stdout, opts))
	}

	if opts.FilePath != "" {
		w, err := newRotatingFile(rotateOptions{
			Path:      opts.FilePath,
			MaxSizeMB: opts.MaxSizeMB,
			MaxAge:    opts.MaxAge,
			Now:       opts.Now,
		})
		if err != nil {
			return nil, err
		}
		file = w
		handlers = append(handlers, newTextOrJSON(w, opts))
	}

	if opts.Store != nil {
		handlers = append(handlers, newStoreHandler(opts.Store, opts.Level, opts.Now))
	}

	return &Logger{Logger: slog.New(fanout(handlers)), file: file}, nil
}

// Close releases the log file, if one was opened.
func (l *Logger) Close() error {
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}

func newTextOrJSON(w io.Writer, opts Options) slog.Handler {
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}
	if opts.Format == FormatJSON {
		return slog.NewJSONHandler(w, handlerOpts)
	}
	return slog.NewTextHandler(w, handlerOpts)
}

// fanout delivers each record to every handler. A handler that fails
// does not stop the others: losing one destination should not silence
// the rest.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, h := range f {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		// Each handler gets its own copy: a handler is allowed to retain
		// the record, and Record contains a shared attribute backing
		// array.
		if err := h.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
