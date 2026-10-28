package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"smartstage/internal/auth"
	"smartstage/internal/cloudflared"
	"smartstage/internal/gateway"
)

func TestLoadMigratesOnlyUnconfiguredGateway(t *testing.T) {
	token := strings.Repeat("ab", 32)
	for _, trial := range []struct {
		name     string
		settings Settings
		want     string
	}{
		{"unconfigured", Settings{Mode: "gateway"}, defaultMode()},
		{"configured gateway", Settings{Mode: "gateway", URL: "https://example.com/smartstage", Token: token}, "gateway"},
		{"explicit LAN", Settings{Mode: "lan"}, "lan"},
		{"LAN with saved gateway", Settings{Mode: "lan", URL: "https://example.com/smartstage", Token: token}, "lan"},
		{"explicit Cloudflare", Settings{Mode: "cloudflare", URL: "https://example.com/smartstage", Token: token}, "cloudflare"},
	} {
		t.Run(trial.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := save(dir, trial.settings); err != nil {
				t.Fatal(err)
			}
			got, err := Load(dir)
			if err != nil || got.Mode != trial.want || got.URL != trial.settings.URL || got.Token != trial.settings.Token {
				t.Fatalf("migration changed saved settings: mode=%s err=%v", got.Mode, err)
			}
		})
	}
}

