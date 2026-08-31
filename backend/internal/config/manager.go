package config

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
)

// Manager owns the running configuration and the file backing it.
//
// It is safe for concurrent use. Readers get an independent copy, so a
// caller can hold and inspect a configuration without blocking an update
// or being affected by one.
type Manager struct {
	path string

	mu  sync.RWMutex
	cfg Config

	subMu  sync.Mutex
	subs   map[int]chan Config
	nextID int
}

// NewManager loads the configuration at path and validates it. A missing
// file is not an error: the defaults are used and nothing is written
// until the first update.
func NewManager(path string) (*Manager, error) {
	cfg, err := LoadOrDefault(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return &Manager{
		path: path,
		cfg:  cfg,
		subs: map[int]chan Config{},
	}, nil
}

// Path is the file this manager reads and writes.
func (m *Manager) Path() string { return m.path }

// Get returns the current configuration. The result is a deep copy and
// may be modified freely.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.Clone()
}

// Update applies mutate to a copy of the current configuration, then
// validates and persists it. The in-memory configuration is replaced
// only after the file has been written, so the two never disagree.
//
// It returns the field-level changes, which the caller can log.
func (m *Manager) Update(mutate func(*Config) error) ([]Change, error) {
	m.mu.Lock()

	before := m.cfg.Clone()
	after := m.cfg.Clone()

	if err := mutate(&after); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if err := after.Validate(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if err := Save(m.path, after); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	m.cfg = after
	m.mu.Unlock()

	changes := Diff(before, after)
	// Notify outside the lock: a subscriber must never be able to
	// deadlock an update by calling back into the manager.
	m.notify(after)
	return changes, nil
}

// Reload re-reads the file and adopts it if it differs from what is
// running, returning the field-level changes.
//
// Nothing is adopted unless it loads and validates. A file being edited
// is read as often as it is saved, and half of an edit is not a
// configuration — replacing a working one with it would take the gateway
// down over a syntax error someone was about to fix.
//
// The diff is what makes this safe to call on a timer. The manager's own
// writes reach the file too, so most reloads find the configuration they
// already have; comparing rather than assuming is what keeps those from
// being announced as changes and reconnecting every server.
//
// A file that is not there is not a change: the running configuration is
// not abandoned because someone moved the file aside.
func (m *Manager) Reload() ([]Change, error) {
	cfg, err := Load(m.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", m.path, err)
	}

	m.mu.Lock()
	changes := Diff(m.cfg, cfg)
	if len(changes) == 0 {
		m.mu.Unlock()
		return nil, nil
	}
	m.cfg = cfg
	m.mu.Unlock()

	// Outside the lock, for the reason Update gives.
	m.notify(cfg)
	return changes, nil
}

// Subscribe returns a channel that receives the configuration after each
// successful update, and a function that stops the subscription.
//
// The channel holds only the most recent configuration: a subscriber
// that falls behind sees the latest value rather than a backlog, and
// never blocks an update.
func (m *Manager) Subscribe() (<-chan Config, func()) {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	id := m.nextID
	m.nextID++
	ch := make(chan Config, 1)
	m.subs[id] = ch

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			m.subMu.Lock()
			defer m.subMu.Unlock()
			if sub, ok := m.subs[id]; ok {
				delete(m.subs, id)
				close(sub)
			}
		})
	}
	return ch, cancel
}

func (m *Manager) notify(cfg Config) {
	m.subMu.Lock()
	defer m.subMu.Unlock()

	for _, ch := range m.subs {
		// Drop a stale pending value so the newest one fits. Both
		// operations are non-blocking, so a slow subscriber cannot hold
		// up an update.
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- cfg.Clone():
		default:
		}
	}
}
