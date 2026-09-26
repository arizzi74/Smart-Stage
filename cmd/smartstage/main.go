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
	"strconv"
	"strings"
	"syscall"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/browseropen"
	"smartstage/internal/files"
	"smartstage/internal/httpapi"
	"smartstage/internal/identity"
	"smartstage/internal/lan"
	"smartstage/internal/locale"
	"smartstage/internal/platform"
	"smartstage/internal/playback"
	"smartstage/internal/playlistfile"
	"smartstage/internal/remote"
	"smartstage/internal/store"
	"smartstage/internal/update"
	"smartstage/internal/web"
)

var version = "dev"
var commit = "unknown"

type rootsFlag []string

func (r *rootsFlag) String() string     { return strings.Join(*r, ", ") }
func (r *rootsFlag) Set(v string) error { *r = append(*r, v); return nil }

var allowStartupDialog = true

func main() {
	if len(os.Args) >= 5 && os.Args[1] == "--smartstage-restart" && os.Args[4] == "--" {
		pid, err := strconv.Atoi(os.Args[2])
		if err == nil {
			err = update.RunRestartHelper(pid, os.Args[3], os.Args[5:])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Smart Stage restart:", err)
			os.Exit(1)
		}
		return
	}
	// Helper mode performs replacement only after the original process exits.
	if len(os.Args) == 3 && os.Args[1] == "--smartstage-apply-update" {
		if err := update.RunHelper(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "Smart Stage update:", err)
			os.Exit(1)
		}
		return
	}
	port := flag.Int("port", 8788, "remote-control HTTP port (0 selects an available port)")
	adminPort := flag.Int("admin-port", 8787, "localhost Admin HTTP port (0 selects an available port)")
	bind := flag.String("bind", "0.0.0.0", "remote-control listen IP; Admin always binds 127.0.0.1")
	noBrowser := flag.Bool("no-browser", false, "do not automatically open the Admin window or system browser")
	noAutoUpdate := flag.Bool("no-auto-update", false, "check for updates without automatically installing them at launch")
	advertise := flag.String("advertise-ip", "", "local address to prefer in Admin's remote-control links")
	configDir := flag.String("config-dir", "", "configuration directory (default: per-user SmartStage directory)")
	logLevel := flag.String("log-level", "info", "debug, info, warn or error")
	showVersion := flag.Bool("version", false, "print build and platform information")
	updateReceipt := flag.String("update-receipt", "", "internal update startup receipt")
	var roots rootsFlag
	flag.Var(&roots, "media-root", "allowed host media directory; repeat for several roots")
	flag.Parse()
	// Headless invocations and supervised update candidates must report errors
	// and exit promptly; a modal dialog would hold up diagnostics or rollback.
	allowStartupDialog = !*noBrowser && *updateReceipt == ""
	if *showVersion {
		prepareVersionOutput()
		fmt.Printf("Smart Stage %s (%s), %s, %s/%s\n", version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}
	var level slog.Level
	if *updateReceipt != "" {
		if err := update.RegisterStartup(*updateReceipt, version); err != nil {
			// Exit promptly so the helper can observe failed startup and roll
			// back. A native error dialog here could keep an unregistered
			// candidate alive without a PID the helper can safely terminate.
			fmt.Fprintln(os.Stderr, "Smart Stage update startup:", err)
			os.Exit(1)
		}
	}
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		exitError(errors.New("invalid --log-level; use debug, info, warn or error"))
	}
	if *port < 0 || *port > 65535 || *adminPort < 0 || *adminPort > 65535 {
		exitError(errors.New("--port and --admin-port must be between 0 and 65535"))
	}
	if *port != 0 && *port == *adminPort {
		exitError(errors.New("Admin and remote control require different ports"))
	}
	if net.ParseIP(*bind) == nil {
		exitError(errors.New("--bind must be a local IP address, not a hostname"))
	}
	*bind = net.ParseIP(*bind).String()
	if *advertise != "" {
		ip := net.ParseIP(*advertise)
		if ip == nil {
			exitError(errors.New("--advertise-ip must be an active local non-loopback address"))
		}
		*advertise = ip.String()
	}
	if *configDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			exitError(err)
		}
		*configDir = filepath.Join(dir, "SmartStage")
	}
	absoluteConfigDir, err := filepath.Abs(*configDir)
	if err != nil {
		exitError(err)
	}
	*configDir = absoluteConfigDir
	if canonical, resolveErr := filepath.EvalSymlinks(*configDir); resolveErr == nil {
		*configDir = canonical
	}
	reopenExisting := func() bool {
		return runtime.GOOS == "windows" && !*noBrowser && *updateReceipt == "" && platform.DesktopReopen(*configDir)
	}
	if reopenExisting() {
		return
	}
	closeDesktopLog, err := prepareDesktopLog(*configDir)
	if err != nil {
		exitError(err)
	}
	defer closeDesktopLog()
	platform.DesktopConfigure(*configDir)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	language, languageErr := locale.New(*configDir, platform.SystemLanguage(), platform.DesktopLanguage)
	if languageErr != nil {
		slog.Warn("using system language because preferences could not be read", "error", languageErr)
	}
	for i, root := range roots {
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			exitError(err)
		}
		roots[i] = absoluteRoot
	}
	err = platform.Run(func(backend playback.Backend) (runErr error) {
		var pendingUpdate updateHandoff
		// Register first so this runs after HTTP, updater, service and storage
		// cleanup. AppKit can terminate directly during the following native
		// cleanup, so the helper must already own the handoff at that point.
		// It waits for this process to exit before replacing any installed file.
		defer func() { runErr = finishUpdateHandoff(pendingUpdate, runErr) }()
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
		settings, err := remote.Load(*configDir)
		if err != nil {
			return err
		}
		*adminPort = adminListener.Addr().(*net.TCPAddr).Port
		service := app.New(backend, fileBrowser, storage, config)
		defer service.Close()
		authentication := auth.New()
		adminAPI := httpapi.NewAdmin(service, authentication, web.Handler(), []string{"127.0.0.1", "localhost"}, *adminPort)
		adminAPI.SetLanguage(language)
		adminServer := newServer(adminAPI)
		serveError := make(chan error, 2)
		local := &lanControl{bind: *bind, advertise: *advertise, port: *port, addresses: addresses, auth: authentication, app: service, admin: adminAPI, errors: serveError}
		if settings.Mode == "lan" {
			if err := local.enable(); err != nil {
				return err
			}
			*port = local.port
		}
		defer local.disable()
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		adminAPI.SetQuit(cancel)
		adminAPI.SetChooseFiles(platform.DesktopChooseFiles, platform.DesktopCanChooseFiles)
		playlistFiles := playlistfile.New(service, platform.DesktopChoosePlaylist, platform.DesktopCanChoosePlaylists)
		defer playlistFiles.Close()
		adminAPI.SetPlaylistFiles(playlistFiles)
		updateReady := make(chan *update.Prepared, 1)
		executable, _ := os.Executable()
		// Normalize parsed options, retaining assigned ports and absolute media
		// roots even when LaunchServices changes the working directory.
		restartArgs := []string{"--port", fmt.Sprint(*port), "--admin-port", fmt.Sprint(*adminPort),
			"--bind", *bind, "--config-dir", *configDir, "--log-level", *logLevel}
		if *noBrowser {
			restartArgs = append(restartArgs, "--no-browser")
		}
		if *noAutoUpdate {
			restartArgs = append(restartArgs, "--no-auto-update")
		}
		if *advertise != "" {
			restartArgs = append(restartArgs, "--advertise-ip", *advertise)
		}
		for _, root := range roots {
			restartArgs = append(restartArgs, "--media-root", root)
		}
		restartReady := make(chan *update.Restart, 1)
		remoteManager, err := remote.New(ctx, settings, remote.Options{ConfigDir: *configDir,
			DisableLAN: local.disable, Changed: local.changed,
			Handler: func(publicURL, prefix string, authentication *auth.Manager) http.Handler {
				handler, err := httpapi.NewGatewayCommand(service, authentication, web.Handler(), publicURL, prefix)
				if err != nil {
					return nil
				}
				return handler
			},
			ReserveRestart: func() (func(), func(), error) {
				release, err := service.ReserveUpdate()
				if err != nil {
					return nil, nil, err
				}
				prepared, err := update.PrepareRestart(restartArgs, *configDir)
				if err != nil {
					release()
					return nil, nil, err
				}
				return func() { restartReady <- prepared }, release, nil
			}})
		if err != nil {
			return err
		}
		defer remoteManager.Close()
		adminAPI.SetGateway(remoteManager)
		updater := update.New(update.Options{CurrentVersion: version, Executable: executable,
			Args: restartArgs, ConfigDir: *configDir, AutoInstall: !*noAutoUpdate && *updateReceipt == "", Reserve: service.ReserveUpdate,
			Ready: func(prepared *update.Prepared) { updateReady <- prepared }})
		defer func() {
			updater.Close()
			// A user Quit may win the select at the same time staging finishes.
			// Do not leave an unclaimed prepared update behind in that case.
			select {
			case abandoned := <-updateReady:
				_ = abandoned.Abort()
			default:
			}
		}()
		adminAPI.SetUpdater(updater)
		_ = service.Validate()
		updater.Start(ctx)
		go func() { serveError <- adminServer.Serve(httpapi.BoundConnections(adminListener)) }()
		defer adminServer.Close()
		fmt.Printf("Smart Stage %s\n", version)
		adminURL := lan.URL("127.0.0.1", *adminPort, "/admin")
		platform.DesktopAdmin(adminURL)
		if *updateReceipt != "" {
			if err := update.ConfirmStartup(*updateReceipt, version); err != nil {
				return err
			}
		}
		fmt.Printf("Admin: %s\nRemote mode: %s\n", adminURL, settings.Mode)
		if settings.Mode == "lan" {
			fmt.Printf("Remote listener: %s\n", lan.URL(*bind, *port, "/command"))
		}
		exitHint := "Ctrl+C exits"
		if runtime.GOOS == "windows" {
			exitHint = "Use Quit Smart Stage in Admin or the tray menu to exit"
		} else if runtime.GOOS == "darwin" && os.Getenv("SMARTSTAGE_APP_LAUNCH") == "1" {
			exitHint = "Right-click the Smart Stage Dock icon and choose Quit"
		}
		fmt.Printf("Open Admin to scan or copy the remote-control link. %s; STOP returns to the stage background.\n", exitHint)
		browserCtx, cancelBrowser := context.WithCancel(ctx)
		defer cancelBrowser()
		adminBrowser := newAdminBrowser(browserCtx, adminAPI.HasAdminPresence,
			func() bool { return service.Snapshot(true).UpdatePending },
			func() error { return browseropen.Open(adminURL) },
			func() { platform.DesktopActivateBrowser() })
		requestAdmin := func(explicit bool) {
			if browserCtx.Err() != nil {
				return
			}
			if platform.DesktopHasAdminWindow() {
				// Desktop apps own their Admin window. They can display startup
				// update progress immediately and always restore that same window,
				// regardless of any separately opened browser's presence.
				if !platform.DesktopShowAdmin() {
					slog.Warn("Could not show the Admin window; open the printed Admin URL")
				} else {
					slog.Info("Requested the dedicated Admin window")
				}
				return
			}
			adminBrowser.Request(explicit)
		}
		if !*noBrowser {
			requestAdmin(false)
		}
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-platform.DesktopEmergencyRequests():
				if _, err := service.EmergencyStop(app.StopRequest{RequestID: identity.New()}); err != nil {
					slog.Error("Native Admin emergency stop failed", "error", err)
				}
			case <-platform.DesktopQuitRequests():
				cancel()
			case <-platform.DesktopAdminRequests():
				requestAdmin(true)
			case result := <-platform.DesktopPlaylistResults():
				playlistFiles.Complete(result)
			case request := <-platform.DesktopFiles():
				// Native desktop actions carry original host paths. Keep the
				// save in this loop so shutdown cannot overtake an accepted append.
				_, err := service.AppendHostFiles(request.Paths)
				message := ""
				if err != nil {
					message = err.Error()
					var problem *app.Error
					if errors.As(err, &problem) && problem.Code == "updating" {
						message = "Smart Stage is checking or installing an update. Add these files again after the update finishes. Your original files have not been changed."
					}
					slog.Warn("Could not add files from the desktop", "error", err)
				}
				platform.DesktopFileResult(request.ID, message)
			case restart := <-restartReady:
				pendingUpdate = restart
				cancelBrowser()
				remoteManager.Close()
				_ = local.disable()
				service.Close()
				shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
				_ = adminServer.Shutdown(shutdownCtx)
				done()
				return nil
			case prepared := <-updateReady:
				if ctx.Err() != nil {
					_ = prepared.Abort()
					return nil
				}
				// This is the commit boundary. A later Quit still allows the
				// verified update to finish after all cleanup and process exit.
				pendingUpdate = prepared
				cancelBrowser()
				service.Close()
				shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
				_ = adminServer.Shutdown(shutdownCtx)
				remoteManager.Close()
				_ = local.disable()
				done()
				return nil
			case <-ctx.Done():
				service.Close()
				shutdownCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
				defer done()
				_ = adminServer.Shutdown(shutdownCtx)
				remoteManager.Close()
				_ = local.disable()
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
					local.addressesChanged(addresses)
					fmt.Println("LAN addresses changed; remote-control links refreshed in Admin.")
				}
			}
		}
	})
	if err != nil {
		// A simultaneous second launch may lose the existing configuration
		// lock after its first reopen check. Restore the established window.
		if reopenExisting() {
			return
		}
		exitError(err)
	}
}

type updateHandoff interface {
	Launch() error
	Abort() error
}

func finishUpdateHandoff(prepared updateHandoff, shutdownErr error) error {
	if prepared == nil {
		return shutdownErr
	}
	if shutdownErr != nil {
		_ = prepared.Abort()
		return shutdownErr
	}
	if err := prepared.Launch(); err != nil {
		_ = prepared.Abort()
		return fmt.Errorf("could not start the updater; the installed app was retained: %w", err)
	}
	return nil
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
func exitError(err error) {
	fmt.Fprintln(os.Stderr, "Smart Stage:", err)
	if allowStartupDialog {
		platform.DesktopError(err.Error())
	}
	os.Exit(1)
}
