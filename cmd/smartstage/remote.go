package main

import (
	"fmt"
	"net"
	"net/http"
	"sync"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/httpapi"
	"smartstage/internal/lan"
	"smartstage/internal/web"
)

// lanControl owns the optional inbound socket. Disable closes existing browser
// streams too; gateway mode must never leave a usable direct LAN connection.
type lanControl struct {
	mu              sync.Mutex
	bind, advertise string
	port            int
	addresses       []lan.Address
	server          *http.Server
	listener        net.Listener
	api             *httpapi.API
	auth            *auth.Manager
	app             *app.Service
	admin           *httpapi.API
	errors          chan<- error
	mode            string
}

func (l *lanControl) enable() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	listener, err := net.Listen("tcp", net.JoinHostPort(l.bind, fmt.Sprint(l.port)))
	if err != nil {
		return fmt.Errorf("cannot start LAN remote control: %w", err)
	}
	l.port = listener.Addr().(*net.TCPAddr).Port
	l.api = httpapi.NewCommand(l.app, l.auth, web.Handler(), lan.Hosts(l.addresses, l.bind), l.port)
	l.server = newServer(l.api)
	l.listener = listener
	server := l.server
	go func() {
		if err := server.Serve(httpapi.BoundConnections(listener)); err != nil && err != http.ErrServerClosed {
			l.errors <- err
		}
	}()
	return nil
}
func (l *lanControl) disable() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.server == nil {
		return nil
	}
	err := l.server.Close()
	_ = l.listener.Close()
	l.listener = nil
	l.server = nil
	l.api = nil
	return err
}
func (l *lanControl) changed(mode, publicURL, token string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mode = mode
	if mode == "gateway" {
		var links []httpapi.RemoteLink
		if publicURL != "" {
			links = []httpapi.RemoteLink{{Label: "Public gateway · HTTPS", URL: publicURL}}
		}
		l.admin.SetRemoteControl(links, token, "gateway")
	} else if l.server != nil {
		l.admin.SetRemoteLinks(remoteLinks(l.addresses, l.bind, l.advertise, l.port, l.auth.CommandToken()))
	} else {
		l.admin.SetRemoteControl(nil, "", "lan")
	}
}
func (l *lanControl) addressesChanged(addresses []lan.Address) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addresses = append([]lan.Address(nil), addresses...)
	if l.api != nil {
		l.api.SetHosts(lan.Hosts(addresses, l.bind))
	}
	if l.mode == "lan" && l.server != nil {
		l.admin.SetRemoteLinks(remoteLinks(addresses, l.bind, l.advertise, l.port, l.auth.CommandToken()))
	}
}
