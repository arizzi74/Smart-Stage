//go:build !windows

package cloudflared

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func processFixture(t *testing.T, registration bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture")
	script := "#!/bin/sh\nprintf '%s' \"$$\" > pid\nprintf '%s\\n' 'https://fixture-test.trycloudflare.com'\n"
	if registration {
		script += "printf '%s\\n' 'Registered tunnel connection'\n"
	}
	script += "exec sleep 300\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary, dir
}

func assertFixtureReaped(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid pid %q", data)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("helper process %d is still present: %v", pid, err)
	}
}

func TestProcessReadyAndCancellationReaps(t *testing.T) {
	binary, dir := processFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- runProcess(ctx, Options{OriginURL: "http://127.0.0.1:8080", OnURL: func(s string) { ready <- s }}, binary, dir, 5*time.Second)
	}()
	select {
	case public := <-ready:
		if public != "https://fixture-test.trycloudflare.com" {
			t.Fatal(public)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not become ready")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not reap helper")
	}
	assertFixtureReaped(t, dir)
	data, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil || string(data) != "{}\n" {
		t.Fatal("private empty configuration missing")
	}
}

func TestProcessURLWithoutRegistrationTimesOutAndReaps(t *testing.T) {
	binary, dir := processFixture(t, false)
	announced := false
	err := runProcess(context.Background(), Options{OriginURL: "http://127.0.0.1:8080", OnURL: func(string) { announced = true }}, binary, dir, 250*time.Millisecond)
	if !errors.Is(err, ErrStartupTimeout) || announced {
		t.Fatalf("URL announced before registration or wrong timeout: announced=%v err=%v", announced, err)
	}
	assertFixtureReaped(t, dir)
}

func TestProcessStartAndExitErrorsSanitized(t *testing.T) {
	dir := t.TempDir()
	err := runProcess(context.Background(), Options{}, filepath.Join(dir, "secret-token"), dir, time.Second)
	if !errors.Is(err, ErrStartup) || strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "secret-token") {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "exit")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' 'secret-token-error' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := runProcess(context.Background(), Options{}, binary, dir, time.Second); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
}

func TestConcurrentProcessCancellation(t *testing.T) {
	for i := 0; i < 4; i++ {
		// Finish fixture writes before parallel fork/exec calls. Otherwise a
		// concurrent child can briefly inherit another fixture's writable FD
		// before CLOEXEC closes it, producing Linux ETXTBSY rather than testing
		// the helper's cancellation behavior.
		binary, dir := processFixture(t, true)
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- runProcess(ctx, Options{OnURL: func(string) { cancel() }}, binary, dir, time.Second)
			}()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("concurrent helper cancellation stalled")
			}
			assertFixtureReaped(t, dir)
		})
	}
}

func TestCancellationBeforeProcessStartReturnsContext(t *testing.T) {
	binary, dir := processFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runProcess(ctx, Options{OnStatus: func(string) { cancel() }}, binary, dir, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("start cancellation was classified as a startup error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pid")); !os.IsNotExist(err) {
		t.Fatal("canceled helper started unexpectedly")
	}
}

func TestUnresponsiveProcessIsForceKilledAndReaped(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ignore-termination")
	script := "#!/bin/sh\ntrap '' TERM\nprintf '%s' \"$$\" > pid\nprintf '%s\\n' 'https://fixture-test.trycloudflare.com' 'Registered tunnel connection'\nexec sleep 300\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	err := runProcess(ctx, Options{OnURL: func(string) { cancel() }}, binary, dir, 5*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < time.Second || elapsed > 4*time.Second {
		t.Fatalf("unresponsive helper did not respect the forced cleanup bound: %v", elapsed)
	}
	assertFixtureReaped(t, dir)
}
