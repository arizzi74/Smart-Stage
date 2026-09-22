package gateway

import (
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// Matches the desktop's maximum command-session count and lifetime. This
	// is only an admission filter; the desktop still authorizes every command.
	maxAdmittedSessions = 128
	maxAdmissionAge     = 24 * time.Hour
	bodyReadTimeout     = 5 * time.Second
)

// sessionAdmission learns sessions from the existing pairing response, without
// changing the tunnel protocol or trusting cookies merely supplied by callers.
// It is scoped to one tunnel and is discarded when that tunnel disconnects.
type sessionAdmission struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]time.Time
}

func sessionKey(h http.Header) ([sha256.Size]byte, bool) {
	var value string
	for _, c := range (&http.Request{Header: h}).Cookies() {
		if c.Name != commandCookie {
			continue
		}
		// Reject ambiguous duplicate cookies and keep hashing work bounded.
		if value != "" || c.Value == "" || len(c.Value) > 256 {
			return [sha256.Size]byte{}, false
		}
		value = c.Value
	}
	return sha256.Sum256([]byte(value)), value != ""
}

func (a *sessionAdmission) expire(now time.Time) {
	for key, expires := range a.sessions {
		if !now.Before(expires) {
			delete(a.sessions, key)
		}
	}
}

func (a *sessionAdmission) known(h http.Header, now time.Time) bool {
	key, ok := sessionKey(h)
	if !ok {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expire(now)
	_, ok = a.sessions[key]
	return ok
}

// observe processes only filtered response headers. False means a successful
// pairing could not be retained without evicting an existing live session.
func (a *sessionAdmission) observe(path string, status int, request, response http.Header, now time.Time) bool {
	previous, hasPrevious := sessionKey(request)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expire(now)
	// A failed attempt to pair again does not revoke the caller's existing
	// desktop session. Only session-protected API responses can invalidate it.
	protected := strings.HasPrefix(path, "/api/") && path != "/api/pair"
	if hasPrevious && (protected && status == http.StatusUnauthorized || path == "/api/logout" && status >= 200 && status < 300) {
		delete(a.sessions, previous)
	}
	if path != "/api/pair" || status < 200 || status >= 300 {
		return true
	}
	var paired *http.Cookie
	for _, c := range (&http.Response{Header: response}).Cookies() {
		if c.Name == commandCookie {
			if paired != nil {
				return false
			}
			paired = c
		}
	}
	// A successful response without a session does not authenticate anything.
	if paired == nil {
		return true
	}
	if paired.Value == "" || len(paired.Value) > 256 || paired.MaxAge < 0 {
		return false
	}
	expires := now.Add(maxAdmissionAge)
	if paired.MaxAge > 0 && paired.MaxAge < int(maxAdmissionAge/time.Second) {
		expires = now.Add(time.Duration(paired.MaxAge) * time.Second)
	}
	if !paired.Expires.IsZero() && paired.Expires.Before(expires) {
		expires = paired.Expires
	}
	if !now.Before(expires) {
		return false
	}
	key := sha256.Sum256([]byte(paired.Value))
	_, alreadyKnown := a.sessions[key]
	_, replacesKnown := a.sessions[previous]
	if !alreadyKnown && len(a.sessions) >= maxAdmittedSessions && !(hasPrevious && replacesKnown) {
		return false
	}
	if a.sessions == nil {
		a.sessions = make(map[[sha256.Size]byte]time.Time)
	}
	if hasPrevious && previous != key {
		delete(a.sessions, previous)
	}
	a.sessions[key] = expires
	return true
}

// Do not let net/http drain an incomplete HTTP/1 body while rejecting its
// headers. In HTTP/2 the server closes the individual request stream instead.
func closeUnread(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil && r.Body != http.NoBody && r.ProtoMajor == 1 {
		r.Close = true
		w.Header().Set("Connection", "close")
	}
}

func rejectUnread(w http.ResponseWriter, r *http.Request, message string, status int) {
	closeUnread(w, r)
	http.Error(w, message, status)
}

func rejectUnpaired(w http.ResponseWriter, r *http.Request) {
	closeUnread(w, r)
	// Match the desktop API so the remote opens its pairing dialog instead of
	// reporting a JSON parsing error when this admission has expired.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, `{"error":{"code":"unpaired","message":"Pair this browser session first"}}`+"\n")
}
