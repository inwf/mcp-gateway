package upstream

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

// discardHandler drops every record. It stands in for a nil logger so
// that the rest of the package never has to check.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (h discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h discardHandler) WithGroup(string) slog.Handler           { return h }

// stderrWriter turns a child process's standard error into log records,
// one per line.
//
// A child that fails to start usually explains why on stderr, and that
// explanation is the most useful thing to show the user. Without this it
// would be discarded.
type stderrWriter struct {
	log   *slog.Logger
	level slog.Level

	mu      sync.Mutex
	partial []byte
}

func newStderrWriter(log *slog.Logger) *stderrWriter {
	// Child stderr is conventionally used for diagnostics rather than
	// errors, so it is recorded at debug level. A failed startup surfaces
	// the same lines through the connection error instead.
	return &stderrWriter{log: log, level: slog.LevelDebug}
}

func (w *stderrWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.partial = append(w.partial, p...)

	for {
		i := bytes.IndexByte(w.partial, '\n')
		if i < 0 {
			break
		}
		w.emit(w.partial[:i])
		w.partial = w.partial[i+1:]
	}

	// A process can produce a very long line, or none at all before it
	// exits. Cap the buffer so a runaway child cannot exhaust memory.
	const maxPartial = 64 << 10
	if len(w.partial) > maxPartial {
		w.emit(w.partial)
		w.partial = w.partial[:0]
	}

	return len(p), nil
}

// Flush records whatever was written without a trailing newline, which
// is the common case for a process that dies mid-sentence.
func (w *stderrWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.partial) > 0 {
		w.emit(w.partial)
		w.partial = w.partial[:0]
	}
}

func (w *stderrWriter) emit(line []byte) {
	text := string(bytes.TrimRight(line, "\r"))
	if text == "" {
		return
	}
	w.log.Log(context.Background(), w.level, text, "source", "stderr")
}
