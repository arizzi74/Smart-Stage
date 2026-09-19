package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/httpapi"
	"smartstage/internal/lan"
	"smartstage/internal/platform"
	"smartstage/internal/playback"
	"smartstage/internal/store"
	"smartstage/internal/web"
)

var version = "dev"
var commit = "unknown"

type rootsFlag []string

func (r *rootsFlag) String() string     { return strings.Join(*r, ", ") }
func (r *rootsFlag) Set(v string) error { *r = append(*r, v); return nil }

func main() {
	port := flag.Int("port", 8787, "HTTP port")
	bind := flag.String("bind", "0.0.0.0", "local listen IP; use a specific local IP to restrict the interface")
	advertise := flag.String("advertise-ip", "", "local address to emphasize in printed URLs")
	configDir := flag.String("config-dir", "", "configuration directory (default: per-user SmartStage directory)")
	logLevel := flag.String("log-level", "info", "debug, info, warn or error")
	showVersion := flag.Bool("version", false, "print build and platform information")
	var roots rootsFlag
	flag.Var(&roots, "media-root", "allowed host media directory; repeat for several roots")
	flag.Parse()
	if *showVersion {
		fmt.Printf("Smart Stage %s (%s), %s, %s/%s\n", version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		exitError(errors.New("invalid --log-level; use debug, info, warn or error"))
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if *port < 1 || *port > 65535 {
		exitError(errors.New("--port must be between 1 and 65535"))
	}
	if net.ParseIP(*bind) == nil {
		exitError(errors.New("--bind must be a local IP address, not a hostname"))
	}
	if *configDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			exitError(err)
		}
		*configDir = filepath.Join(dir, "SmartStage")
	}
	err := platform.Run(func(backend playback.Backend) error {
		storage, config, err := store.Open(*configDir)
		if err != nil {
			return err
		}
		defer storage.Close()
		browser, err := files.New(roots)
		if err != nil {
			return err
		}
		addresses, err := lan.Addresses()
		if err != nil {
			return err
		}
		if *advertise != "" {
			valid := false
			for _, a := range addresses {
				if a.IP == *advertise {
					valid = true
				}
			}
			if !valid {
				return errors.New("--advertise-ip must be an active local non-loopback address")
			}
			if *bind != "0.0.0.0" && *bind != "::" && *bind != *advertise {
				return errors.New("--advertise-ip is not reachable through the selected --bind address")
			}
			if *bind == "0.0.0.0" && net.ParseIP(*advertise).To4() == nil {
				return errors.New("an IPv6 advertised address requires an IPv6 --bind address")
			}
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(*bind, fmt.Sprint(*port)))
		if err != nil {
			return fmt.Errorf("cannot listen on %s:%d (check port conflicts): %w", *bind, *port, err)
		}
		defer listener.Close()
		service := app.New(backend, browser, storage, config)
		defer service.Close()
		authentication := auth.New()
		api := httpapi.New(service, authentication, web.Handler(), lan.Hosts(addresses, *bind), *port)
		server := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		serveError := make(chan error, 1)
		go func() { serveError <- server.Serve(httpapi.BoundConnections(listener)) }()
		fmt.Printf("Smart Stage %s\n", version)
		printAddresses(addresses, *bind, *advertise, *port)
		adminKey, commandKey := authentication.Keys()
		fmt.Printf("\nAdmin pairing key:   %s\nCommand pairing key: %s\n\n", adminKey, commandKey)
		fmt.Println("Trusted LAN only: HTTP is not encrypted. Ctrl+C exits; STOP retains an enabled black stage.")
		_ = service.Validate()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				service.Close()
				shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
				defer done()
				_ = server.Shutdown(shutdownCtx)
				return nil
			case err := <-serveError:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			case <-ticker.C:
				next, err := lan.Addresses()
				if err != nil {
					slog.Warn("Could not refresh LAN addresses", "error", err)
					continue
				}
				if !reflect.DeepEqual(addresses, next) {
					addresses = next
					api.SetHosts(lan.Hosts(addresses, *bind))
					fmt.Println("\nLAN addresses changed:")
					printAddresses(addresses, *bind, *advertise, *port)
				}
			}
		}
	})
	if err != nil {
		exitError(err)
	}
}
func printAddresses(addresses []lan.Address, bind, advertise string, port int) {
	count := 0
	for _, a := range addresses {
		if bind == "0.0.0.0" && net.ParseIP(a.IP).To4() == nil {
			continue
		}
		if bind != "0.0.0.0" && bind != "::" && a.IP != bind {
			continue
		}
		label := a.Interface
		if a.IP == advertise {
			label += " · preferred"
		}
		fmt.Printf("\n%s\nAdmin:   %s\nCommand: %s\n", label, lan.URL(a.IP, port, "/admin"), lan.URL(a.IP, port, "/command"))
		count++
	}
	if count == 0 {
		fmt.Println("No reachable LAN address for this listener. Local configuration is available.")
	}
	if bind == "0.0.0.0" || bind == "127.0.0.1" {
		fmt.Printf("\nLocal: %s\n", lan.URL("127.0.0.1", port, "/admin"))
	}
	if bind == "::" || bind == "::1" {
		fmt.Printf("\nLocal: %s\n", lan.URL("::1", port, "/admin"))
	}
}
func exitError(err error) { fmt.Fprintln(os.Stderr, "Smart Stage:", err); os.Exit(1) }
