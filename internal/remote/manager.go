// Package remote owns the desktop's remote-access mode and outbound gateway
// connection. Credentials are deliberately separate from saved shows.
package remote

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"smartstage/internal/auth"
	"smartstage/internal/gateway"
)

type Settings struct {
	Mode  string `json:"mode"`
	URL   string `json:"url"`
	Token string `json:"token"`
}
type Edit struct {
	Mode  string `json:"mode"`
	URL   string `json:"url"`
	Token string `json:"token"`
}
type Status struct {
	Mode      string `json:"mode"`
	URL       string `json:"url"`
	HasToken  bool   `json:"hasToken"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	RemoteURL string `json:"remoteURL"`
	Restart   bool   `json:"restart,omitempty"`
}
type Options struct {
	ConfigDir  string
	Handler    func(publicURL, prefix string, authentication *auth.Manager) http.Handler
	DisableLAN func() error
	// ReserveRestart prevents playback until the requested LAN restart. Commit
	// is nonblocking; Abort releases the reservation if saving fails.
	ReserveRestart func() (commit, abort func(), err error)
	Changed        func(mode, remoteURL, phoneToken string)
	HTTPClient     *http.Client
}
type Manager struct {
	serial           sync.Mutex
	mu               sync.RWMutex
	options          Options
	settings         Settings
	status           Status
	ctx              context.Context
	cancel           context.CancelFunc
	connectionCancel context.CancelFunc
	generation       uint64
	closed           bool
	wg               sync.WaitGroup
}

// Load defaults to gateway even for installations created before this option
// existed. An unconfigured gateway never falls back to an inbound LAN port.
func Load(dir string) (Settings, error) {
	s := Settings{Mode: "gateway"}
	path := filepath.Join(dir, "gateway.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8192 {
		return s, errors.New("invalid gateway settings file")
	}
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8193))
	d.DisallowUnknownFields()
	var decoded *Settings
	if err := d.Decode(&decoded); err != nil || decoded == nil {
		return s, errors.New("invalid gateway settings JSON")
	}
	s = *decoded
	var extra any
	if d.Decode(&extra) != io.EOF {
		return s, errors.New("invalid gateway settings JSON")
	}
	if s.Token != "" && runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return s, errors.New("gateway credentials must be private; set gateway.json permissions to 0600")
	}
	return validate(s, true)
}
func validate(s Settings, allowEmpty bool) (Settings, error) {
	if s.Mode != "lan" && s.Mode != "gateway" {
		return s, errors.New("select public gateway or local LAN")
	}
	s.URL, s.Token = strings.TrimSpace(s.URL), strings.TrimSpace(s.Token)
	if s.URL == "" && s.Token == "" && (allowEmpty || s.Mode == "lan") {
		return s, nil
	}
	u, err := gateway.ValidateURL(s.URL)
	if err != nil {
		return s, err
	}
	s.URL = u.String()
	b, err := hex.DecodeString(s.Token)
	if err != nil || len(b) != 32 || len(s.Token) != 64 {
		return s, errors.New("enter the 64-character gateway registration token printed by the installer")
	}
	return s, nil
}
func New(parent context.Context, settings Settings, options Options) (*Manager, error) {
	if options.Handler == nil {
		return nil, errors.New("gateway command handler is required")
	}
	s, err := validate(settings, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{settings: s, options: options, ctx: ctx, cancel: cancel}
	if s.Mode == "gateway" && options.DisableLAN != nil {
		if err := options.DisableLAN(); err != nil {
			cancel()
			return nil, err
		}
	}
	m.mu.Lock()
	m.startLocked(false)
	m.mu.Unlock()
	return m, nil
}
func (m *Manager) Status() Status { m.mu.RLock(); defer m.mu.RUnlock(); return m.status }

func (m *Manager) Configure(edit Edit) (Status, error) {
	m.serial.Lock()
	defer m.serial.Unlock()
	m.mu.RLock()
	old, closed, restarting := m.settings, m.closed, m.status.Restart
	m.mu.RUnlock()
	if closed || restarting {
		return m.Status(), errors.New("Smart Stage is shutting down or restarting")
	}
	next := Settings{Mode: edit.Mode, URL: strings.TrimSpace(edit.URL), Token: strings.TrimSpace(edit.Token)}
	// Choosing LAN preserves a previously saved gateway unless a replacement
	// URL is explicitly supplied. Credentials are never returned to the UI.
	if next.Mode == "lan" && next.URL == "" && next.Token == "" {
		next.URL, next.Token = old.URL, old.Token
	}
	// A blank token retains the saved registration credential, including when
	// the user explicitly saves a different gateway URL. A supplied token replaces it.
	if next.Token == "" {
		next.Token = old.Token
	}
	next, err := validate(next, false)
	if err != nil {
		return m.Status(), err
	}
	needsRestart := next.Mode == "lan" && old.Mode != "lan"
	var commit, abort func()
	if needsRestart {
		if m.options.ReserveRestart == nil {
			return m.Status(), errors.New("restart is unavailable on this host")
		}
		commit, abort, err = m.options.ReserveRestart()
		if err != nil {
			return m.Status(), err
		}
	}
	if err = save(m.options.ConfigDir, next); err != nil {
		if abort != nil {
			abort()
		}
		return m.Status(), errors.New("could not save gateway settings")
	}
	// Close every inbound connection before making gateway mode visible.
	if next.Mode == "gateway" && m.options.DisableLAN != nil {
		if err = m.options.DisableLAN(); err != nil {
			_ = save(m.options.ConfigDir, old)
			if abort != nil {
				abort()
			}
			return m.Status(), err
		}
	}
	m.mu.Lock()
	m.settings = next
	m.startLocked(needsRestart)
	status := m.status
	m.mu.Unlock()
	if commit != nil {
		commit()
	}
	return status, nil
}
func (m *Manager) Reconnect() (Status, error) {
	m.serial.Lock()
	defer m.serial.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.status.Restart || m.settings.Mode != "gateway" || m.settings.URL == "" {
		return m.status, errors.New("configure a public gateway before reconnecting")
	}
	m.startLocked(false)
	return m.status, nil
}

// startLocked invokes only the nonblocking Changed callback under the lock.
// The callback must not call back into Manager.
func (m *Manager) startLocked(restart bool) {
	if m.connectionCancel != nil {
		m.connectionCancel()
	}
	m.generation++
	m.status = Status{Mode: m.settings.Mode, URL: m.settings.URL, HasToken: m.settings.Token != "", Status: "disabled", Restart: restart}
	if m.options.Changed != nil {
		m.options.Changed(m.settings.Mode, "", "")
	}
	if restart {
		m.status.Message = "Restarting Smart Stage to configure local LAN access."
		return
	}
	if m.settings.Mode == "lan" {
		m.status.Message = "Local LAN remote control is enabled."
		if warning, err := os.ReadFile(filepath.Join(m.options.ConfigDir, "lan-firewall-warning.txt")); err == nil && len(warning) <= 8192 && len(warning) > 0 {
			m.status.Message += " " + strings.TrimSpace(string(warning))
		}
		return
	}
	if m.settings.URL == "" {
		m.status.Status = "unconfigured"
		m.status.Message = "Configure your gateway URL and token. Local LAN access is disabled."
		return
	}
	m.status.Status = "connecting"
	m.status.Message = "Connecting securely to the public gateway…"
	ctx, cancel := context.WithCancel(m.ctx)
	m.connectionCancel = cancel
	m.wg.Add(1)
	go m.connect(ctx, m.generation, m.settings)
}
func (m *Manager) connect(ctx context.Context, generation uint64, s Settings) {
	defer m.wg.Done()
	backoff := time.Second
	for ctx.Err() == nil {
		authentication := auth.NewPublicCommand()
		err := gateway.ServeConnection(ctx, gateway.ClientOptions{URL: s.URL, Token: s.Token, HTTPClient: m.options.HTTPClient,
			Handler: func(prefix string) http.Handler { return m.options.Handler(s.URL, prefix, authentication) },
			OnConnect: func(endpoint string) {
				m.mu.Lock()
				defer m.mu.Unlock()
				if m.closed || generation != m.generation || ctx.Err() != nil {
					return
				}
				m.status.Status = "connected"
				m.status.Message = "Public gateway connected. Local LAN access is disabled."
				m.status.RemoteURL = endpoint + "#token=" + authentication.CommandToken()
				if m.options.Changed != nil {
					m.options.Changed("gateway", m.status.RemoteURL, authentication.CommandToken())
				}
				backoff = time.Second
			}})
		m.mu.Lock()
		if m.closed || generation != m.generation || ctx.Err() != nil {
			m.mu.Unlock()
			return
		}
		m.status.Status = "error"
		m.status.Message = "Gateway unavailable. Check the URL, token and internet connection; retrying automatically."
		if err != nil && err.Error() == "gateway rejected the registration token" {
			m.status.Message = "Gateway rejected the registration token. Check it in Admin."
		}
		m.status.RemoteURL = ""
		if m.options.Changed != nil {
			m.options.Changed("gateway", "", "")
		}
		m.mu.Unlock()
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
func (m *Manager) Close() {
	m.serial.Lock()
	m.mu.Lock()
	m.closed = true
	m.generation++
	m.cancel()
	if m.connectionCancel != nil {
		m.connectionCancel()
	}
	m.mu.Unlock()
	m.serial.Unlock()
	m.wg.Wait()
}
func save(dir string, s Settings) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gateway-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "gateway.json"))
}
