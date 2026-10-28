package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/identity"
	"smartstage/internal/model"
	"smartstage/internal/playback"
	"smartstage/internal/web"
)

// TestCloudflareBrowser exercises the real command API and browser over TLS.
// Cloudflare's external proxy and native playback are outside this fixture;
// the helper lifecycle and an opt-in real tunnel have separate tests.
func TestCloudflareBrowser(t *testing.T) {
	if os.Getenv("SMARTSTAGE_TEST_CLOUDFLARE_BROWSER") != "1" {
		t.Skip("set SMARTSTAGE_TEST_CLOUDFLARE_BROWSER=1 with Playwright installed")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "PRIVATE_CLOUDFLARE_BROWSER.wav")
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
	cfg.Cues = []model.Cue{{ID: "cloudflare-browser-cue", Label: "Cloudflare browser cue", Path: mediaPath, Cache: model.Validation{Status: "ready", Media: playback.Media{Kind: "audio", Duration: 30, HasAudio: true}}}}
	service := app.New(backend, fs, discard{}, cfg)
	defer service.Close()
	authentication := auth.NewPublicCommand()
	var handler *API
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer public.Close()
	handler, err = NewCloudflareCommand(service, authentication, web.Handler(), public.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	var disconnects, revocations atomic.Int32
	// A separate loopback controller induces actual socket loss and session
	// revocation. It never replaces a browser request or a command API response.
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/disconnect":
			handler.websockets.mu.Lock()
			connections := make([]net.Conn, 0, len(handler.websockets.connections))
			for connection := range handler.websockets.connections {
				connections = append(connections, connection)
			}
			handler.websockets.mu.Unlock()
			for _, connection := range connections {
				_ = connection.Close()
			}
			disconnects.Add(1)
		case "/revoke":
			var body struct {
				Session string `json:"session"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil || body.Session == "" {
				http.Error(w, "Invalid test session", http.StatusBadRequest)
				return
			}
			authentication.Logout(body.Session)
			_, _ = service.Stop(app.StopRequest{RequestID: identity.New()})
			revocations.Add(1)
		default:
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	defer control.Close()
	input, _ := json.Marshal(map[string]string{
		"publicURL": public.URL, "controlURL": control.URL, "token": authentication.CommandToken(),
		"outputDir": filepath.Join(root, "dist", "cloudflare-browser-checks"),
	})
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "cloudflare-browser-smoke.cjs"))
	command.Dir = root
	command.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(root, "scripts", "browser", "node_modules"))
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real Cloudflare command browser check: %v\n%s", err, output)
	}
	t.Log(string(output))
	if backend.played.Load() < 2 || backend.stopped.Load() < 2 || disconnects.Load() != 1 || revocations.Load() != 1 {
		t.Fatalf("browser did not exercise the real service and lifecycle: play=%d stop=%d disconnects=%d revocations=%d", backend.played.Load(), backend.stopped.Load(), disconnects.Load(), revocations.Load())
	}
}