func TestCloudflareSwitchPreservesGatewayAndWaitsForCleanup(t *testing.T) {
	if !cloudflared.Available() {
		t.Skip("official native helper unavailable")
	}
	dir := t.TempDir()
	old := Settings{Mode: "gateway", URL: "https://example.com/smartstage", Token: strings.Repeat("ab", 32)}
	// A closed fake gateway transport is not needed: start in explicit LAN with
	// saved gateway credentials, then exercise mode-only saves and actual stop.
	old.Mode = "lan"
	options := testOptions(dir)
	started, cleanup, stale := make(chan struct{}), make(chan struct{}), make(chan func())
	options.Cloudflare = func(ctx context.Context, connected func(string, string), status func(string, string)) error {
		status("downloading", "Downloading the Cloudflare helper…")
		connected("https://sample.trycloudflare.com/command#token=secret", "secret")
		close(started)
		stale <- func() { connected("https://stale.trycloudflare.com/command#token=old", "old") }
		<-ctx.Done()
		close(cleanup)
		return ctx.Err()
	}
	restart := false
	options.ReserveRestart = func() (func(), func(), error) { return func() { restart = true }, func() {}, nil }
	m, err := New(context.Background(), old, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Configure(Edit{Mode: "cloudflare"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tunnel did not start")
	}
	staleCallback := <-stale
	saved, err := Load(dir)
	if err != nil || saved.URL != old.URL || saved.Token != old.Token || saved.Mode != "cloudflare" {
		t.Fatal("mode switch lost gateway settings")
	}
	status := m.Status()
	if status.URL != old.URL || !status.HasToken || status.Status != "connected" {
		t.Fatalf("unexpected status: %s", status.Status)
	}
	data, _ := json.Marshal(status)
	if strings.Contains(string(data), old.Token) {
		t.Fatal("gateway credential exposed to browser")
	}
	if _, err = m.Configure(Edit{Mode: "lan"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleanup:
	default:
		t.Fatal("mode switch returned before helper cleanup")
	}
	staleCallback()
	if m.Status().Mode != "lan" || m.Status().RemoteURL != "" || !restart {
		t.Fatal("old helper republished link after mode switch")
	}
	saved, err = Load(dir)
	if err != nil || saved.URL != old.URL || saved.Token != old.Token {
		t.Fatal("LAN switch lost gateway settings")
	}
}

func TestCloudflareReconnectAndGatewaySwitchAwaitCleanup(t *testing.T) {
	if !cloudflared.Available() {
		t.Skip("official native helper unavailable")
	}
	type attempt struct {
		link, token string
		canceled    chan struct{}
		exited      chan struct{}
		release     chan struct{}
		once        sync.Once
		stale       func()
	}
	attempts := []*attempt{
		{canceled: make(chan struct{}), exited: make(chan struct{}), release: make(chan struct{})},
		{canceled: make(chan struct{}), exited: make(chan struct{}), release: make(chan struct{})},
	}
	started := make(chan *attempt, len(attempts))
	type publication struct{ mode, link, token string }
	published := make(chan publication, 8)
	gatewayRequests := make(chan struct{}, 8)
	orderingErrors := make(chan string, 8)
	var relay *gateway.Server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-attempts[1].exited:
		default:
			orderingErrors <- "gateway connected before Cloudflare cleanup finished"
		}
		gatewayRequests <- struct{}{}
		relay.ServeHTTP(w, r)
	}))
	defer server.Close()
	registrationToken := strings.Repeat("ab", 32)
	var err error
	relay, err = gateway.NewServer(gateway.Config{PublicURL: server.URL + "/smartstage", Token: registrationToken})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	dir := t.TempDir()
	options := testOptions(dir)
	options.HTTPClient = server.Client()
	options.HTTPClient.Timeout = 3 * time.Second
	options.Changed = func(mode, link, token string) {
		if link != "" {
			published <- publication{mode, link, token}
		}
	}
	run := 0
	options.Cloudflare = func(ctx context.Context, connected func(string, string), _ func(string, string)) error {
		if run >= len(attempts) {
			return fmt.Errorf("unexpected Cloudflare attempt")
		}
		current := attempts[run]
		run++
		current.token = auth.NewPublicCommand().CommandToken()
		current.link = fmt.Sprintf("https://attempt-%d.trycloudflare.com/command#token=%s", run, current.token)
		current.stale = func() { connected(current.link, current.token) }
		connected(current.link, current.token)
		started <- current
		<-ctx.Done()
		close(current.canceled)
		<-current.release // Hold cleanup to expose overlapping connection starts.
		close(current.exited)
		return ctx.Err()
	}
	m, err := New(context.Background(), Settings{Mode: "cloudflare", URL: server.URL + "/smartstage", Token: registrationToken}, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, current := range attempts {
			current.once.Do(func() { close(current.release) })
		}
		m.Close()
	})
	waitStarted := func() *attempt {
		t.Helper()
		select {
		case current := <-started:
			return current
		case <-time.After(3 * time.Second):
			t.Fatal("Cloudflare attempt did not start")
			return nil
		}
	}
	waitPublication := func(mode string) publication {
		t.Helper()
		select {
		case value := <-published:
			if value.mode != mode {
				t.Fatalf("published mode %q, want %q", value.mode, mode)
			}
			return value
		case <-time.After(3 * time.Second):
			t.Fatal("remote connection was not published")
			return publication{}
		}
	}
	first := waitStarted()
	firstPublication := waitPublication("cloudflare")
	reconnected := make(chan error, 1)
	go func() { _, err := m.Reconnect(); reconnected <- err }()
	select {
	case <-first.canceled:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not cancel the previous helper")
	}
	select {
	case err := <-reconnected:
		t.Fatalf("reconnect returned before cleanup: %v", err)
	case <-started:
		t.Fatal("new Cloudflare helper started before cleanup")
	case <-time.After(100 * time.Millisecond):
	}
	first.once.Do(func() { close(first.release) })
	select {
	case err := <-reconnected:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect did not finish after cleanup")
	}
	second := waitStarted()
	secondPublication := waitPublication("cloudflare")
	if firstPublication.link == secondPublication.link || firstPublication.token == secondPublication.token || len(secondPublication.token) != 64 {
		t.Fatal("reconnect retained the old remote link or pairing authority")
	}
	first.stale()
	if m.Status().RemoteURL != second.link {
		t.Fatal("previous helper republished its link after reconnect")
	}
	switched := make(chan error, 1)
	go func() { _, err := m.Configure(Edit{Mode: "gateway", URL: server.URL + "/smartstage"}); switched <- err }()
	select {
	case <-second.canceled:
	case <-time.After(time.Second):
		t.Fatal("gateway switch did not cancel Cloudflare")
	}
	select {
	case err := <-switched:
		t.Fatalf("gateway switch returned before Cloudflare cleanup: %v", err)
	case <-gatewayRequests:
		t.Fatal("gateway registration started before Cloudflare cleanup")
	case <-time.After(100 * time.Millisecond):
	}
	second.once.Do(func() { close(second.release) })
	select {
	case err := <-switched:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway switch did not finish after cleanup")
	}
	gatewayPublication := waitPublication("gateway")
	if gatewayPublication.token == first.token || gatewayPublication.token == second.token || len(gatewayPublication.token) != 64 {
		t.Fatal("gateway reused a Cloudflare pairing authority")
	}
	second.stale()
	if status := m.Status(); status.Mode != "gateway" || status.RemoteURL != gatewayPublication.link {
		t.Fatal("old Cloudflare helper replaced the gateway connection")
	}
	select {
	case message := <-orderingErrors:
		t.Fatal(message)
	default:
	}
	saved, err := Load(dir)
	if err != nil || saved.Mode != "gateway" || saved.URL != server.URL+"/smartstage" || saved.Token != registrationToken {
		t.Fatalf("gateway switch lost saved credentials: %v", err)
	}
}

func waitForCloudflareStatus(t *testing.T, manager *Manager, wanted string) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status()
		if status.Status == wanted {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Cloudflare status did not become %s", wanted)
	return Status{}
}

