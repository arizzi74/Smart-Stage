package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/httpapi"
	"smartstage/internal/model"
	"smartstage/internal/playback"
)

type remoteTestBackend struct{ events chan playback.Event }

func (b *remoteTestBackend) Devices(context.Context) (playback.Devices, error) {
	return playback.Devices{}, nil
}
func (b *remoteTestBackend) Inspect(context.Context, string) (playback.Media, error) {
	return playback.Media{}, nil
}
func (b *remoteTestBackend) Start(playback.Start) error       { return nil }
func (b *remoteTestBackend) Stop(uint64) error                { return nil }
func (b *remoteTestBackend) Stage(uint64, string, bool) error { return nil }
func (b *remoteTestBackend) Events() <-chan playback.Event    { return b.events }
func (b *remoteTestBackend) Close() error                     { return nil }

type remoteTestSaver struct{}

func (remoteTestSaver) Save(model.Config) error { return nil }

func newLANTestControl(t *testing.T) *lanControl {
	t.Helper()
	fs, err := files.New([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(&remoteTestBackend{events: make(chan playback.Event, 16)}, fs, remoteTestSaver{}, model.DefaultConfig())
	t.Cleanup(service.Close)
	l := &lanControl{bind: "127.0.0.1", app: service, auth: auth.New(), errors: make(chan error, 1)}
	t.Cleanup(func() { l.disable() })
	return l
}

func TestGatewayModeClosesLANListenerAndExistingStreams(t *testing.T) {
	l := newLANTestControl(t)
	if err := l.enable(); err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(l.port))
	session, err := l.auth.PairCommand(l.auth.CommandToken(), "test-phone")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	r, _ := http.NewRequest("GET", "http://"+address+"/api/events", nil)
	r.AddCookie(&http.Cookie{Name: httpapi.CommandCookie, Value: session.ID})
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("could not open authenticated LAN stream: %d", resp.StatusCode)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || len(line) == 0 {
		t.Fatalf("missing first LAN event: %q %v", line, err)
	}
	// Keep an additional TCP connection open without an HTTP request: disabling
	// LAN must also terminate idle sockets, not merely unpublish its QR code.
	idle, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	if err := l.disable(); err != nil {
		t.Fatal(err)
	}
	if l.server != nil || l.api != nil {
		t.Fatal("LAN handler retained after disable")
	}
	if conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond); err == nil {
		conn.Close()
		t.Fatal("gateway mode retained an inbound LAN socket")
	}
	ended := make(chan struct{})
	go func() { io.Copy(io.Discard, resp.Body); close(ended) }()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("gateway mode retained the existing LAN event stream")
	}
	idle.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := idle.Read(make([]byte, 1)); err == nil {
		t.Fatal("idle LAN socket remains usable")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("idle LAN socket was not closed")
	}
	// An explicit later LAN enable can reuse the port normally.
	if err := l.enable(); err != nil {
		t.Fatalf("explicit LAN re-enable failed: %v", err)
	}
	resp, err = client.Get("http://" + address + "/command")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("re-enabled LAN page returned %d", resp.StatusCode)
	}
}

func TestImmediateLANDisableReleasesSocketBeforeReturn(t *testing.T) {
	l := newLANTestControl(t)
	for range 30 {
		if err := l.enable(); err != nil {
			t.Fatal(err)
		}
		address := net.JoinHostPort("127.0.0.1", fmt.Sprint(l.port))
		if err := l.disable(); err != nil {
			t.Fatal(err)
		}
		probe, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("disable returned before listener was released: %v", err)
		}
		probe.Close()
	}
}
