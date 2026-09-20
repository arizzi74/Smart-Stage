package gatewayinstall

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"smartstage/internal/gateway"
)

// Exercise production gateway registration and streaming through the actual
// proxy executables and the generated proxy blocks. Only isolated, unprivileged
// loopback listeners and temporary self-signed test certificates are used.
func TestRealReverseProxiesRelayWebSocketAndSSE(t *testing.T) {
	for _, kind := range []string{"nginx", "caddy"} {
		t.Run(kind, func(t *testing.T) { testRealProxy(t, kind) })
	}
}

func testRealProxy(t *testing.T, kind string) {
	executable, err := exec.LookPath(kind)
	if err != nil {
		t.Skip(kind + " is unavailable")
	}
	dir := t.TempDir()
	cert, key := testCertificate(t)
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress := reservation.Addr().String()
	reservation.Close()
	_, port, _ := net.SplitHostPort(proxyAddress)
	const origin = "https://show.example.com"
	token := strings.Repeat("ab", 32)
	relay, err := gateway.NewServer(gateway.Config{PublicURL: origin + "/smartstage", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	backend := httptest.NewServer(relay)
	defer backend.Close()
	backendAddress := strings.TrimPrefix(backend.URL, "http://")
	var command *exec.Cmd
	if kind == "nginx" {
		proxy := strings.ReplaceAll(nginxProxy, "127.0.0.1:8790", backendAddress)
		config := fmt.Sprintf("daemon off; master_process off; error_log %s warn; pid %s; events {} http { access_log off; client_body_temp_path %s; proxy_temp_path %s; fastcgi_temp_path %s; uwsgi_temp_path %s; scgi_temp_path %s; server { listen %s ssl; server_name show.example.com; ssl_certificate %s; ssl_certificate_key %s; %s } }\n", filepath.Join(dir, "error.log"), filepath.Join(dir, "nginx.pid"), filepath.Join(dir, "body"), filepath.Join(dir, "proxy"), filepath.Join(dir, "fastcgi"), filepath.Join(dir, "uwsgi"), filepath.Join(dir, "scgi"), proxyAddress, certPath, keyPath, proxy)
		path := filepath.Join(dir, "nginx.conf")
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		command = exec.Command(executable, "-p", dir, "-c", path)
	} else {
		data, err := caddyConfiguration(nil, "show.example.com")
		if err != nil {
			t.Fatal(err)
		}
		config := "{\n admin off\n auto_https off\n}\n" + string(data)
		config = strings.Replace(config, "show.example.com {", fmt.Sprintf("https://show.example.com:%s {\n bind 127.0.0.1\n tls %s %s", port, certPath, keyPath), 1)
		config = strings.ReplaceAll(config, "127.0.0.1:8790", backendAddress)
		path := filepath.Join(dir, "Caddyfile")
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		command = exec.Command(executable, "run", "--config", path, "--adapter", "caddyfile")
		command.Env = append(os.Environ(), "XDG_DATA_HOME="+filepath.Join(dir, "data"), "XDG_CONFIG_HOME="+filepath.Join(dir, "config"))
	}
	logFile, err := os.Create(filepath.Join(dir, "proxy.log"))
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	t.Cleanup(func() {
		command.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("proxy did not exit")
		}
		logFile.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logFile.Name())
			t.Logf("%s proxy log:\n%s", kind, data)
		}
	})
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		t.Fatal("test certificate failed")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "show.example.com:443" {
			return nil, fmt.Errorf("unexpected test destination %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, proxyAddress)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	ready := false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get(origin + "/smartstage/health")
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("reverse proxy failed to become ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connected := make(chan string, 1)
	done := make(chan error, 1)
	requestCanceled := make(chan struct{}, 1)
	go func() {
		done <- gateway.ServeConnection(ctx, gateway.ClientOptions{URL: origin + "/smartstage", Token: token, HTTPClient: client, OnConnect: func(endpoint string) { connected <- endpoint }, Handler: func(prefix string) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil || r.Host != "show.example.com" {
					http.Error(w, "wrong origin", 400)
					return
				}
				switch r.URL.Path {
				case "/command":
					io.WriteString(w, "proxied remote")
				case "/api/stop":
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"accepted":true}`)
				case "/api/events":
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "event: state\ndata: {\"playing\":true}\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					requestCanceled <- struct{}{}
				default:
					http.NotFound(w, r)
				}
			})
		}})
	}()
	var endpoint string
	select {
	case endpoint = <-connected:
	case err := <-done:
		t.Fatalf("WebSocket registration through %s: %v", kind, err)
	case <-time.After(6 * time.Second):
		t.Fatal("WebSocket registration timed out")
	}
	response, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(body) != "proxied remote" {
		t.Fatalf("remote page: %d %q", response.StatusCode, body)
	}
	base := strings.TrimSuffix(endpoint, "/command")
	for _, path := range []string{"/admin", "/api/files", "/api/quit"} {
		response, err = client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("private route %s escaped proxy relay: %d", path, response.StatusCode)
		}
	}
	request, err := http.NewRequest(http.MethodPost, base+"/api/stop", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(body) != `{"accepted":true}` {
		t.Fatalf("STOP through relay: %d %q", response.StatusCode, body)
	}
	started := time.Now()
	response, err = client.Get(base + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || line != "event: state\n" || time.Since(started) > 2*time.Second {
		t.Fatalf("SSE was buffered: status=%d first=%q elapsed=%s", response.StatusCode, line, time.Since(started))
	}
	response.Body.Close()
	select {
	case <-requestCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE cancellation did not reach host")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("host WebSocket did not close")
	}
}
