package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"smartstage/internal/auth"
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
func TestTokenPreservedOnlyForSameGateway(t *testing.T) {
	dir := t.TempDir()
	token := strings.Repeat("ab", 32)
	m, err := New(context.Background(), Settings{Mode: "lan", URL: "https://stage.example/smartstage", Token: token}, testOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	status, err := m.Configure(Edit{Mode: "lan", URL: "https://stage.example/smartstage/"})
	if err != nil || !status.HasToken {
		t.Fatal(status, err)
	}
	saved, err := Load(dir)
	if err != nil || saved.Token != token {
		t.Fatal("token lost", err)
	}
	data, _ := json.Marshal(status)
	if strings.Contains(string(data), token) {
		t.Fatal("secret echoed in status")
	}
	if _, err := m.Configure(Edit{Mode: "lan", URL: "https://different.example/smartstage"}); err == nil {
		t.Fatal("token reused on another gateway")
	}
	saved, _ = Load(dir)
	if saved.URL != "https://stage.example/smartstage" {
		t.Fatal("failed edit replaced saved settings")
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
