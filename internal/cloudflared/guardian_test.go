package cloudflared

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const guardianParentFixture = "--smartstage-cloudflared-test-parent"

// Only this package's test binary dispatches subprocess fixtures. Production
// uses the explicit early Guardian call in cmd/smartstage, without package init.
func init() {
	if handled, err := Guardian(os.Args[1:]); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == guardianParentFixture {
		var settings guardianSettings
		if json.NewDecoder(os.Stdin).Decode(&settings) != nil {
			os.Exit(2)
		}
		err := launchGuardian(context.Background(), Options{OriginURL: settings.Origin, OnURL: func(string) { fmt.Println("ready") }}, settings.Binary, settings.RunDir, time.Duration(settings.StartupTimeoutMillis)*time.Millisecond)
		if err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	if filepath.Base(os.Args[0]) == guardianTestBinaryName() && len(os.Args) > 1 && os.Args[1] == "tunnel" {
		guardianHelperFixture()
		os.Exit(0)
	}
}

func guardianTestBinaryName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

func guardianHelperFixture() {
	data, _ := json.Marshal(map[string]int{"helper": os.Getpid(), "guardian": os.Getppid()})
	_ = os.WriteFile("processes.json", data, 0600)
	behavior, _ := os.ReadFile("behavior")
	switch string(behavior) {
	case "exit":
		return
	case "unready":
	default:
		fmt.Println("https://guardian-fixture.trycloudflare.com")
		fmt.Println("Registered tunnel connection")
	}
	for {
		time.Sleep(time.Hour)
	}
}

func guardianFixture(t *testing.T, behavior string) guardianSettings {
	t.Helper()
	base := filepath.Join(t.TempDir(), "cloudflared")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	runDir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	binary := filepath.Join(runDir, guardianTestBinaryName())
	out, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("could not prepare native subprocess fixture")
	}
	if err := os.WriteFile(filepath.Join(runDir, "behavior"), []byte(behavior), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, runtimeMarker), []byte(runtimeMarkerValue), 0600); err != nil {
		t.Fatal(err)
	}
	return guardianSettings{Binary: binary, RunDir: runDir, Origin: "http://127.0.0.1:1234", StartupTimeoutMillis: 5000}
}

func guardianProcesses(t *testing.T, runDir string) (helper, guardian int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runDir, "processes.json"))
	if err != nil {
		t.Fatal("native helper did not start")
	}
	var processes map[string]int
	if json.Unmarshal(data, &processes) != nil || processes["helper"] <= 0 || processes["guardian"] <= 0 {
		t.Fatal("invalid native subprocess fixture metadata")
	}
	return processes["helper"], processes["guardian"]
}

func waitGuardianCleanup(t *testing.T, settings guardianSettings, helper, guardian int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := os.Stat(settings.RunDir)
		if os.IsNotExist(err) && guardianTestProcessExited(helper) && guardianTestProcessExited(guardian) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("guardian did not stop/reap the helper and remove runtime files")
}

func TestGuardianNormalCancellationReapsAndRemovesRuntime(t *testing.T) {
	settings := guardianFixture(t, "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- launchGuardian(ctx, Options{OriginURL: settings.Origin, OnURL: func(string) { close(ready) }}, settings.Binary, settings.RunDir, 5*time.Second)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("guardian failed before readiness: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("native guardian did not start")
	}
	helper, guardian := guardianProcesses(t, settings.RunDir)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("normal cancellation stalled guardian cleanup")
	}
	waitGuardianCleanup(t, settings, helper, guardian)
}

// This test kills the supervising app process without running its defers. The
// same native Go fixture runs on Linux, macOS and Windows CI, including ARM64.
func TestGuardianAbruptParentDeathReapsAndRemovesRuntime(t *testing.T) {
	settings := guardianFixture(t, "ready")
	header, _ := json.Marshal(settings)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(self, guardianParentFixture)
	parent.Stdin = bytes.NewReader(header)
	stdout, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	parent.Stderr = io.Discard
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Process.Kill(); _ = parent.Wait() })
	ready := make(chan bool, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		ready <- err == nil && line == "ready\n"
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("parent fixture did not start native guardian/helper")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("parent fixture readiness timed out")
	}
	helper, guardian := guardianProcesses(t, settings.RunDir)
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	waitGuardianCleanup(t, settings, helper, guardian)
}

