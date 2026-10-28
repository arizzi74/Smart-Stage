package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"smartstage/internal/cloudflared"
	"smartstage/internal/httpapi"
)

// A package test executable is also the guardian's executable during tests.
func init() {
	if handled, err := cloudflared.Guardian(os.Args[1:]); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// Exercise the actual listener/API wiring with an in-process helper stand-in;
// only the external Cloudflare process is replaced. No Admin route is mounted.
func TestCloudflareOriginLifecycle(t *testing.T) {
	l := newLANTestControl(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	origin := make(chan string, 1)
	ready := make(chan string, 1)
	control := &cloudflareControl{configDir: t.TempDir(), app: l.app, run: func(ctx context.Context, opts cloudflared.Options) error {
		origin <- opts.OriginURL
		opts.OnURL("https://fixture.trycloudflare.com")
		<-ctx.Done()
		return ctx.Err()
	}}
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- control.serve(ctx, func(link, _ string) { ready <- link }, func(string, string) {})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("origin cleanup timed out")
		}
	})
	local := <-origin
	link := <-ready
	u, _ := url.Parse(local)
	if u.Hostname() != "127.0.0.1" || u.Port() == "8787" {
		t.Fatal("wrong origin binding")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, u.Host)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	cloudflareCommandChecks(t, client, "http://fixture.trycloudflare.com", "https://fixture.trycloudflare.com", link, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("helper/origin cleanup timed out")
		}
	})
	if conn, err := net.DialTimeout("tcp", u.Host, 100*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("origin still listening after close")
	}
}

func TestCloudflareHelperReturnClosesOriginWithoutParentCancellation(t *testing.T) {
	helperError := errors.New("fixture helper exited")
	l := newLANTestControl(t)
	control := &cloudflareControl{configDir: t.TempDir(), app: l.app}
	var previousToken string
	for _, trial := range []struct {
		name     string
		announce bool
		err      error
	}{
		{"startup failure", false, helperError},
		{"connected failure", true, helperError},
		{"connected return", true, nil},
	} {
		t.Run(trial.name, func(t *testing.T) {
			parent, cancelParent := context.WithCancel(context.Background())
			origin, ready := make(chan string, 1), make(chan string, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			control.run = func(_ context.Context, opts cloudflared.Options) error {
				origin <- opts.OriginURL
				if trial.announce {
					opts.OnURL("https://fixture.trycloudflare.com")
				}
				<-release
				return trial.err
			}
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- control.serve(parent, func(link, _ string) { ready <- link }, func(string, string) {})
			}()
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				cancelParent()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("helper-return cleanup timed out")
				}
			})
			var local string
			select {
			case local = <-origin:
			case <-time.After(3 * time.Second):
				t.Fatal("origin listener did not start")
			}
			u, err := url.Parse(local)
			if err != nil {
				t.Fatal(err)
			}
			idle, err := net.DialTimeout("tcp", u.Host, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer idle.Close()
			finishHelper := func() {
				t.Helper()
				releaseOnce.Do(func() { close(release) })
				select {
				case err := <-done:
					if !errors.Is(err, trial.err) {
						t.Fatalf("serve returned %v, want %v", err, trial.err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("serve waited for parent cancellation after helper returned")
				}
				if err := parent.Err(); err != nil {
					t.Fatalf("cleanup canceled the caller's context: %v", err)
				}
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, u.Host)
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			if trial.announce {
				var link string
				select {
				case link = <-ready:
				case <-time.After(3 * time.Second):
					t.Fatal("helper did not publish a remote link")
				}
				parsed, _ := url.Parse(link)
				token := strings.TrimPrefix(parsed.Fragment, "token=")
				if previousToken != "" {
					if token == previousToken {
						t.Fatal("new helper run reused its previous pairing authority")
					}
					r, _ := http.NewRequest("POST", "http://fixture.trycloudflare.com/api/pair", strings.NewReader(`{"key":"`+previousToken+`"}`))
					r.Header.Set("Origin", "https://fixture.trycloudflare.com")
					r.Header.Set("Content-Type", "application/json")
					resp, err := client.Do(r)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusUnauthorized {
						t.Fatalf("new helper accepted previous pairing key: %d", resp.StatusCode)
					}
				}
				previousToken = token
				// This opens and reads a real authenticated WebSocket before
				// releasing the helper, then checks that it closes promptly.
				cloudflareCommandChecks(t, client, "http://fixture.trycloudflare.com", "https://fixture.trycloudflare.com", link, finishHelper)
			} else {
				resp, err := client.Get("http://fixture.trycloudflare.com/command")
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusServiceUnavailable {
					t.Fatalf("unready origin returned %d", resp.StatusCode)
				}
				finishHelper()
			}
			probe, err := net.Listen("tcp4", u.Host)
			if err != nil {
				t.Fatalf("origin port was not released before serve returned: %v", err)
			}
			probe.Close()
			idle.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := idle.Read(make([]byte, 1)); err == nil {
				t.Fatal("idle origin connection survived helper return")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("idle origin connection was not closed")
			}
		})
	}
}

