package cloudflared

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
)

// This opt-in test executes the real pinned native helper. Its provisioner,
// origin and both edge addresses are loopback fixtures: UDP packets are consumed
// without replying, and TCP acceptance proves the real helper reached HTTP2.
// No public tunnel, firewall change or simulated helper log is involved.
func TestPinnedBinaryFallsBackFromBlockedQUIC(t *testing.T) {
	binary, dir, _ := pinnedNativeTestHelper(t)
	var provisioned atomic.Int32
	provisioner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tunnel" {
			http.NotFound(w, r)
			return
		}
		provisioned.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"id":          "00000000-0000-4000-8000-000000000001",
				"hostname":    "fallback-fixture.trycloudflare.com",
				"account_tag": "synthetic-loopback-fixture",
				"secret":      make([]byte, 32),
			},
		})
	}))
	defer provisioner.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("synthetic command origin"))
	}))
	defer origin.Close()

	tcpAccepted := make(chan struct{}, 2)
	var udpPackets atomic.Int32
	var edges []string
	for range 2 {
		tcp, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer tcp.Close()
		address := tcp.Addr().(*net.TCPAddr)
		udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: address.IP, Port: address.Port})
		if err != nil {
			t.Fatal(err)
		}
		defer udp.Close()
		edges = append(edges, address.String())
		go func() {
			conn, err := tcp.Accept()
			if err == nil {
				_ = conn.Close()
				tcpAccepted <- struct{}{}
			}
		}()
		go func() {
			buffer := make([]byte, 2048)
			for {
				if _, _, err := udp.ReadFromUDP(buffer); err != nil {
					return
				}
				udpPackets.Add(1)
			}
		}()
	}
	config := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(config, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := append(helperArgs(origin.URL, config), "--quick-service", provisioner.URL,
		"--edge", edges[0], "--edge", edges[1], "--no-prechecks")
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = isolatedEnvironment(os.Environ(), dir)
	cmd.WaitDelay = 2 * time.Second
	cleanup := configureProcess(cmd)
	defer cleanup()
	// exec serializes writes when stdout and stderr share the same writer;
	// output is inspected only after Wait has reaped the helper.
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal("cannot start the pinned native fallback fixture")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("native fallback fixture cleanup timed out")
		}
	}()
	select {
	case <-tcpAccepted:
	case <-done:
		t.Fatal("pinned helper stopped before falling back to the loopback TCP edge")
	case <-ctx.Done():
		t.Fatal("pinned helper did not reach HTTP2 within the startup budget")
	}
	elapsed := time.Since(started)
	cancel()
	<-done
	var initialQUIC, switchedHTTP2 bool
	quicFailures := 0
	for _, line := range bytes.Split(output.Bytes(), []byte{'\n'}) {
		var entry struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		switch entry.Message {
		case "Initial protocol quic":
			initialQUIC = true
		case "Failed to dial a quic connection":
			quicFailures++
		case "Switching to fallback protocol http2":
			switchedHTTP2 = true
		}
	}
	if !initialQUIC || !switchedHTTP2 || quicFailures != 3 || udpPackets.Load() == 0 || provisioned.Load() != 1 {
		t.Fatalf("native transport behavior differs: initialQUIC=%v switchedHTTP2=%v failedQUICDials=%d UDPObserved=%v provisioningRequests=%d",
			initialQUIC, switchedHTTP2, quicFailures, udpPackets.Load() > 0, provisioned.Load())
	}
	t.Logf("pinned native helper tried QUIC, fell back after exactly three failed UDP dials, and reached the loopback TCP edge in %.1fs", elapsed.Seconds())
}
