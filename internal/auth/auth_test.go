package auth

import (
	"testing"
	"time"
)

func TestPairRolesRotationCSRFExpiryAndLogout(t *testing.T) {
	m := New()
	admin, command := m.Keys()
	if len(admin) < 32 || len(command) < 32 || admin == command {
		t.Fatal("weak or shared launch keys")
	}
	a, err := m.Pair(admin, "host-a")
	if err != nil || a.Role != "admin" {
		t.Fatalf("admin pairing: %+v %v", a, err)
	}
	c, err := m.Pair(command, "host-b")
	if err != nil || c.Role != "command" {
		t.Fatalf("command pairing: %+v %v", c, err)
	}
	if !CheckCSRF(a, a.CSRF) || CheckCSRF(a, c.CSRF) || CheckCSRF(a, "") {
		t.Fatal("CSRF binding failed")
	}
	m.Logout(a.ID)
	if _, ok := m.Get(a.ID); ok {
		t.Fatal("logout retained session")
	}
	m.now = func() time.Time { return c.Expires.Add(time.Second) }
	if _, ok := m.Get(c.ID); ok {
		t.Fatal("expired session retained")
	}
	other := New()
	if _, ok := other.Get(c.ID); ok {
		t.Fatal("session survived process restart")
	}
	if a, b := other.Keys(); a == admin || b == command {
		t.Fatal("launch keys did not rotate")
	}
}
func TestPairRateLimit(t *testing.T) {
	m := New()
	admin, _ := m.Keys()
	for i := 0; i < 10; i++ {
		_, _ = m.Pair("invalid", "same-host")
	}
	if _, err := m.Pair(admin, "same-host"); err == nil {
		t.Fatal("pairing was not rate limited")
	}
	if _, err := m.Pair(admin, "different-host"); err != nil {
		t.Fatal(err)
	}
}
