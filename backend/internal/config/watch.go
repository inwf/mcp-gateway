package config

import (
	"context"
	"os"
	"time"
)

// DefaultWatchInterval is how often the file is checked when a caller
// does not say.
//
// Two seconds is chosen against what the delay costs: someone who edits
// config.yaml by hand waits at most that long to see it take, which is
// shorter than the edit itself. Making it much shorter would buy nothing
// a person could perceive.
const DefaultWatchInterval = 2 * time.Second

// WatchOptions configures [Manager.Watch].
type WatchOptions struct {
	// Interval is how often to look. Zero means DefaultWatchInterval.
	Interval time.Duration

	// OnChange is called with the changes after the file has been adopted.
	OnChange func([]Change)

	// OnError is called when the file changed but could not be loaded.
	//
	// Reported rather than swallowed: the running configuration is still
	// the last good one, so nothing is broken, but the edit someone just
	// made is not in effect and they need to be told which one it was.
	OnError func(error)
}

// Watch adopts the file's contents whenever they change on disk, until
// ctx ends. It returns immediately; the watching runs in its own
// goroutine.
//
// It polls rather than subscribing to filesystem events, which is a
// deliberate trade. A polled stat follows the path, so it is unaffected
// by the way this program saves — a write to a temporary file followed by
// a rename, which replaces the file rather than modifying it. An event
// watch on the file itself would follow the inode and stop hearing about
// a file it no longer names, and the usual repair for that is to watch
// the directory instead and filter, which is more machinery than one
// file's mtime is worth. Polling also keeps working on the filesystems
// where change notifications do not arrive at all, such as a network
// mount, and costs one stat every couple of seconds.
func (m *Manager) Watch(ctx context.Context, opts WatchOptions) {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultWatchInterval
	}

	ticker := time.NewTicker(interval)

	// The state at startup, so the first tick does not report the file the
	// process just read as a change.
	last := fileStamp(m.path)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := fileStamp(m.path)
				if now == last {
					continue
				}
				last = now

				changes, err := m.Reload()
				switch {
				case err != nil && opts.OnError != nil:
					opts.OnError(err)
				case len(changes) > 0 && opts.OnChange != nil:
					opts.OnChange(changes)
				}
			}
		}
	}()
}

// stamp is what a file looks like from the outside. Size joins the
// modification time because a filesystem may record that time at a
// coarser resolution than an edit takes: two saves within the same tick
// of a one-second clock would otherwise be one.
type stamp struct {
	modTime time.Time
	size    int64
	exists  bool
}

func fileStamp(path string) stamp {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{modTime: info.ModTime(), size: info.Size(), exists: true}
}