func TestCloudflareTimeoutDiagnosticPersistsAcrossProgressAndRecovery(t *testing.T) {
	if !cloudflared.Available() {
		t.Skip("official native helper unavailable")
	}
	const secret = "PRIVATE_HELPER_TOKEN"
	const privatePath = "/PRIVATE_HELPER_PATH"
	phases, advance := make(chan string, 1), make(chan struct{})
	options := testOptions(t.TempDir())
	attempt := 0
	options.Cloudflare = func(ctx context.Context, connected func(string, string), status func(string, string)) error {
		attempt++
		if attempt == 1 {
			return fmt.Errorf("https://private.example/?token=%s %s: %w", secret, privatePath, cloudflared.ErrStartupTimeout)
		}
		for _, phase := range []string{"downloading", "connecting"} {
			status(phase, "Ordinary helper progress")
			phases <- phase
			select {
			case <-advance:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		connected("https://recovered.trycloudflare.com/command#token=recovered", "recovered")
		phases <- "connected"
		<-ctx.Done()
		return ctx.Err()
	}
	m, err := New(context.Background(), Settings{Mode: "cloudflare"}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	initial := waitForCloudflareStatus(t, m, "error")
	if initial.Message != cloudflareTimeoutMessage {
		t.Fatal("wrapped timeout did not produce fixed network guidance")
	}
	for _, phase := range []string{"downloading", "connecting"} {
		select {
		case got := <-phases:
			if got != phase {
				t.Fatalf("progress phase %q, want %q", got, phase)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("automatic retry did not report progress")
		}
		current := m.Status()
		if current.Status != phase || current.Message != cloudflareTimeoutMessage || current.RemoteURL != "" {
			t.Fatal("retry progress hid the last timeout or advertised an unconnected link")
		}
		data, _ := json.Marshal(current)
		for _, value := range []string{secret, privatePath, "private.example"} {
			if strings.Contains(string(data), value) {
				t.Fatal("status disclosed raw helper error details")
			}
		}
		advance <- struct{}{}
	}
	select {
	case <-phases:
	case <-time.After(3 * time.Second):
		t.Fatal("successful retry did not connect")
	}
	current := m.Status()
	if current.Status != "connected" || current.Message == cloudflareTimeoutMessage || current.RemoteURL == "" {
		t.Fatal("successful connection retained timeout guidance")
	}
}

func TestCloudflareTimeoutDiagnosticResetsOnReconnectAndModeSwitch(t *testing.T) {
	if !cloudflared.Available() {
		t.Skip("official native helper unavailable")
	}
	for _, reset := range []string{"reconnect", "mode switch"} {
		t.Run(reset, func(t *testing.T) {
			options := testOptions(t.TempDir())
			options.ReserveRestart = func() (func(), func(), error) { return func() {}, func() {}, nil }
			started := make(chan struct{}, 1)
			attempt := 0
			options.Cloudflare = func(ctx context.Context, _ func(string, string), status func(string, string)) error {
				attempt++
				if attempt == 1 {
					return cloudflared.ErrStartupTimeout
				}
				status("connecting", "A fresh connection attempt")
				started <- struct{}{}
				<-ctx.Done()
				return cloudflared.ErrStartupTimeout // Cancellation must not publish a fresh failure.
			}
			m, err := New(context.Background(), Settings{Mode: "cloudflare"}, options)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if waitForCloudflareStatus(t, m, "error").Message != cloudflareTimeoutMessage {
				t.Fatal("timeout guidance was not recorded")
			}
			if reset == "reconnect" {
				if _, err := m.Reconnect(); err != nil {
					t.Fatal(err)
				}
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("explicit reconnect did not start")
				}
			} else if _, err := m.Configure(Edit{Mode: "lan"}); err != nil {
				t.Fatal(err)
			}
			if current := m.Status(); current.Message == cloudflareTimeoutMessage || current.Status == "error" {
				t.Fatal("explicit connection reset retained stale timeout guidance")
			}
		})
	}
}

func TestCloudflareCancellationDoesNotPublishTimeoutDiagnostic(t *testing.T) {
	if !cloudflared.Available() {
		t.Skip("official native helper unavailable")
	}
	options := testOptions(t.TempDir())
	started := make(chan struct{})
	options.Cloudflare = func(ctx context.Context, _ func(string, string), status func(string, string)) error {
		status("connecting", "Connecting without a timeout")
		close(started)
		<-ctx.Done()
		return cloudflared.ErrStartupTimeout
	}
	m, err := New(context.Background(), Settings{Mode: "cloudflare"}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("helper did not start")
	}
	m.Close()
	if current := m.Status(); current.Status == "error" || current.Message == cloudflareTimeoutMessage {
		t.Fatal("expected cancellation was reported as a connection timeout")
	}
}

func TestUnavailableCloudflareCannotBeSelectedOrOpenLAN(t *testing.T) {
	if cloudflared.Available() {
		t.Skip("this test runs on unsupported native targets")
	}
	dir := t.TempDir()
	options := testOptions(dir)
	called := false
	options.Cloudflare = func(context.Context, func(string, string), func(string, string)) error { called = true; return nil }
	m, err := New(context.Background(), Settings{Mode: "gateway"}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.Configure(Edit{Mode: "cloudflare"}); err == nil {
		t.Fatal("unsupported mode accepted")
	}
	if called || m.Status().CloudflareAvailable || m.Status().CloudflareReason == "" {
		t.Fatal("unsupported helper was started or not explained")
	}
	if _, err = os.Stat(filepath.Join(dir, "gateway.json")); !os.IsNotExist(err) {
		t.Fatal("rejected mode was persisted")
	}
}
