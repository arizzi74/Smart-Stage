package cloudflared

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnsupported    = errors.New("Cloudflare Quick Tunnel is unavailable on this platform")
	ErrOrigin         = errors.New("Cloudflare tunnel requires a loopback HTTP origin")
	ErrCache          = errors.New("cannot prepare Cloudflare tunnel helper cache")
	ErrDownload       = errors.New("cannot download verified Cloudflare tunnel helper")
	ErrStartup        = errors.New("cannot start Cloudflare tunnel helper")
	ErrStartupTimeout = errors.New("Cloudflare tunnel connection timed out")
	ErrStopped        = errors.New("Cloudflare tunnel helper stopped")
)

// Options configures one helper lifetime. ConfigDir is the application's user
// configuration/cache directory, never the user's existing cloudflared directory.
// Callbacks run serially within Run and must return promptly. OnURL is called
// once, only after both URL allocation and a registered edge connection.
type Options struct {
	ConfigDir string
	OriginURL string
	OnURL     func(string)
	OnStatus  func(string)
}

// Run downloads or verifies the native pinned helper, starts an account-free
// tunnel, and blocks until it stops or ctx is canceled. Cancellation waits for
// process reaping and removes its private runtime directory. It never retries;
// the caller owns retry policy and any public URL/token rotation.
func Run(ctx context.Context, opts Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, ok := nativeAssets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return ErrUnsupported
	}
	if !validOrigin(opts.OriginURL) {
		return ErrOrigin
	}
	binary, runDir, err := prepareBinary(ctx, opts.ConfigDir, a, downloadClient(), opts.OnStatus)
	if err != nil {
		return err
	}
	defer os.RemoveAll(runDir)
	return launchGuardian(ctx, opts, binary, runDir, 90*time.Second)
}

func validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	// A literal address avoids DNS/proxy ambiguity and rebinding. The caller
	// must supply its dedicated command listener, never its admin listener.
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	return ip != nil && ip.IsLoopback() && err == nil && port > 0 && port <= 65535
}

func helperArgs(origin, config string) []string {
	return []string{"tunnel", "--no-autoupdate", "--config", config,
		"--url", origin, "--metrics", "127.0.0.1:0", "--protocol", "auto",
		// Address rotation gates protocol fallback. Bound both counters so
		// blocked QUIC gives HTTP2 time to connect within the startup deadline.
		"--retries", "2", "--max-edge-addr-retries", "2",
		"--grace-period", "1s", "--loglevel", "info", "--output", "json"}
}

func isolatedEnvironment(env []string, home string) []string {
	result := make([]string, 0, len(env))
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(key)
		// Allow only OS, language, trust-store and network-proxy settings. This
		// prevents current and future cloudflared account/token/ingress flags
		// from being enabled through the parent application's environment.
		switch key {
		case "PATH", "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "TEMP", "TMP", "TMPDIR", "LANG", "LANGUAGE", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			result = append(result, item)
		default:
			if strings.HasPrefix(key, "LC_") {
				result = append(result, item)
			}
		}
	}
	return append(result, "HOME="+home, "USERPROFILE="+home, "APPDATA="+home,
		"LOCALAPPDATA="+home, "XDG_CONFIG_HOME="+home, "NO_AUTOUPDATE=true")
}

func runProcess(ctx context.Context, opts Options, binary, runDir string, startupTimeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	config := filepath.Join(runDir, "config.yml")
	if err := os.WriteFile(config, []byte("{}\n"), 0600); err != nil {
		return ErrCache
	}
	procCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(procCtx, binary, helperArgs(opts.OriginURL, config)...)
	cmd.Dir = runDir
	cmd.Env = isolatedEnvironment(os.Environ(), runDir)
	cmd.WaitDelay = 2 * time.Second
	cleanupProcess := configureProcess(cmd)
	defer cleanupProcess()
	events := make(chan processEvent, 4)
	output := &eventWriter{events: events}
	cmd.Stdout, cmd.Stderr = output, output
	if opts.OnStatus != nil {
		opts.OnStatus("connecting")
	}
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrStartup
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	// Every return after Start cancels and waits, including startup timeout.
	defer func() {
		cancel()
		<-wait
	}()
	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	var publicURL string
	registered, announced := false, false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-wait:
			// Preserve one wait result for the unconditional reaping defer.
			wait <- err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrStopped
		case <-timer.C:
			return ErrStartupTimeout
		case event := <-events:
			if event.url != "" {
				publicURL = event.url
			}
			registered = registered || event.registered
			if !announced && publicURL != "" && registered {
				announced = true
				timer.Stop()
				if opts.OnURL != nil {
					opts.OnURL(publicURL)
				}
			}
		}
	}
}

type processEvent struct {
	url        string
	registered bool
}

var (
	logURL      = regexp.MustCompile(`https://[^\s"<>|]+`)
	quickDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.trycloudflare\.com$`)
)

func exactQuickURL(candidate string) string {
	u, err := url.Parse(candidate)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host != u.Hostname() || !quickDomain.MatchString(u.Host) {
		return ""
	}
	return u.String()
}

// eventWriter consumes both log streams without retaining or exposing raw logs,
// origin URLs, credentials or unrelated user paths. Oversized lines are dropped.
type eventWriter struct {
	mu        sync.Mutex
	buffer    []byte
	dropping  bool
	urlSent   bool
	readySent bool
	events    chan<- processEvent
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		part := p
		if end >= 0 {
			part = p[:end]
		}
		if !w.dropping {
			if len(w.buffer)+len(part) > 64<<10 {
				w.buffer = w.buffer[:0]
				w.dropping = true
			} else {
				w.buffer = append(w.buffer, part...)
			}
		}
		if end < 0 {
			break
		}
		if !w.dropping {
			w.line(w.buffer)
		}
		w.buffer = w.buffer[:0]
		w.dropping = false
		p = p[end+1:]
	}
	return n, nil
}

func (w *eventWriter) line(line []byte) {
	if !w.urlSent {
		for _, candidate := range logURL.FindAllString(string(line), -1) {
			if public := exactQuickURL(candidate); public != "" {
				w.events <- processEvent{url: public}
				w.urlSent = true
				break
			}
		}
	}
	if !w.readySent && bytes.Contains(line, []byte("Registered tunnel connection")) {
		w.events <- processEvent{registered: true}
		w.readySent = true
	}
}
