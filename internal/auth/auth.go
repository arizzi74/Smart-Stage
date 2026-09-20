package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"smartstage/internal/identity"
)

const Lifetime = 24 * time.Hour
const MaxSessions = 128
const AttemptsPerIP = 10
const AttemptsGlobal = 100

var ErrTooManyAttempts = errors.New("too many pairing attempts; wait one minute")
var ErrInvalidToken = errors.New("invalid remote control code")

type Session struct {
	ID      string
	Role    string
	CSRF    string
	Expires time.Time
}
type attempt struct {
	since time.Time
	count int
}
type Manager struct {
	mu             sync.Mutex
	adminKey       string
	commandKey     string
	commandToken   string
	sessions       map[string]Session
	attempts       map[string]attempt
	globalAttempts attempt
	now            func() time.Time
}

func New() *Manager {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		panic("secure random source unavailable: " + err.Error())
	}
	return &Manager{adminKey: identity.New(), commandKey: identity.New(), commandToken: fmt.Sprintf("%08d", n.Int64()), sessions: map[string]Session{}, attempts: map[string]attempt{}, now: time.Now}
}

// NewPublicCommand creates a separate, high-entropy pairing secret for one
// public gateway connection. Reconnecting discards its browser sessions.
func NewPublicCommand() *Manager {
	m := New()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("secure random source unavailable: " + err.Error())
	}
	m.commandToken = hex.EncodeToString(b)
	return m
}

// CommandToken is generated once per process and only disclosed by local Admin.
// Session and CSRF secrets retain their full cryptographic entropy.
func (m *Manager) CommandToken() string { return m.commandToken }

// LocalAdmin is called only after the HTTP layer verifies the dedicated
// loopback listener, actual peer, Host and explicit same-origin header.
// Reusing a session keeps browser reloads from exhausting the session budget.
func (m *Manager) LocalAdmin(existing string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expire(now)
	if s, ok := m.sessions[existing]; ok && s.Role == "admin" {
		return s, nil
	}
	return m.newSession("admin", now)
}

// PairCommand never issues Admin sessions. Both per-peer and global budgets
// apply before checking a submitted code, including guesses from new IPs.
func (m *Manager) PairCommand(token, remote string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expire(now)
	if now.Sub(m.globalAttempts.since) >= time.Minute {
		m.globalAttempts = attempt{since: now}
	}
	if m.globalAttempts.count >= AttemptsGlobal || !m.allowPeer(remote, now) {
		return Session{}, ErrTooManyAttempts
	}
	m.globalAttempts.count++
	if subtle.ConstantTimeCompare([]byte(token), []byte(m.commandToken)) != 1 {
		return Session{}, ErrInvalidToken
	}
	return m.newSession("command", now)
}

// Keys and Pair support direct auth callers. HTTP listeners never accept these
// legacy role keys: Admin is local-only and Command uses its numeric code.
func (m *Manager) Keys() (string, string) { return m.adminKey, m.commandKey }
func (m *Manager) Pair(key, remote string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expire(now)
	if !m.allowPeer(remote, now) {
		return Session{}, ErrTooManyAttempts
	}
	role := ""
	if subtle.ConstantTimeCompare([]byte(key), []byte(m.adminKey)) == 1 {
		role = "admin"
	}
	if subtle.ConstantTimeCompare([]byte(key), []byte(m.commandKey)) == 1 {
		role = "command"
	}
	if role == "" {
		return Session{}, errors.New("invalid pairing key")
	}
	return m.newSession(role, now)
}

func (m *Manager) expire(now time.Time) {
	for id, s := range m.sessions {
		if !now.Before(s.Expires) {
			delete(m.sessions, id)
		}
	}
	for ip, a := range m.attempts {
		if now.Sub(a.since) >= time.Minute {
			delete(m.attempts, ip)
		}
	}
}

func (m *Manager) allowPeer(remote string, now time.Time) bool {
	a := m.attempts[remote]
	if a.count >= AttemptsPerIP || len(m.attempts) >= 1024 && a.count == 0 {
		return false
	}
	if a.count == 0 {
		a.since = now
	}
	a.count++
	m.attempts[remote] = a
	return true
}

func (m *Manager) newSession(role string, now time.Time) (Session, error) {
	if len(m.sessions) >= MaxSessions {
		return Session{}, errors.New("too many browser sessions; log out an existing session")
	}
	s := Session{ID: identity.New(), Role: role, CSRF: identity.New(), Expires: now.Add(Lifetime)}
	m.sessions[s.ID] = s
	return s, nil
}
func (m *Manager) Get(id string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if ok && !m.now().Before(s.Expires) {
		delete(m.sessions, id)
		return Session{}, false
	}
	return s, ok
}
func (m *Manager) Logout(id string) { m.mu.Lock(); defer m.mu.Unlock(); delete(m.sessions, id) }
func CheckCSRF(s Session, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(token)) == 1
}
