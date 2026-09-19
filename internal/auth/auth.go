package auth

import (
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"smartstage/internal/identity"
)

const Lifetime = 24 * time.Hour
const MaxSessions = 128

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
	mu         sync.Mutex
	adminKey   string
	commandKey string
	sessions   map[string]Session
	attempts   map[string]attempt
	now        func() time.Time
}

func New() *Manager {
	return &Manager{adminKey: identity.New(), commandKey: identity.New(), sessions: map[string]Session{}, attempts: map[string]attempt{}, now: time.Now}
}
func (m *Manager) Keys() (string, string) { return m.adminKey, m.commandKey }
func (m *Manager) Pair(key, remote string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
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
	a := m.attempts[remote]
	if a.count >= 10 || len(m.attempts) >= 1024 && a.count == 0 {
		return Session{}, errors.New("too many pairing attempts; wait one minute")
	}
	if a.count == 0 {
		a.since = now
	}
	a.count++
	m.attempts[remote] = a
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
