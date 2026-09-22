package httpapi

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/gateway"
	"smartstage/internal/identity"
	"smartstage/internal/web"
)

// These regressions use the actual TLS listener, websocket relay and command
// API. Only native playback is replaced by setupAPI's test backend.
type securityGateway struct {
	server   *httptest.Server
	client   *http.Client
	auth     *auth.Manager
	service  *app.Service
	prefix   string
	incoming chan struct{}
	reading  chan struct{}
}

type securityObservedBody struct {
	io.ReadCloser
	reading chan<- struct{}
	started bool
}

func (b *securityObservedBody) Read(p []byte) (int, error) {
	if !b.started {
		b.started = true
		b.reading <- struct{}{}
	}
	return b.ReadCloser.Read(p)
}

func newSecurityGateway(t *testing.T) *securityGateway {
	t.Helper()
	_, _, service, _ := setupAPI(t)
	f := &securityGateway{auth: auth.NewPublicCommand(), service: service, incoming: make(chan struct{}, 64), reading: make(chan struct{}, 64)}
	var relay *gateway.Server
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Incomplete") == "1" {
			f.incoming <- struct{}{}
			r.Body = &securityObservedBody{ReadCloser: r.Body, reading: f.reading}
		}
		relay.ServeHTTP(w, r)
	}))
	f.server.Config.ReadHeaderTimeout = 3 * time.Second
	f.server.Config.ReadTimeout = 15 * time.Second
	f.server.StartTLS()
	registrationToken, err := gateway.NewToken()
	if err != nil {
		f.server.Close()
		t.Fatal(err)
	}
	relay, err = gateway.NewServer(gateway.Config{PublicURL: f.server.URL + "/smartstage", Token: registrationToken})
	if err != nil {
		f.server.Close()
		t.Fatal(err)
	}
	client := *f.server.Client()
	client.Timeout = 3 * time.Second
	f.client = &client
	ctx, cancel := context.WithCancel(context.Background())
	connected := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- gateway.ServeConnection(ctx, gateway.ClientOptions{
			URL: f.server.URL + "/smartstage", Token: registrationToken, HTTPClient: f.server.Client(),
			Handler: func(prefix string) http.Handler {
				handler, err := NewGatewayCommand(service, f.auth, web.Handler(), f.server.URL+"/smartstage", prefix)
				if err != nil {
					t.Error(err)
					return nil
				}
				return handler
			},
			OnConnect: func(endpoint string) { connected <- endpoint },
		})
	}()
	t.Cleanup(func() {
		cancel()
		relay.Close()
		f.server.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("gateway client did not shut down")
		}
	})
	select {
	case endpoint := <-connected:
		f.prefix = strings.TrimSuffix(strings.TrimPrefix(endpoint, f.server.URL), "command")
	case err := <-done:
		// Keep cleanup's completion wait valid after consuming the result.
		done <- err
		t.Fatalf("gateway registration: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("gateway registration timed out")
	}
	return f
}

