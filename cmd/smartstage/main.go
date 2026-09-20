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
	"sort"
	"strings"
	"syscall"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/browseropen"
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
	port := flag.Int("port", 8788, "remote-control HTTP port (0 selects an available port)")
	adminPort := flag.Int("admin-port", 8787, "localhost Admin HTTP port (0 selects an available port)")
	bind := flag.String("bind", "0.0.0.0", "remote-control listen IP; Admin always binds 127.0.0.1")
	noBrowser := flag.Bool("no-browser", false, "do not automatically open Admin in the system browser")
	advertise := flag.String("advertise-ip", "", "local address to prefer in Admin's remote-control links")
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
	if *port < 0 || *port > 65535 || *adminPort < 0 || *adminPort > 65535 {
		exitError(errors.New("--port and --admin-port must be between 0 and 65535"))
	}
	if *port != 0 && *port == *adminPort {
		exitError(errors.New("Admin and remote control require different ports"))
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
		fileBrowser, err := files.New(roots)
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
		adminListener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(*adminPort)))
		if err != nil {
			return fmt.Errorf("cannot start localhost Admin on port %d (check another Smart Stage instance): %w", *adminPort, err)
		}
		defer adminListener.Close()
		listener, err := net.Listen("tcp", net.JoinHostPort(*bind, fmt.Sprint(*port)))
		if err != nil {
			return fmt.Errorf("cannot listen on %s:%d (check port conflicts): %w", *bind, *port, err)
		}
		defer listener.Close()
		*adminPort = adminListener.Addr().(*net.TCPAddr).Port
		*port = listener.Addr().(*net.TCPAddr).Port
		service := app.New(backend, fileBrowser, storage, config)
		defer service.Close()
		authentication := auth.New()
		adminAPI := httpapi.NewAdmin(service, authentication, web.Handler(), []string{"127.0.0.1", "localhost"}, *adminPort)
		commandAPI := httpapi.NewCommand(service, authentication, web.Handler(), lan.Hosts(addresses, *bind), *port)
		adminAPI.SetRemoteLinks(remoteLinks(addresses, *bind, *advertise, *port, authentication.CommandToken()))
		adminServer, commandServer := newServer(adminAPI), newServer(commandAPI)
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		serveError := make(chan error, 2)
		go func() { serveError <- adminServer.Serve(httpapi.BoundConnections(adminListener)) }()
		go func() { serveError <- commandServer.Serve(httpapi.BoundConnections(listener)) }()
		defer adminServer.Close()
		defer commandServer.Close()
		fmt.Printf("Smart Stage %s\n", version)
		adminURL := lan.URL("127.0.0.1", *adminPort, "/admin")
		fmt.Printf("Admin: %s\nRemote listener: %s\n", adminURL, lan.URL(*bind, *port, "/command"))
		fmt.Println("Open Admin to scan or copy the remote-control link. Ctrl+C exits; STOP retains an enabled black stage.")
		if !*noBrowser {
			go func() {
				if err := browseropen.Open(adminURL); err != nil {
					slog.Warn("Could not open the system browser; open the printed Admin URL", "error", err)
				} else {
					slog.Info("Opened Admin in the system browser")
				}
			}()
		}
		_ = service.Validate()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				service.Close()
				shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
				defer done()
				_ = adminServer.Shutdown(shutdownCtx)
				_ = commandServer.Shutdown(shutdownCtx)
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
					commandAPI.SetHosts(lan.Hosts(addresses, *bind))
					adminAPI.SetRemoteLinks(remoteLinks(addresses, *bind, *advertise, *port, authentication.CommandToken()))
					fmt.Println("LAN addresses changed; remote-control links refreshed in Admin.")
				}
			}
		}
	})
	if err != nil {
		exitError(err)
	}
}
func newServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
}

func remoteLinks(addresses []lan.Address, bind, advertise string, port int, token string) []httpapi.RemoteLink {
	links := []httpapi.RemoteLink{}
	ordered := append([]lan.Address(nil), addresses...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].IP == advertise && ordered[j].IP != advertise })
	for _, a := range ordered {
		if bind == "0.0.0.0" && net.ParseIP(a.IP).To4() == nil {
			continue
		}
		if bind != "0.0.0.0" && bind != "::" && a.IP != bind {
			continue
		}
		label := a.Interface + " · " + a.IP
		if a.IP == advertise {
			label += " · preferred"
		}
		links = append(links, httpapi.RemoteLink{Label: label, URL: lan.URL(a.IP, port, "/command") + "#token=" + token})
	}
	return links
}
func exitError(err error) { fmt.Fprintln(os.Stderr, "Smart Stage:", err); os.Exit(1) }
