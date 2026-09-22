package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"smartstage/internal/auth"
	"smartstage/internal/gateway"
)

func testOptions(dir string) Options {
	return Options{ConfigDir: dir, Handler: func(string, string, *auth.Manager) http.Handler { return http.NotFoundHandler() }}
}
func TestDefaultIsClosedUnconfiguredGateway(t *testing.T) {
	dir := t.TempDir()
	settings, err := Load(dir)
	if err != nil || settings.Mode != "gateway" {
		t.Fatalf("default %+v %v", settings, err)
	}
	options := testOptions(dir)
	closed := false
	options.DisableLAN = func() error { closed = true; return nil }
	m, err := New(context.Background(), settings, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if !closed || m.Status().Status != "unconfigured" || m.Status().HasToken || m.Status().RemoteURL != "" {
		t.Fatalf("default status %+v", m.Status())
	}
	if _, err := m.Reconnect(); err == nil {
		t.Fatal("unconfigured reconnect accepted")
	}
}
func TestBlankTokenPreservesCredentialAcrossGatewayEdits(t *testing.T) {
	dir := t.TempDir()
	token := strings.Repeat("ab", 32)
	replacement := strings.Repeat("cd", 32)
	m, err := New(context.Background(), Settings{Mode: "lan", URL: "https://stage.example/smartstage", Token: token}, testOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, trial := range []struct {
		name, url, token, wantURL, wantToken string
	}{
		{"same gateway", "https://STAGE.example:443/smartstage/", "", "https://stage.example/smartstage", token},
		{"changed hostname", " https://different.example/smartstage/ ", " \t ", "https://different.example/smartstage", token},
		{"explicit replacement", "https://third.example/smartstage", " " + replacement + " ", "https://third.example/smartstage", replacement},
		{"LAN blank fields", "", "", "https://third.example/smartstage", replacement},
	} {
		t.Run(trial.name, func(t *testing.T) {
			status, err := m.Configure(Edit{Mode: "lan", URL: trial.url, Token: trial.token})
			if err != nil || !status.HasToken || status.URL != trial.wantURL {
				t.Fatalf("gateway edit failed: %v", err)
			}
			saved, err := Load(dir)
			if err != nil || saved.URL != trial.wantURL || saved.Token != trial.wantToken || saved.Mode != "lan" {
				t.Fatalf("gateway edit was not persisted correctly: %v", err)
			}
			data, err := json.Marshal(status)
			if err != nil || strings.Contains(string(data), token) || strings.Contains(string(data), replacement) {
				t.Fatal("registration credential echoed in status")
			}
		})
	}
}

func TestGatewayEditFailuresRetainSavedSettings(t *testing.T) {
	dir := t.TempDir()
	old := Settings{Mode: "lan", URL: "https://stage.example/smartstage", Token: strings.Repeat("ab", 32)}
	if err := save(dir, old); err != nil {
		t.Fatal(err)
	}
	m, err := New(context.Background(), old, testOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	beforeStatus := m.Status()
	beforeFile, err := os.ReadFile(filepath.Join(dir, "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, trial := range []struct {
		name string
		edit Edit
	}{
		{"HTTP URL", Edit{Mode: "gateway", URL: "http://different.example/smartstage"}},
		{"invalid path", Edit{Mode: "gateway", URL: "https://different.example/private"}},
		{"missing gateway URL", Edit{Mode: "gateway"}},
		{"invalid replacement token", Edit{Mode: "gateway", URL: "https://different.example/smartstage", Token: "not-a-valid-token"}},
		{"save failure", Edit{Mode: "gateway", URL: "https://different.example/smartstage"}},
	} {
		t.Run(trial.name, func(t *testing.T) {
			if trial.name == "save failure" {
				// An existing regular file cannot be the destination directory.
				// This forces an I/O failure on every OS without changing the saved file.
				m.options.ConfigDir = filepath.Join(dir, "gateway.json")
				defer func() { m.options.ConfigDir = dir }()
			}
			if _, err := m.Configure(trial.edit); err == nil {
				t.Fatal("invalid or unsaved gateway edit succeeded")
			}
			if m.Status() != beforeStatus || m.settings != old {
				t.Fatal("failed edit changed the active gateway settings")
			}
			afterFile, err := os.ReadFile(filepath.Join(dir, "gateway.json"))
			if err != nil || string(beforeFile) != string(afterFile) {
				t.Fatal("failed edit changed the saved gateway settings")
			}
		})
	}
}

func TestFirstGatewaySetupStillRequiresToken(t *testing.T) {
	dir := t.TempDir()
	m, err := New(context.Background(), Settings{Mode: "gateway"}, testOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, token := range []string{"", " \t ", "not-a-valid-token"} {
		if _, err := m.Configure(Edit{Mode: "gateway", URL: "https://stage.example/smartstage", Token: token}); err == nil {
			t.Fatal("first gateway setup accepted a missing or invalid token")
		}
		if m.Status().HasToken || m.Status().Status != "unconfigured" {
			t.Fatal("failed initial setup changed gateway state")
		}
		if _, err := os.Stat(filepath.Join(dir, "gateway.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed initial setup persisted credentials")
		}
	}
}

func TestURLOnlySaveReconnectsToNewGatewayHostname(t *testing.T) {
	token := strings.Repeat("ab", 32)
	addresses := make(map[string]string)
	roots := x509.NewCertPool()
	for _, host := range []string{"old.example.com", "new.example.com"} {
		var relay *gateway.Server
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { relay.ServeHTTP(w, r) }))
		t.Cleanup(server.Close)
		var err error
		relay, err = gateway.NewServer(gateway.Config{PublicURL: "https://" + host + "/smartstage", Token: token})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { relay.Close() })
		addresses[host+":443"] = server.Listener.Addr().String()
		roots.AddCert(server.Certificate())
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			mapped, ok := addresses[address]
			if !ok {
				return nil, fmt.Errorf("unexpected test destination: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, mapped)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	connected := make(chan string, 4)
	dir := t.TempDir()
	options := testOptions(dir)
	options.HTTPClient = client
	options.Changed = func(_, remoteURL, _ string) {
		if remoteURL != "" {
			connected <- remoteURL
		}
	}
	options.Handler = func(publicURL, _ string, _ *auth.Manager) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, publicURL) })
	}
	committed := false
	options.ReserveRestart = func() (func(), func(), error) { return func() { committed = true }, func() {}, nil }
	m, err := New(context.Background(), Settings{Mode: "gateway", URL: "https://old.example.com/smartstage", Token: token}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	waitConnected := func(origin string) string {
		t.Helper()
		select {
		case link := <-connected:
			if !strings.HasPrefix(link, origin+"/smartstage/e/") {
				t.Fatal("gateway connection used the wrong hostname")
			}
			return strings.SplitN(link, "#", 2)[0]
		case <-time.After(5 * time.Second):
			t.Fatal("gateway connection timed out")
			return ""
		}
	}
	oldLink := waitConnected("https://old.example.com")
	if _, err := m.Configure(Edit{Mode: "gateway", URL: "https://new.example.com/smartstage"}); err != nil {
		t.Fatal(err)
	}
	newLink := waitConnected("https://new.example.com")
	response, err := client.Get(newLink)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "https://new.example.com/smartstage" {
		t.Fatal("new registered endpoint did not route to the selected gateway")
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		response, err := client.Get(oldLink)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode == http.StatusGone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old gateway endpoint remained connected after URL change")
		}
		time.Sleep(10 * time.Millisecond)
	}
	saved, err := Load(dir)
	if err != nil || saved.URL != "https://new.example.com/smartstage" || saved.Token != token || saved.Mode != "gateway" {
		t.Fatal("new gateway URL did not persist the existing credential")
	}
	status, err := m.Configure(Edit{Mode: "lan"})
	if err != nil || !status.Restart || !committed {
		t.Fatalf("LAN switch did not reserve its restart: %v", err)
	}
	saved, err = Load(dir)
	if err != nil || saved.URL != "https://new.example.com/smartstage" || saved.Token != token || saved.Mode != "lan" {
		t.Fatal("LAN switch with blank fields lost saved gateway settings")
	}
}
func TestLANRestartReservationAndSaveFailure(t *testing.T) {
	dir := t.TempDir()
	options := testOptions(dir)
	reserved, committed, aborted := 0, 0, 0
	options.ReserveRestart = func() (func(), func(), error) { reserved++; return func() { committed++ }, func() { aborted++ }, nil }
	m, err := New(context.Background(), Settings{Mode: "gateway"}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// A directory occupying the settings file causes an atomic rename failure.
	os.Mkdir(filepath.Join(dir, "gateway.json"), 0700)
	if _, err := m.Configure(Edit{Mode: "lan"}); err == nil {
		t.Fatal("save should fail")
	}
	if reserved != 1 || committed != 0 || aborted != 1 || m.Status().Mode != "gateway" {
		t.Fatal("failed save committed restart")
	}
	os.Remove(filepath.Join(dir, "gateway.json"))
	s, err := m.Configure(Edit{Mode: "lan"})
	if err != nil || !s.Restart || committed != 1 {
		t.Fatal(s, err)
	}
	if _, err := m.Configure(Edit{Mode: "lan"}); err == nil {
		t.Fatal("accepted edit during restart")
	}
	saved, err := Load(dir)
	if err != nil || saved.Mode != "lan" {
		t.Fatal(saved, err)
	}
}
func TestUnavailableRestartDoesNotPersistLAN(t *testing.T) {
	dir := t.TempDir()
	options := testOptions(dir)
	options.ReserveRestart = func() (func(), func(), error) { return nil, nil, errors.New("stop playback first") }
	m, err := New(context.Background(), Settings{Mode: "gateway"}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Configure(Edit{Mode: "lan"}); err == nil {
		t.Fatal("restart reservation ignored")
	}
	if _, err := os.Stat(filepath.Join(dir, "gateway.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected edit persisted")
	}
}
func TestPrivateSettingsValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.json")
	for _, body := range []string{`null`, `{"mode":"internet"}`, `{"mode":"lan"} {}`, `{"mode":"gateway","unknown":true}`, `{"mode":"gateway","url":"http://example.com/smartstage","token":"` + strings.Repeat("ab", 32) + `"}`} {
		os.WriteFile(path, []byte(body), 0600)
		if _, err := Load(dir); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if runtime.GOOS != "windows" {
		os.WriteFile(path, []byte(`{"mode":"gateway","url":"https://example.com/smartstage","token":"`+strings.Repeat("ab", 32)+`"}`), 0600)
		os.Chmod(path, 0644)
		if _, err := Load(dir); err == nil {
			t.Fatal("accepted publicly readable registration credentials")
		}
	}
}
