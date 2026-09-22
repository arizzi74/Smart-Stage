package auth

import (
	"errors"
	"fmt"
	"regexp"
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

func TestCommandTokenNumericRolesAndGlobalGuessBudget(t *testing.T) {
	m := New()
	if !regexp.MustCompile(`^[0-9]{8}$`).MatchString(m.CommandToken()) {
		t.Fatal("remote control token must have exactly eight digits")
	}
	admin, err := m.LocalAdmin("")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxSessions*2; i++ {
		again, err := m.LocalAdmin(admin.ID)
		if err != nil || again != admin {
			t.Fatalf("local browser reload changed or exhausted its session: %+v %v", again, err)
		}
	}
	adminKey, commandKey := m.Keys()
	for _, forbidden := range []string{adminKey, commandKey, admin.ID, admin.CSRF, "", "1234567", "123456789"} {
		if _, err := m.PairCommand(forbidden, "wrong"); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("accepted a non-code credential: %v", err)
		}
	}
	command, err := m.PairCommand(m.CommandToken(), "phone")
	if err != nil || command.Role != "command" || len(command.ID) < 32 || len(command.CSRF) < 32 {
		t.Fatalf("command session = %+v, %v", command, err)
	}
	local, err := m.LocalAdmin(command.ID)
	if err != nil || local.Role != "admin" || local.ID == command.ID {
		t.Fatal("local session reused the command role")
	}
	if got, ok := m.Get(command.ID); !ok || got.Role != "command" {
		t.Fatal("local session modified the controller's role")
	}

	// Distributed guesses cannot evade the global budget by changing source IP.
	m = New()
	now := time.Now()
	m.now = func() time.Time { return now }
	for i := 0; i < AttemptsGlobal; i++ {
		if _, err := m.PairCommand("not-a-code", fmt.Sprintf("192.0.2.%d", i)); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := m.PairCommand(m.CommandToken(), "new-peer"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("distributed guessing bypassed global budget: %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := m.PairCommand(m.CommandToken(), "new-peer"); err != nil {
		t.Fatalf("budget did not recover: %v", err)
	}
}

func TestCommandPerPeerBudgetAndExpiredLocalSession(t *testing.T) {
	m := New()
	now := time.Now()
	m.now = func() time.Time { return now }
	for i := 0; i < AttemptsPerIP; i++ {
		_, _ = m.PairCommand("wrong", "same-phone")
	}
	if _, err := m.PairCommand(m.CommandToken(), "same-phone"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatal("per-peer guessing budget bypassed")
	}
	if _, err := m.PairCommand(m.CommandToken(), "other-phone"); err != nil {
		t.Fatal(err)
	}
	a, err := m.LocalAdmin("")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(Lifetime)
	b, err := m.LocalAdmin(a.ID)
	if err != nil || a.ID == b.ID {
		t.Fatal("expired Admin session was retained")
	}
	if _, ok := m.Get(a.ID); ok {
		t.Fatal("expired Admin session still authorized")
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

func TestPublicPairingSurvivesInvalidGuessBudgets(t *testing.T) {
	m := NewPublicCommand()
	now := time.Now()
	m.now = func() time.Time { return now }
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(m.CommandToken()) {
		t.Fatal("public pairing requires a 256-bit secret")
	}
	for i := 0; i < AttemptsGlobal; i++ {
		peer := fmt.Sprintf("192.0.2.%d", i/AttemptsPerIP)
		if _, err := m.PairCommand("wrong", peer); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	for _, peer := range []string{"192.0.2.0", "new-phone"} {
		if _, err := m.PairCommand("wrong", peer); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("invalid guesses bypassed exhausted budget: %v", err)
		}
		paired, err := m.PairCommand(m.CommandToken(), peer)
		if err != nil || paired.Role != "command" || paired.ID == "" || paired.CSRF == "" {
			t.Fatalf("valid public credential denied: %+v %v", paired, err)
		}
		if got, ok := m.Get(paired.ID); !ok || got.Role != "command" {
			t.Fatal("public session missing or privileged")
		}
	}
	admin, legacy := m.Keys()
	for _, token := range []string{admin, legacy, "", "12345678", m.CommandToken()[:63]} {
		if _, err := m.PairCommand(token, "new-phone"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("different credential bypassed budget: %v", err)
		}
	}
}

func TestPublicPairingSessionLimitsAndExpiry(t *testing.T) {
	m := NewPublicCommand()
	now := time.Now()
	m.now = func() time.Time { return now }
	var first Session
	for i := 0; i < MaxSessions; i++ {
		paired, err := m.PairCommand(m.CommandToken(), "shared-gateway-peer")
		if err != nil {
			t.Fatalf("valid public pairing %d denied: %v", i, err)
		}
		if i == 0 {
			first = paired
		}
	}
	if _, err := m.PairCommand(m.CommandToken(), "shared-gateway-peer"); err == nil || errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("expected session capacity rejection, got %v", err)
	}
	m.Logout(first.ID)
	if _, err := m.PairCommand(m.CommandToken(), "shared-gateway-peer"); err != nil {
		t.Fatalf("logout did not release public session capacity: %v", err)
	}
	now = now.Add(Lifetime)
	paired, err := m.PairCommand(m.CommandToken(), "shared-gateway-peer")
	if err != nil || paired.ID == first.ID {
		t.Fatalf("expired public sessions did not release capacity: %v", err)
	}
	if _, ok := m.Get(first.ID); ok {
		t.Fatal("expired session retained")
	}
}
