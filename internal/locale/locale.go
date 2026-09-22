// Package locale stores the desktop language independently of saved shows.
package locale

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var ErrMode = errors.New("select system, en or it")

type Snapshot struct {
	Mode      string `json:"mode"`
	Effective string `json:"effective"`
	System    string `json:"system"`
}

type Manager struct {
	mu      sync.RWMutex
	dir     string
	state   Snapshot
	changed func(string)
}

// New always returns a usable manager, falling back to the system language if
// the preference cannot be read. The error lets the host log that fallback.
// changed must be nonblocking and must not call back into this manager.
func New(dir, system string, changed func(string)) (*Manager, error) {
	if system != "it" {
		system = "en"
	}
	m := &Manager{dir: dir, state: Snapshot{Mode: "system", Effective: system, System: system}, changed: changed}
	err := m.load()
	if changed != nil {
		changed(m.state.Effective)
	}
	return m, err
}

func (m *Manager) load() error {
	path := filepath.Join(m.dir, "language.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return errors.New("invalid language preferences file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 1025))
	decoder.DisallowUnknownFields()
	var preference *struct {
		Mode string `json:"mode"`
	}
	if err := decoder.Decode(&preference); err != nil || preference == nil {
		return errors.New("invalid language preferences JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("invalid language preferences JSON")
	}
	if !valid(preference.Mode) {
		return ErrMode
	}
	m.state = m.resolve(preference.Mode)
	return nil
}

func valid(mode string) bool { return mode == "system" || mode == "en" || mode == "it" }

func (m *Manager) resolve(mode string) Snapshot {
	effective := mode
	if mode == "system" {
		effective = m.state.System
	}
	return Snapshot{Mode: mode, Effective: effective, System: m.state.System}
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Set persists before applying the preference. It never changes show state or
// restarts playback; the separate file is also safe for older app versions.
func (m *Manager) Set(mode string) error {
	if !valid(mode) {
		return ErrMode
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if mode == m.state.Mode {
		return nil
	}
	data, err := json.Marshal(struct {
		Mode string `json:"mode"`
	}{mode})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".language-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(m.dir, "language.json")); err != nil {
		return err
	}
	m.state = m.resolve(mode)
	if m.changed != nil {
		m.changed(m.state.Effective)
	}
	return nil
}
