//go:build linux

// Smart Stage Gateway is an independently deployable, statically linked Linux
// service. Desktop playback and the Admin UI are never hosted by this daemon.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"smartstage/internal/gateway"
	"smartstage/internal/gatewayinstall"
)

var version = "dev"
var commit = "unknown"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "smartstage-gateway:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-version":
			fmt.Printf("%s (%s)\n", version, commit)
			return nil
		case "licenses", "--licenses":
			fmt.Print(gateway.Licenses)
			return nil
		case "install":
			return gatewayinstall.Run(args[1:])
		case "init", "serve":
			command = args[0]
			args = args[1:]
		case "help", "--help", "-h":
			fmt.Println("Smart Stage Gateway " + version + "\nUsage: smartstage-gateway [serve] [--config PATH]\n       smartstage-gateway install\n       smartstage-gateway init --public-url https://example.com/smartstage [--config PATH]\n       smartstage-gateway version\n       smartstage-gateway licenses")
			return nil
		}
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := flags.String("config", "/etc/smartstage-gateway/config.json", "private gateway configuration file")
	var publicURL, listen *string
	if command == "init" {
		publicURL = flags.String("public-url", "", "HTTPS gateway URL ending in /smartstage")
		listen = flags.String("listen", "127.0.0.1:8790", "loopback HTTP listener")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected command argument")
	}
	if command == "init" {
		return initialize(*configPath, *publicURL, *listen)
	}
	cfg, err := gateway.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	relay, err := gateway.NewServer(cfg)
	if err != nil {
		return err
	}
	defer relay.Close()
	server := &http.Server{Addr: cfg.Listen, Handler: relay, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	log.Printf("Smart Stage Gateway %s listening on %s", version, cfg.Listen)
	select {
	case err := <-stopped:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("gateway HTTP server: %w", err)
	case <-ctx.Done():
		relay.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
			return err
		}
		return nil
	}
}

func initialize(path, rawURL, listen string) error {
	u, err := gateway.ValidateURL(rawURL)
	if err != nil {
		return err
	}
	token, err := gateway.NewToken()
	if err != nil {
		return errors.New("could not generate gateway registration token")
	}
	cfg := gateway.Config{Listen: listen, PublicURL: u.String(), Token: token}
	validation, err := gateway.NewServer(cfg)
	if err != nil {
		return err
	}
	validation.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("could not create configuration (existing configuration is never overwritten)")
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	fmt.Println("Created gateway configuration:", path)
	fmt.Println("Gateway URL:", cfg.PublicURL)
	fmt.Println("Registration token:", cfg.Token)
	return nil
}
