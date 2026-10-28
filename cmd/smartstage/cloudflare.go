package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/cloudflared"
	"smartstage/internal/httpapi"
	"smartstage/internal/web"
)

// cloudflareControl creates a remote-only origin on an ephemeral loopback port.
// Admin and the native application never listen through this tunnel. Each new
// helper receives a fresh pairing authority, invalidating earlier phone links.
type cloudflareControl struct {
	configDir string
	app       *app.Service
	run       func(context.Context, cloudflared.Options) error
}

func (c *cloudflareControl) serve(parent context.Context, connected func(string, string), status func(string, string)) error {
	if err := parent.Err(); err != nil {
		return err
	}
	// Close the origin before canceling the helper, so Cloudflare can forward
	// the stream closures instead of retaining an abandoned edge connection.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return errors.New("could not start the Cloudflare remote listener")
	}
	defer listener.Close()
	var mu sync.RWMutex
	var api *httpapi.API
	closed := false
	authentication := auth.NewPublicCommand()
	server := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		current := api
		retired := closed
		mu.RUnlock()
		if current == nil || retired || ctx.Err() != nil {
			http.Error(w, "Remote connection is not ready", http.StatusServiceUnavailable)
			return
		}
		current.ServeHTTP(w, r)
	}))
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := server.Serve(httpapi.BoundConnections(listener)); err != nil && err != http.ErrServerClosed {
			cancel()
		}
	}()
	var closeOnce sync.Once
	closeOrigin := func() {
		closeOnce.Do(func() {
			mu.Lock()
			closed = true
			hadAPI := api != nil
			if api != nil {
				_ = api.Close()
			}
			mu.Unlock()
			// A short bounded drain lets the proxy carry EOF before its process is
			// stopped; the API already rejects all requests and closes its sockets.
			if hadAPI {
				time.Sleep(200 * time.Millisecond)
			}
			_ = server.Close()
		})
	}
	// Closing the HTTP server alone does not close hijacked WebSockets. Close
	// the API explicitly both on cancellation and on normal helper termination.
	originDone := make(chan struct{})
	go func() {
		defer close(originDone)
		select {
		case <-parent.Done():
		case <-ctx.Done():
		}
		closeOrigin()
		cancel()
	}()
	defer func() {
		closeOrigin()
		cancel()
		<-originDone
		<-serveDone
	}()
	run := c.run
	if run == nil {
		run = cloudflared.Run
	}
	return run(ctx, cloudflared.Options{
		ConfigDir: c.configDir, OriginURL: "http://" + listener.Addr().String(),
		OnStatus: func(s string) {
			if ctx.Err() != nil {
				return
			}
			switch s {
			case "downloading":
				status("downloading", "Downloading the Cloudflare helper…")
			case "connecting":
				status("connecting", "Connecting to Cloudflare… Local network remote access is disabled.")
			}
		},
		OnURL: func(publicURL string) {
			mu.Lock()
			if ctx.Err() != nil || closed || api != nil {
				mu.Unlock()
				return
			}
			handler, err := httpapi.NewCloudflareCommand(c.app, authentication, web.Handler(), publicURL)
			if err != nil {
				mu.Unlock()
				cancel()
				return
			}
			api = handler
			mu.Unlock()
			connected(publicURL+"/command#token="+authentication.CommandToken(), authentication.CommandToken())
		},
	})
}
