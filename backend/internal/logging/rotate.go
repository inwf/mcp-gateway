package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// rotateTimeFormat names a rotated file. It sorts chronologically as
// text, which is what makes finding the newest one a matter of sorting.
const rotateTimeFormat = "20060102-150405"

type rotateOptions struct {
	Path      string
	MaxSizeMB int
	MaxAge    time.Duration

	// Now overrides the clock. Retention is otherwise untestable without
	// waiting for real days to pass.
	Now func() time.Time
}

// rotatingFile is a write destination that starts a new file once the
// current one grows past a limit, and deletes files older than a
// retention window.
type rotatingFile struct {
	dir     string
	base    string // file name without the extension
	ext     string
	path    string
	maxSize int64
	maxAge  time.Duration
	now     func() time.Time

	mu   sync.Mutex
	file *os.File
	size int64
}

func newRotatingFile(opts rotateOptions) (*rotatingFile, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	dir := filepath.Dir(opts.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", dir, err)
	}

	name := filepath.Base(opts.Path)
	ext := filepath.Ext(name)

	w := &rotatingFile{
		dir:     dir,
		base:    strings.TrimSuffix(name, ext),
		ext:     ext,
		path:    opts.Path,
		maxSize: int64(opts.MaxSizeMB) * 1 << 20,
		maxAge:  opts.MaxAge,
		now:     now,
	}

	if err := w.open(); err != nil {
		return nil, err
	}
	// Old files accumulate while the process is not running, so the
	// retention window has to be applied at startup and not only when a
	// rotation happens.
	w.removeExpired()

	return w, nil
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Rotate before writing rather than after, so a single record is
	// never split across two files.
	if w.maxSize > 0 && w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingFile) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", w.path, err)
	}

	// Appending to an existing file means its current length counts
	// toward the size limit.
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("inspect log file %s: %w", w.path, err)
	}

	w.file = f
	w.size = info.Size()
	return nil
}

// rotate closes the current file, moves it aside under a timestamped
// name, and opens a fresh one.
func (w *rotatingFile) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close log file %s: %w", w.path, err)
	}
	w.file = nil

	archived := filepath.Join(w.dir, fmt.Sprintf("%s-%s%s",
		w.base, w.now().Format(rotateTimeFormat), w.ext))

	// A second rotation inside the same second would otherwise overwrite
	// the first one's output.
	archived = w.uniqueName(archived)

	if err := os.Rename(w.path, archived); err != nil {
		// Reopen so that logging keeps working even though this
		// rotation failed.
		_ = w.open()
		return fmt.Errorf("archive log file %s: %w", w.path, err)
	}

	if err := w.open(); err != nil {
		return err
	}
	w.removeExpired()
	return nil
}

func (w *rotatingFile) uniqueName(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s.%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// removeExpired deletes archived files older than the retention window.
// Failures are ignored: not reclaiming disk space is not a reason to
// stop logging.
func (w *rotatingFile) removeExpired() {
	if w.maxAge <= 0 {
		return
	}

	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := w.now().Add(-w.maxAge)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		stamp, ok := w.timestampOf(entry.Name())
		if !ok {
			continue
		}
		if stamp.Before(cutoff) {
			_ = os.Remove(filepath.Join(w.dir, entry.Name()))
		}
	}
}

// timestampOf reads the rotation time back out of an archived file name,
// which is what lets retention work without trusting file modification
// times that a backup or a copy would have rewritten.
func (w *rotatingFile) timestampOf(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, w.base+"-") || !strings.HasSuffix(name, w.ext) {
		return time.Time{}, false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(name, w.base+"-"), w.ext)

	// Drop the ".1" disambiguating suffix, if present.
	if i := strings.Index(middle, "."); i >= 0 {
		middle = middle[:i]
	}

	stamp, err := time.ParseInLocation(rotateTimeFormat, middle, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return stamp, true
}