func (f *securityGateway) call(t *testing.T, method, path, body, csrf string, cookie *http.Cookie) (*http.Response, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, f.server.URL+f.prefix+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPost {
		r.Header.Set("Origin", f.server.URL)
		r.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	response, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, data
}

func (f *securityGateway) pair(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	response, body := f.call(t, http.MethodPost, "api/pair", `{"key":"`+f.auth.CommandToken()+`"}`, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("valid public token could not pair: %d %s", response.StatusCode, body)
	}
	var paired struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(body, &paired); err != nil || paired.CSRF == "" || len(response.Cookies()) != 1 {
		t.Fatalf("invalid pairing response: %s (decode error: %v)", body, err)
	}
	return response.Cookies()[0], paired.CSRF
}

func (f *securityGateway) incomplete(t *testing.T, path string, cookie *http.Cookie) net.Conn {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(f.server.Certificate())
	connection, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", f.server.Listener.Addr().String(), &tls.Config{RootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	cookieHeader := ""
	if cookie != nil {
		cookieHeader = "Cookie: " + cookie.String() + "\r\n"
	}
	_, err = fmt.Fprintf(connection, "POST %s%s HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nX-Test-Incomplete: 1\r\n%sContent-Type: application/json\r\nContent-Length: 1048576\r\n\r\n{", f.prefix, path, strings.TrimPrefix(f.server.URL, "https://"), f.server.URL, cookieHeader)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func TestGatewayPublicPairingSurvivesSharedPeerGuessPressure(t *testing.T) {
	f := newSecurityGateway(t)
	// The relay intentionally presents the same synthetic peer for every phone;
	// shared NATs have the same property. Keep attacking beyond the per-peer
	// limit without creating any legitimate browser session first.
	for i := 0; i < auth.AttemptsGlobal+10; i++ {
		response, body := f.call(t, http.MethodPost, "api/pair", `{"key":"incorrect"}`, "", nil)
		want := http.StatusUnauthorized
		if i >= auth.AttemptsPerIP {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Fatalf("invalid guess %d: got %d, want %d: %s", i+1, response.StatusCode, want, body)
		}
		if want == http.StatusTooManyRequests && response.Header.Get("Retry-After") != "60" {
			t.Fatal("rate-limited pairing omitted its retry interval")
		}
	}
	first, _ := f.pair(t)
	second, csrf := f.pair(t)
	if first.Value == second.Value {
		t.Fatal("separate phones received the same browser session")
	}
	for _, cookie := range []*http.Cookie{first, second} {
		response, body := f.call(t, http.MethodGet, "api/state", "", "", cookie)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("paired phone could not read state: %d %s", response.StatusCode, body)
		}
	}
	response, body := f.call(t, http.MethodPost, "api/stop", `{"requestId":"paired-under-guess-pressure"}`, csrf, second)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("paired phone could not stop: %d %s", response.StatusCode, body)
	}
	response, body = f.call(t, http.MethodPost, "api/pair", `{"key":"still-incorrect"}`, "", nil)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("successful pairing cleared the invalid-guess limit: %d %s", response.StatusCode, body)
	}
}

func TestGatewayFailedRepairPreservesPairedStop(t *testing.T) {
	f := newSecurityGateway(t)
	cookie, csrf := f.pair(t)
	response, body := f.call(t, http.MethodPost, "api/pair", `{"key":"incorrect"}`, "", cookie)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("incorrect replacement pairing: %d %s", response.StatusCode, body)
	}
	for _, path := range []string{"api/stop", "api/emergency-stop"} {
		before := f.service.Snapshot(false).StopEpoch
		response, body := f.call(t, http.MethodPost, path, `{"requestId":"stop-after-failed-repair-`+path[4:]+`"}`, csrf, cookie)
		if response.StatusCode != http.StatusAccepted || f.service.Snapshot(false).StopEpoch != before+1 {
			t.Fatalf("failed pairing revoked existing %s session: %d %s", path, response.StatusCode, body)
		}
	}
}

func TestGatewayStopSurvivesUnauthenticatedSlowBodies(t *testing.T) {
	for _, path := range []string{"api/stop", "api/emergency-stop"} {
		for _, forgedCookie := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/forged-cookie=%v", path, forgedCookie), func(t *testing.T) {
				f := newSecurityGateway(t)
				cookie, csrf := f.pair(t)
				var held []net.Conn
				// Four requests exhausted the complete reserved STOP lane before
				// the fix, even without a cookie, CSRF token or completed body.
				for i := 0; i < 4; i++ {
					var attackerCookie *http.Cookie
					if forgedCookie {
						attackerCookie = &http.Cookie{Name: cookie.Name, Value: identity.New()}
					}
					held = append(held, f.incomplete(t, path, attackerCookie))
				}
				for range held {
					select {
					case <-f.incoming:
					case <-time.After(3 * time.Second):
						t.Fatal("slow request did not reach gateway")
					}
				}
				before := f.service.Snapshot(false).StopEpoch
				response, body := f.call(t, http.MethodPost, path, `{"requestId":"stop-during-slow-bodies"}`, csrf, cookie)
				if response.StatusCode != http.StatusAccepted || f.service.Snapshot(false).StopEpoch != before+1 {
					t.Fatalf("authenticated STOP blocked by unauthenticated slow bodies: %d %s", response.StatusCode, body)
				}
				for _, connection := range held {
					if err := connection.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
						t.Fatal(err)
					}
					rejected, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
					if err != nil {
						t.Fatalf("gateway waited for an unauthenticated body instead of rejecting its headers: %v", err)
					}
					body, err := io.ReadAll(rejected.Body)
					rejected.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if rejected.StatusCode != http.StatusUnauthorized {
						t.Fatalf("unauthenticated incomplete STOP status: %d", rejected.StatusCode)
					}
					var result struct {
						Error struct {
							Code string `json:"code"`
						} `json:"error"`
					}
					if err := json.Unmarshal(body, &result); err != nil || result.Error.Code != "unpaired" {
						t.Fatalf("early rejection cannot reopen the remote pairing screen: %s (decode error: %v)", body, err)
					}
				}
			})
		}
	}
}