func TestGuardianHelperFailureAndTimeoutCleanRuntime(t *testing.T) {
	for _, behavior := range []string{"exit", "unready"} {
		t.Run(behavior, func(t *testing.T) {
			settings := guardianFixture(t, behavior)
			startupTimeout := 5 * time.Second
			if behavior == "unready" {
				startupTimeout = 200 * time.Millisecond
			}
			err := launchGuardian(context.Background(), Options{OriginURL: settings.Origin}, settings.Binary, settings.RunDir, startupTimeout)
			want := ErrStopped
			if behavior == "unready" {
				want = ErrStartupTimeout
			}
			if !errors.Is(err, want) {
				t.Fatalf("guardian failed with %v, want %v", err, want)
			}
			if _, err := os.Stat(settings.RunDir); !os.IsNotExist(err) {
				t.Fatal("helper failure retained runtime files")
			}
		})
	}
}

func TestGuardianRejectsMalformedOrUnsafeLaunchSettings(t *testing.T) {
	settings := guardianFixture(t, "ready")
	for _, test := range []struct {
		name   string
		change func(*guardianSettings)
	}{
		{"outside binary", func(s *guardianSettings) { s.Binary = filepath.Join(filepath.Dir(s.RunDir), "foreign") }},
		{"relative directory", func(s *guardianSettings) { s.RunDir = "run-relative" }},
		{"non-loopback origin", func(s *guardianSettings) { s.Origin = "http://192.0.2.1:1234" }},
		{"excessive timeout", func(s *guardianSettings) { s.StartupTimeoutMillis = 120001 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			edited := settings
			test.change(&edited)
			data, _ := json.Marshal(edited)
			if err := runGuardian(bytes.NewReader(append(data, '\n')), io.Discard); !errors.Is(err, ErrStartup) {
				t.Fatal("unsafe guardian settings were accepted")
			}
		})
	}
	for _, data := range []string{"{}\n", "null\n", strings.Repeat("x", maxGuardianMessage) + "\n", "{}", `{"unknown":true}` + "\n"} {
		if err := runGuardian(strings.NewReader(data), io.Discard); !errors.Is(err, ErrStartup) {
			t.Fatal("malformed guardian settings were accepted")
		}
	}
	if _, err := os.Stat(settings.Binary); err != nil {
		t.Fatal("invalid settings removed an unrelated runtime directory")
	}
	if err := os.Remove(filepath.Join(settings.RunDir, runtimeMarker)); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(settings)
	if err := runGuardian(bytes.NewReader(append(data, '\n')), io.Discard); !errors.Is(err, ErrStartup) {
		t.Fatal("guardian accepted an unmarked directory for recursive cleanup")
	}
	if _, err := os.Stat(settings.Binary); err != nil {
		t.Fatal("missing marker caused unrelated runtime deletion")
	}
	if handled, err := Guardian([]string{"--ordinary-flag"}); handled || err != nil {
		t.Fatal("guardian intercepted normal application startup")
	}
	if handled, err := Guardian([]string{guardianArgument, "extra"}); !handled || !errors.Is(err, ErrStartup) {
		t.Fatal("guardian accepted extra arguments")
	}
}

func TestGuardianOutputBoundsAndSafeProtocol(t *testing.T) {
	w := &guardianOutput{ready: make(chan string, 1)}
	for _, input := range []string{
		strings.Repeat("x", maxGuardianMessage+1) + "\n",
		`{"type":"ready","url":"https://evil.example"}` + "\n",
		`{"type":"ready","url":"https://guardian-fixture.trycloudflare.com"}` + "\n",
		`{"type":"ready","url":"https://duplicate.trycloudflare.com"}` + "\n",
		`{"type":"result","code":"timeout"}` + "\n",
	} {
		if n, err := w.Write([]byte(input)); err != nil || n != len(input) {
			t.Fatal("guardian output stalled")
		}
	}
	if len(w.ready) != 1 || w.result() != "timeout" || len(w.buffer) != 0 {
		t.Fatal("guardian protocol accepted excessive or invalid messages")
	}
	for _, code := range []string{"startup", "timeout", "cache", "stopped", "canceled", strconv.Itoa(999)} {
		if strings.Contains(guardianResultError(code).Error(), code) && code == "999" {
			t.Fatal("untrusted guardian result reached error text")
		}
	}
}
