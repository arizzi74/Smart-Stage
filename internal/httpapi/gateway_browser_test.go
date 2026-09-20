package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	qr "github.com/piglig/go-qr"
	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/gateway"
	"smartstage/internal/model"
	"smartstage/internal/playback"
	"smartstage/internal/remote"
	"smartstage/internal/web"
)

// This test exercises the actual TLS relay, host tunnel, HTTP API and browser
// assets. Only the native playback boundary and OS wake-lock grant are fakes.
// It runs explicitly in the browser CI job, where Playwright is installed.
func TestGatewayBrowser(t *testing.T) {
	if os.Getenv("SMARTSTAGE_TEST_GATEWAY_BROWSER") != "1" {
		t.Skip("set SMARTSTAGE_TEST_GATEWAY_BROWSER=1 with Playwright installed")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "gateway-browser.wav")
	if err := os.WriteFile(mediaPath, []byte("deterministic playback boundary fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	fs, err := files.New([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	backend := &gatewayBrowserBackend{events: make(chan playback.Event, 128)}
	cfg := model.DefaultConfig()
	cfg.Outputs = model.Outputs{AudioID: "audio", DisplayID: "display", AllowPrimary: true}
	cfg.Cues = []model.Cue{{ID: "gateway-browser-cue", Label: "Gateway browser cue", Path: mediaPath, Cache: model.Validation{Status: "ready", Media: playback.Media{Kind: "audio", Duration: 30, HasAudio: true}}}}
	service := app.New(backend, fs, discard{}, cfg)
	defer service.Close()
	registrationToken, err := gateway.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	var relay *gateway.Server
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { relay.ServeHTTP(w, r) }))
	defer public.Close()
	relay, err = gateway.NewServer(gateway.Config{PublicURL: public.URL + "/smartstage", Token: registrationToken})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	var admin *API
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { admin.ServeHTTP(w, r) }))
	defer local.Close()
	port, err := strconv.Atoi(local.Listener.Addr().String()[len("127.0.0.1:"):])
	if err != nil {
		t.Fatal(err)
	}
	adminAuth := auth.New()
	admin = NewAdmin(service, adminAuth, web.Handler(), []string{"127.0.0.1"}, port)
	var disables atomic.Int32
	manager, err := remote.New(t.Context(), remote.Settings{Mode: "gateway"}, remote.Options{
		ConfigDir: dir, HTTPClient: public.Client(), DisableLAN: func() error { disables.Add(1); return nil },
		Handler: func(publicURL, prefix string, a *auth.Manager) http.Handler {
			handler, e := NewGatewayCommand(service, a, web.Handler(), publicURL, prefix)
			if e != nil {
				t.Error(e)
				return http.NotFoundHandler()
			}
			return handler
		},
		Changed: func(mode, link, token string) {
			var links []RemoteLink
			if link != "" {
				links = []RemoteLink{{Label: "Public gateway · HTTPS", URL: link}}
			}
			admin.SetRemoteControl(links, token, mode)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	admin.SetGateway(manager)
	input, _ := json.Marshal(map[string]string{"adminURL": local.URL, "gatewayURL": public.URL + "/smartstage", "registrationToken": registrationToken, "outputDir": filepath.Join(root, "dist", "gateway-browser-checks")})
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "gateway-browser-smoke.cjs"))
	command.Dir = root
	command.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(root, "scripts", "browser", "node_modules"))
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real gateway browser check: %v\n%s", err, output)
	}
	t.Log(string(output))
	if backend.played.Load() < 1 || backend.stopped.Load() < 1 || disables.Load() < 2 {
		t.Fatalf("real service was not controlled: play=%d stop=%d LAN disabled=%d", backend.played.Load(), backend.stopped.Load(), disables.Load())
	}
	if manager.Status().Status != "connected" {
		t.Fatal("browser logout incorrectly disconnected host tunnel")
	}
	// Independently decode the actual Admin QR, including its 256-bit pairing
	// secret and endpoint path, using the already-vendored QR decoder.
	session, _ := adminAuth.LocalAdmin("")
	r := httptest.NewRequest("GET", local.URL+"/api/remote-control/qr?index=0", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.AddCookie(&http.Cookie{Name: AdminCookie, Value: session.ID})
	w := httptest.NewRecorder()
	admin.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("Admin QR failed: %d", w.Code)
	}
	img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := qr.Decode(img)
	if err != nil || decoded != manager.Status().RemoteURL {
		t.Fatal("Admin QR did not decode to the actual public pairing URL", err)
	}
	t.Log("Actual Admin QR round trip passed; native playback and device wake-lock grants remain simulated.")
}

type gatewayBrowserBackend struct {
	events          chan playback.Event
	played, stopped atomic.Int32
}

func (b *gatewayBrowserBackend) Devices(context.Context) (playback.Devices, error) {
	return playback.Devices{Audio: []playback.AudioDevice{{ID: "audio", Name: "Test audio", Default: true}}, Displays: []playback.Display{{ID: "display", Name: "Test stage", Width: 1920, Height: 1080, Primary: true}}}, nil
}
func (b *gatewayBrowserBackend) Inspect(context.Context, string) (playback.Media, error) {
	return playback.Media{Kind: "audio", Duration: 30, HasAudio: true}, nil
}
func (b *gatewayBrowserBackend) Start(playback.Start) error       { return nil }
func (b *gatewayBrowserBackend) Stop(uint64) error                { return nil }
func (b *gatewayBrowserBackend) Stage(uint64, string, bool) error { return nil }
func (b *gatewayBrowserBackend) Events() <-chan playback.Event    { return b.events }
func (b *gatewayBrowserBackend) Close() error                     { return nil }
func (b *gatewayBrowserBackend) ApplyScene(s playback.Scene) error {
	b.events <- playback.Event{Kind: "stage", Generation: s.Generation, SceneRevision: s.Revision, StageEnabled: s.StageEnabled}
	if s.ForegroundID != 0 && s.ForegroundPath != "" {
		b.played.Add(1)
		b.events <- playback.Event{Kind: "playing", Generation: s.ForegroundID, SceneRevision: s.Revision, Duration: 30, StageEnabled: s.StageEnabled}
	} else {
		b.stopped.Add(1)
		b.events <- playback.Event{Kind: "stopped", Generation: s.Generation, SceneRevision: s.Revision, StageEnabled: s.StageEnabled}
	}
	return nil
}