func TestGatewayStopSurvivesAnonymousPairingBodySaturation(t *testing.T) {
	f := newSecurityGateway(t)
	cookie, csrf := f.pair(t)
	// Public pairing must accept an unpaired request's body, but its bounded
	// read pool must not consume the reserved authenticated STOP pool.
	for i := 0; i < 32; i++ {
		f.incomplete(t, "api/pair", nil)
	}
	for i := 0; i < 32; i++ {
		select {
		case <-f.reading:
		case <-time.After(3 * time.Second):
			t.Fatal("incomplete pairing requests did not occupy body readers")
		}
	}
	response, body := f.call(t, http.MethodPost, "api/pair", `{"key":"`+f.auth.CommandToken()+`"}`, "", nil)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("pairing-body read pool was not bounded: %d %s", response.StatusCode, body)
	}
	for _, path := range []string{"api/stop", "api/emergency-stop"} {
		before := f.service.Snapshot(false).StopEpoch
		response, body := f.call(t, http.MethodPost, path, `{"requestId":"stop-during-pair-pressure-`+path[4:]+`"}`, csrf, cookie)
		if response.StatusCode != http.StatusAccepted || f.service.Snapshot(false).StopEpoch != before+1 {
			t.Fatalf("authenticated %s blocked by pairing bodies: %d %s", path, response.StatusCode, body)
		}
	}
}

func TestGatewayPairedBodyTimeoutReleasesStopCapacity(t *testing.T) {
	f := newSecurityGateway(t)
	cookie, csrf := f.pair(t)
	var held []net.Conn
	for i := 0; i < 4; i++ {
		held = append(held, f.incomplete(t, "api/stop", cookie))
	}
	for range held {
		select {
		case <-f.reading:
		case <-time.After(3 * time.Second):
			t.Fatal("paired incomplete requests did not occupy STOP body readers")
		}
	}
	response, body := f.call(t, http.MethodPost, "api/stop", `{"requestId":"full-stop-body-pool"}`, csrf, cookie)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("paired STOP body pool was not bounded: %d %s", response.StatusCode, body)
	}
	// The gateway's five-second body deadline must fire before this fixture's
	// fifteen-second server timeout, even for a legitimately paired browser.
	deadline := time.Now().Add(8 * time.Second)
	for _, connection := range held {
		if err := connection.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
		if err != nil {
			t.Fatalf("gateway did not bound an incomplete paired body: %v", err)
		}
		_, err = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("incomplete body did not fail with the body-read error: %d", response.StatusCode)
		}
	}
	for _, path := range []string{"api/stop", "api/emergency-stop"} {
		before := f.service.Snapshot(false).StopEpoch
		response, body := f.call(t, http.MethodPost, path, `{"requestId":"stop-after-body-timeout-`+path[4:]+`"}`, csrf, cookie)
		if response.StatusCode != http.StatusAccepted || f.service.Snapshot(false).StopEpoch != before+1 {
			t.Fatalf("body timeout did not restore %s capacity: %d %s", path, response.StatusCode, body)
		}
	}
}
