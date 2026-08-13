package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// The two providers gooey ships. Neither is privileged: they implement
// the same three-line interface a host implements, which is the point —
// "the host owns persistence" is only true if the built-in provider has
// no capability an outside one lacks.

// File is a file-backed provider.
//
// Save is atomic within the directory: the document is written to a
// temp file alongside the target and renamed over it, so a crash or a
// full disk leaves the previous document intact rather than a truncated
// one. That matters more here than it looks — the store always writes
// the WHOLE document, so a torn write is not a lost setting, it is a
// lost settings file.
//
// A missing file loads as an empty document, which is how first run
// works. A directory that does not exist is created on first save, not
// on open: opening must not have side effects on a host that never
// changes a setting.
func File(path string) Provider { return fileProvider(path) }

type fileProvider string

func (f fileProvider) Load() ([]byte, error) {
	b, err := os.ReadFile(string(f))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", string(f), err)
	}
	return b, nil
}

func (f fileProvider) Save(doc []byte) error {
	path := string(f)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename has succeeded
	if _, err := tmp.Write(doc); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("rename onto %s: %w", path, err)
	}
	return nil
}

// UserFile locates a document under the user's config directory —
// $XDG_CONFIG_HOME/<app>/<name> on Linux, the platform equivalent
// elsewhere — and returns a File provider for it.
//
// It is a convenience, not a policy: an app whose settings are
// project-local rather than user-global (a dev-loop flag, say) should
// build its own path and call File. Choosing between those two scopes
// is exactly the choice the Provider seam exists to make cheap.
func UserFile(app, name string) (Provider, error) {
	if app == "" || name == "" {
		return nil, fmt.Errorf("settings: UserFile(%q, %q): both an app directory and a file name are required", app, name)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("settings: UserFile: no user config directory: %w", err)
	}
	return File(filepath.Join(dir, app, name)), nil
}

// Memory is an in-memory provider: the whole persistence contract with
// the persistence taken out.
//
// It is the provider an embedded app uses when the enclosing program
// keeps the document itself, and the one tests use. Its counters are
// mutex-guarded because Load and Save are called from the store's
// writer goroutine, never from the UI goroutine — which is the same
// reason a real provider must not touch the property graph.
type Memory struct {
	mu      sync.Mutex
	doc     []byte
	saves   int
	loadErr error
	saveErr error
	saved   chan struct{}
}

// NewMemory returns a provider holding doc as the stored document. An
// empty doc is a fresh install.
func NewMemory(doc string) *Memory {
	return &Memory{doc: []byte(doc), saved: make(chan struct{}, 64)}
}

func (m *Memory) Load() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return append([]byte(nil), m.doc...), nil
}

func (m *Memory) Save(doc []byte) error {
	m.mu.Lock()
	if m.saveErr != nil {
		err := m.saveErr
		m.mu.Unlock()
		return err
	}
	m.doc = append([]byte(nil), doc...)
	m.saves++
	m.mu.Unlock()
	select {
	case m.saved <- struct{}{}:
	default:
	}
	return nil
}

// Saves reports how many documents the store has actually written. It
// is the measurement behind every claim this package makes about how
// often a Set costs a write.
func (m *Memory) Saves() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

// Doc returns the stored document.
func (m *Memory) Doc() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.doc)
}

// Saved receives once per completed Save. It is the barrier that lets a
// test observe an asynchronous write without a sleep.
func (m *Memory) Saved() <-chan struct{} { return m.saved }

// FailLoad makes Load return err.
func (m *Memory) FailLoad(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadErr = err
}

// FailSave makes Save return err without storing anything.
func (m *Memory) FailSave(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveErr = err
}