func cloudflareCommandChecks(t *testing.T, client *http.Client, base, origin, link string, closeTunnel func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	request := func(method, path, body string, cookie *http.Cookie, csrf string) (*http.Response, []byte) {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", origin)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return resp, data
	}
	for _, path := range []string{"/admin", "/api/local-session", "/api/playlist", "/api/quit", "/api/gateway", "/api/files"} {
		resp, _ := request("GET", path, "", nil, "")
		if resp.StatusCode == 200 {
			t.Fatalf("private route exposed: %s", path)
		}
	}
	resp, _ := request("GET", "/api/state", "", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous state: %d", resp.StatusCode)
	}
	parsed, _ := url.Parse(link)
	token := strings.TrimPrefix(parsed.Fragment, "token=")
	if len(token) != 64 {
		t.Fatal("public remote pairing is not high entropy")
	}
	resp, data := request("POST", "/api/pair", `{"key":"`+token+`"}`, nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("pair: %d", resp.StatusCode)
	}
	var paired struct {
		CSRF      string `json:"csrfToken"`
		Role      string `json:"role"`
		Transport string `json:"liveTransport"`
	}
	if json.Unmarshal(data, &paired) != nil || paired.Role != "command" || paired.Transport != "websocket" {
		t.Fatal("wrong command transport/session")
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == httpapi.CommandCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.Secure || !cookie.HttpOnly {
		t.Fatal("missing secure command cookie")
	}
	wsURL := strings.Replace(strings.Replace(base, "https:", "wss:", 1), "http:", "ws:", 1) + "/api/ws"
	headers := http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
	if err != nil {
		t.Fatalf("WebSocket handshake: %v", err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "authenticate", "csrfToken": paired.CSRF}); err != nil {
		t.Fatal(err)
	}
	var state struct {
		Type  string         `json:"type"`
		State map[string]any `json:"state"`
	}
	if err := wsjson.Read(ctx, conn, &state); err != nil || state.Type != "state" {
		t.Fatalf("live state missing: %v", err)
	}
	// STOP remains an independent authenticated HTTP request while WS is open.
	resp, _ = request("POST", "/api/emergency-stop", `{"requestId":"cloudflare-lifecycle-stop"}`, cookie, paired.CSRF)
	if resp.StatusCode != 202 {
		t.Fatalf("STOP failed with live socket open: %d", resp.StatusCode)
	}
	closeTunnel()
	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	for {
		_, _, err = conn.Read(readCtx)
		if err != nil {
			break
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("WebSocket survived tunnel close")
	}
}

// Explicit opt-in performs a real account-free Quick Tunnel from this host,
// exposing only synthetic command state for the lifetime of this test.
func TestCloudflareLiveTunnel(t *testing.T) {
	if os.Getenv("SMARTSTAGE_TEST_CLOUDFLARE") != "1" {
		t.Skip("live public tunnel requires explicit opt-in")
	}
	if !cloudflared.Available() {
		t.Skip("no native helper")
	}
	l := newLANTestControl(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	control := &cloudflareControl{configDir: t.TempDir(), app: l.app}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- control.serve(ctx, func(link, _ string) { ready <- link }, func(state, _ string) { t.Log("helper status:", state) })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("live helper cleanup timed out")
		}
	})
	var link string
	select {
	case link = <-ready:
	case err := <-done:
		t.Fatalf("live tunnel failed: %v", err)
	case <-ctx.Done():
		t.Fatal("live tunnel connection timed out")
	}
	u, _ := url.Parse(link)
	base := "https://" + u.Host
	client := &http.Client{Timeout: 15 * time.Second}
	defer client.CloseIdleConnections()
	// DNS propagation can briefly return NXDOMAIN after edge registration;
	// local resolvers may retain that negative answer for about a minute.
	started := time.Now()
	deadline := started.Add(120 * time.Second)
	lastStatus := "no response"
	for {
		resp, err := client.Get(base + "/command")
		if err == nil {
			lastStatus = fmt.Sprintf("HTTP %d", resp.StatusCode)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				t.Logf("public DNS and command page ready after %.1fs", time.Since(started).Seconds())
				break
			}
		} else {
			var dnsError *net.DNSError
			var operationError *net.OpError
			var errno syscall.Errno
			var transportError *url.Error
			if errors.As(err, &dnsError) {
				lastStatus = fmt.Sprintf("DNS error (notFound=%v timeout=%v temporary=%v)", dnsError.IsNotFound, dnsError.IsTimeout, dnsError.IsTemporary)
			} else if errors.As(err, &operationError) && errors.As(err, &errno) {
				lastStatus = fmt.Sprintf("network %s: %s", operationError.Op, errno.Error())
			} else if errors.As(err, &transportError) {
				lastStatus = fmt.Sprintf("transport error %T", transportError.Err)
			} else {
				lastStatus = fmt.Sprintf("request error %T", err)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("public command page did not become reachable:", lastStatus)
		}
		time.Sleep(500 * time.Millisecond)
	}
	cloudflareCommandChecks(t, client, base, base, link, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("live helper did not exit")
		}
	})
	t.Log("real HTTPS pairing, private-route isolation, WebSocket status, HTTP STOP and tunnel cleanup passed")
}
