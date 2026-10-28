package cloudflared

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const guardianArgument = "--smartstage-cloudflared-guard"
const maxGuardianMessage = 16 << 10
const runtimeMarker = ".smartstage-runtime"
const runtimeMarkerValue = "SmartStage cloudflared runtime " + Version + "\n"

type guardianSettings struct {
	Binary               string `json:"binary"`
	RunDir               string `json:"runDir"`
	Origin               string `json:"origin"`
	StartupTimeoutMillis int64  `json:"startupTimeoutMillis"`
}

type guardianEvent struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Code string `json:"code,omitempty"`
}

// Guardian handles the private helper mode before the application's flag or UI
// initialization. It receives one bounded launch message through stdin, then
// keeps cloudflared alive only while that same pipe remains open. An abruptly
// terminated parent closes the pipe on every supported OS, allowing this
// separate process to stop and reap cloudflared and remove its runtime files.
// Unrelated arguments return handled=false without touching stdin or the UI.
func Guardian(args []string) (handled bool, err error) {
	if len(args) == 0 || args[0] != guardianArgument {
		return false, nil
	}
	if len(args) != 1 {
		return true, ErrStartup
	}
	ignoreGuardianBrokenPipe()
	return true, runGuardian(os.Stdin, os.Stdout)
}

func validGuardianSettings(settings guardianSettings) bool {
	binaryName := "cloudflared"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	if !validOrigin(settings.Origin) || !filepath.IsAbs(settings.RunDir) || filepath.Clean(settings.RunDir) != settings.RunDir || filepath.Base(filepath.Dir(settings.RunDir)) != "cloudflared" || !strings.HasPrefix(filepath.Base(settings.RunDir), "run-") || settings.Binary != filepath.Join(settings.RunDir, binaryName) || settings.StartupTimeoutMillis < 1 || settings.StartupTimeoutMillis > 120000 {
		return false
	}
	directory, err := os.Lstat(settings.RunDir)
	if err != nil || !directory.IsDir() || directory.Mode()&os.ModeSymlink != 0 {
		return false
	}
	markerPath := filepath.Join(settings.RunDir, runtimeMarker)
	marker, err := os.Lstat(markerPath)
	if err != nil || !marker.Mode().IsRegular() || marker.Size() != int64(len(runtimeMarkerValue)) {
		return false
	}
	markerFile, err := os.Open(markerPath)
	if err != nil {
		return false
	}
	markerData, markerErr := io.ReadAll(io.LimitReader(markerFile, int64(len(runtimeMarkerValue)+1)))
	_ = markerFile.Close()
	if markerErr != nil || string(markerData) != runtimeMarkerValue {
		return false
	}
	binary, err := os.Lstat(settings.Binary)
	return err == nil && binary.Mode().IsRegular() && binary.Size() > 0 && binary.Size() <= maxBinarySize
}

func runGuardian(stdin io.Reader, stdout io.Writer) error {
	reader := bufio.NewReaderSize(stdin, maxGuardianMessage)
	header, err := reader.ReadSlice('\n')
	if err != nil {
		return ErrStartup
	}
	var settings guardianSettings
	decoder := json.NewDecoder(bytes.NewReader(header))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&settings) != nil || decoder.Decode(new(any)) != io.EOF || !validGuardianSettings(settings) {
		return ErrStartup
	}
	defer os.RemoveAll(settings.RunDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Any EOF or extra input ends this lifetime. The parent sends only the
	// initial settings and holds its pipe open until cancellation or death.
	go func() {
		var extra [1]byte
		_, _ = reader.Read(extra[:])
		cancel()
	}()
	encoder := json.NewEncoder(stdout)
	result := runProcess(ctx, Options{
		OriginURL: settings.Origin,
		OnURL: func(publicURL string) {
			if encoder.Encode(guardianEvent{Type: "ready", URL: publicURL}) != nil {
				cancel()
			}
		},
	}, settings.Binary, settings.RunDir, time.Duration(settings.StartupTimeoutMillis)*time.Millisecond)
	// runProcess has already reaped the helper before reporting completion.
	// Raw subprocess logs and local paths never cross this protocol.
	_ = encoder.Encode(guardianEvent{Type: "result", Code: guardianResultCode(result)})
	if errors.Is(result, context.Canceled) {
		return nil
	}
	return result
}

func guardianResultCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrStartupTimeout):
		return "timeout"
	case errors.Is(err, ErrStartup):
		return "startup"
	case errors.Is(err, ErrCache):
		return "cache"
	default:
		return "stopped"
	}
}

func guardianResultError(code string) error {
	switch code {
	case "timeout":
		return ErrStartupTimeout
	case "startup":
		return ErrStartup
	case "cache":
		return ErrCache
	default:
		return ErrStopped
	}
}

// launchGuardian deliberately does not attach the caller's cancellation to
// exec.CommandContext. Closing stdin asks the guardian to reap its child before
// exiting; killing the guardian first would strand the actual tunnel helper.
func launchGuardian(ctx context.Context, opts Options, binary, runDir string, startupTimeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return ErrStartup
	}
	settings := guardianSettings{Binary: binary, RunDir: runDir, Origin: opts.OriginURL, StartupTimeoutMillis: startupTimeout.Milliseconds()}
	if !validGuardianSettings(settings) {
		return ErrCache
	}
	header, err := json.Marshal(settings)
	if err != nil || len(header)+1 >= maxGuardianMessage {
		return ErrCache
	}
	cmd := exec.CommandContext(context.Background(), self, guardianArgument)
	// Windows locks a process's current directory against deletion. Only the
	// actual helper runs inside runDir; its guardian must outlive and remove it.
	cmd.Dir = filepath.Dir(runDir)
	cmd.Env = isolatedEnvironment(os.Environ(), runDir)
	cmd.WaitDelay = 2 * time.Second
	cleanupProcess := configureProcess(cmd)
	defer cleanupProcess()
	output := &guardianOutput{ready: make(chan string, 1)}
	cmd.Stdout, cmd.Stderr = output, io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ErrStartup
	}
	defer stdin.Close()
	stopCancellation := context.AfterFunc(ctx, func() { _ = stdin.Close() })
	defer stopCancellation()
	if opts.OnStatus != nil {
		opts.OnStatus("connecting")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrStartup
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	if _, err := stdin.Write(append(header, '\n')); err != nil {
		_ = stdin.Close()
		<-wait
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrStartup
	}
	for {
		select {
		case <-ctx.Done():
			_ = stdin.Close()
			<-wait
			return ctx.Err()
		case publicURL := <-output.ready:
			if ctx.Err() == nil && opts.OnURL != nil {
				opts.OnURL(publicURL)
			}
		case <-wait:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return guardianResultError(output.result())
		}
	}
}

type guardianOutput struct {
	mu      sync.Mutex
	buffer  []byte
	drop    bool
	code    string
	ready   chan string
	urlSent bool
}

func (w *guardianOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		part := p
		if end >= 0 {
			part = p[:end]
		}
		if !w.drop {
			if len(w.buffer)+len(part) >= maxGuardianMessage {
				w.buffer = w.buffer[:0]
				w.drop = true
			} else {
				w.buffer = append(w.buffer, part...)
			}
		}
		if end < 0 {
			break
		}
		if !w.drop {
			var event guardianEvent
			if json.Unmarshal(w.buffer, &event) == nil {
				switch event.Type {
				case "ready":
					if !w.urlSent && exactQuickURL(event.URL) != "" {
						w.ready <- event.URL
						w.urlSent = true
					}
				case "result":
					w.code = event.Code
				}
			}
		}
		w.buffer = w.buffer[:0]
		w.drop = false
		p = p[end+1:]
	}
	return n, nil
}

func (w *guardianOutput) result() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.code
}
