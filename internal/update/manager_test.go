package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func managerTestInstance(t *testing.T, options Options, dependencies managerDependencies) *Manager {
	t.Helper()
	if options.CurrentVersion == "" {
		options.CurrentVersion = "v0.1.0-preview.8"
	}
	options.ConfigDir = t.TempDir()
	dependencies.detectTarget = func(string) (Target, error) {
		return Target{Path: "/Applications/Smart Stage.app", Kind: "bundle", GOOS: "darwin", GOARCH: "arm64"}, nil
	}
	if dependencies.readOutcome == nil {
		dependencies.readOutcome = func(string) (Outcome, error) { return Outcome{}, os.ErrNotExist }
	}
	dependencies.cleanupResidue = func(string) error { return nil }
	m := newManager(options, dependencies)
	t.Cleanup(m.Close)
	return m
}

func managerWaitPhase(t *testing.T, m *Manager, phase string) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Status()
		if s.Phase == phase {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("updater did not reach %s: %+v", phase, m.Status())
	return Status{}
}

func managerReleaseTransport(release candidate) managerRoundTripper {
	_, download := managerDownloadFixture("verified archive bytes")
	return func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			data, _ := json.Marshal([]githubRelease{{Tag: release.version, Prerelease: true, Assets: []releaseAsset{release.archive, release.checksum}}})
			return managerResponse(string(data)), nil
		}
		return download(r)
	}
}

func TestStartupAutomaticallyInstallsWithOneSynchronousReservation(t *testing.T) {
	release, _ := managerDownloadFixture("verified archive bytes")
	transport := managerReleaseTransport(release)
	gate := make(chan struct{})
	ready := make(chan *Prepared, 1)
	var reserved, released, stages atomic.Int32
	work := t.TempDir()
	m := managerTestInstance(t, Options{AutoInstall: true, Reserve: func() (func(), error) {
		reserved.Add(1)
		return func() { released.Add(1) }, nil
	}, Ready: func(p *Prepared) { ready <- p }}, managerDependencies{
		client: &http.Client{Transport: managerRoundTripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "api.github.com" {
				select {
				case <-gate:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			return transport(r)
		})},
		stage: func(_ context.Context, _ Target, path, version string, _ []string, _ string) (*Prepared, error) {
			stages.Add(1)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "verified archive bytes" || version != release.version {
				return nil, errors.New("stage received unverified or incorrect payload")
			}
			return &Prepared{plan: applyPlan{Work: work}}, nil
		},
	})
	m.Start(context.Background())
	if reserved.Load() != 1 || m.Status().Phase != "checking" || m.Status().CanInstall {
		t.Fatalf("startup did not reserve synchronously: reserved=%d status=%+v", reserved.Load(), m.Status())
	}
	close(gate)
	select {
	case prepared := <-ready:
		defer prepared.Abort()
	case <-time.After(3 * time.Second):
		t.Fatalf("update was not handed off: %+v", m.Status())
	}
	if reserved.Load() != 1 || released.Load() != 0 || stages.Load() != 1 || m.Status().Phase != "restarting" {
		t.Fatalf("incorrect ownership transfer: reserved=%d released=%d staged=%d status=%+v", reserved.Load(), released.Load(), stages.Load(), m.Status())
	}
	m.Close()
	if released.Load() != 0 {
		t.Fatal("shutdown released the update reservation before restarting")
	}
}

func TestStartupReleasesReservationWhenNoUpdateOrOffline(t *testing.T) {
	for _, offline := range []bool{false, true} {
		t.Run(map[bool]string{false: "up to date", true: "offline"}[offline], func(t *testing.T) {
			released := make(chan struct{}, 1)
			m := managerTestInstance(t, Options{AutoInstall: true, Reserve: func() (func(), error) {
				return func() { released <- struct{}{} }, nil
			}, Ready: func(*Prepared) { t.Error("unexpected install") }}, managerDependencies{
				client: &http.Client{Transport: managerRoundTripper(func(r *http.Request) (*http.Response, error) {
					deadline, ok := r.Context().Deadline()
					if !ok || time.Until(deadline) > 10*time.Second {
						t.Error("startup metadata request lacks a short deadline")
					}
					if offline {
						return nil, errors.New("network unavailable")
					}
					return managerResponse("[]"), nil
				})},
			})
			m.Start(context.Background())
			select {
			case <-released:
			case <-time.After(time.Second):
				t.Fatal("playback was left reserved")
			}
			wantPhase := "idle"
			if offline {
				wantPhase = "error"
			}
			if got := m.Status(); got.Phase != wantPhase || got.CanInstall || got.Available {
				t.Fatalf("unexpected completion: %+v", got)
			}
		})
	}
}

func TestLaterChecksNeverInstallDuringTheSession(t *testing.T) {
	release, _ := managerDownloadFixture("verified archive bytes")
	transport := managerReleaseTransport(release)
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	var calls, reserves, stages atomic.Int32
	released := make(chan struct{}, 1)
	m := managerTestInstance(t, Options{AutoInstall: true, Reserve: func() (func(), error) {
		reserves.Add(1)
		return func() { released <- struct{}{} }, nil
	}, Ready: func(*Prepared) { t.Error("later check unexpectedly installed") }}, managerDependencies{
		now: func() time.Time { return time.Unix(0, now.Load()) },
		client: &http.Client{Transport: managerRoundTripper(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return managerResponse("[]"), nil
			}
			return transport(r)
		})},
		stage: func(context.Context, Target, string, string, []string, string) (*Prepared, error) {
			stages.Add(1)
			return nil, errors.New("unexpected staging")
		},
	})
	m.Start(context.Background())
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("startup check did not finish")
	}
	if err := m.Check(); err != nil || calls.Load() != 1 {
		t.Fatalf("minimum check interval was not honored: calls=%d err=%v", calls.Load(), err)
	}
	now.Add(int64(2 * time.Minute))
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	s := managerWaitPhase(t, m, "available")
	if reserves.Load() != 1 || stages.Load() != 0 || !s.Available || !s.CanInstall {
		t.Fatalf("later check reserved or installed: reserves=%d stages=%d status=%+v", reserves.Load(), stages.Load(), s)
	}
}

func TestStartupDoesNotInterruptBusyAppOrRepeatFailedUpdate(t *testing.T) {
	for _, mode := range []string{"busy app", "previous rollback", "automatic disabled"} {
		t.Run(mode, func(t *testing.T) {
			release, _ := managerDownloadFixture("verified archive bytes")
			var reserves, releases, stages atomic.Int32
			m := managerTestInstance(t, Options{AutoInstall: mode != "automatic disabled", Reserve: func() (func(), error) {
				reserves.Add(1)
				if mode == "busy app" {
					return nil, errors.New("playback is active")
				}
				return func() { releases.Add(1) }, nil
			}, Ready: func(*Prepared) { t.Error("unexpected automatic install") }}, managerDependencies{
				client: &http.Client{Transport: managerReleaseTransport(release)},
				readOutcome: func(string) (Outcome, error) {
					if mode == "previous rollback" {
						return Outcome{Version: release.version, Status: "rolled_back", Message: "Startup failed."}, nil
					}
					return Outcome{}, os.ErrNotExist
				},
				stage: func(context.Context, Target, string, string, []string, string) (*Prepared, error) {
					stages.Add(1)
					return nil, errors.New("unexpected staging")
				},
			})
			m.Start(context.Background())
			managerWaitPhase(t, m, "available")
			m.Close()
			if stages.Load() != 0 || mode == "automatic disabled" && reserves.Load() != 0 || mode == "previous rollback" && releases.Load() != 1 {
				t.Fatalf("unsafe startup decision: reserves=%d releases=%d stages=%d", reserves.Load(), releases.Load(), stages.Load())
			}
		})
	}
}

func TestFailedStagingReleasesAppAndDoesNotRestart(t *testing.T) {
	release, _ := managerDownloadFixture("verified archive bytes")
	released := make(chan struct{}, 1)
	m := managerTestInstance(t, Options{Reserve: func() (func(), error) {
		return func() { released <- struct{}{} }, nil
	}, Ready: func(*Prepared) { t.Error("failed staging attempted restart") }}, managerDependencies{
		client: &http.Client{Transport: managerReleaseTransport(release)},
		stage: func(context.Context, Target, string, string, []string, string) (*Prepared, error) {
			return nil, errors.New("installation folder is read only")
		},
	})
	if err := m.Install(); err == nil {
		t.Fatal("installation accepted without a checked release")
	}
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	managerWaitPhase(t, m, "available")
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("failed staging kept app reserved")
	}
	if s := m.Status(); s.Phase != "error" || !s.CanInstall {
		t.Fatalf("failed staging is not retryable: %+v", s)
	}
}

func TestCloseCancelsStartupCheckAndReleasesApp(t *testing.T) {
	entered := make(chan struct{})
	var released atomic.Int32
	m := managerTestInstance(t, Options{AutoInstall: true, Reserve: func() (func(), error) {
		return func() { released.Add(1) }, nil
	}, Ready: func(*Prepared) { t.Error("unexpected restart") }}, managerDependencies{
		client: &http.Client{Transport: managerRoundTripper(func(r *http.Request) (*http.Response, error) {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})},
	})
	m.Start(context.Background())
	<-entered
	start := time.Now()
	m.Close()
	if time.Since(start) > time.Second || released.Load() != 1 {
		t.Fatalf("cancellation did not promptly release reservation: elapsed=%s released=%d", time.Since(start), released.Load())
	}
	if err := m.Check(); err == nil {
		t.Fatal("closed updater accepted new work")
	}
}

func TestDevelopmentBuildDoesNotCheckOrReserve(t *testing.T) {
	m := managerTestInstance(t, Options{CurrentVersion: "dev", AutoInstall: true, Reserve: func() (func(), error) {
		t.Error("development build reserved playback")
		return nil, errors.New("unsupported")
	}, Ready: func(*Prepared) {}}, managerDependencies{client: &http.Client{Transport: managerRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Error("development build requested release metadata")
		return managerResponse("[]"), nil
	})}})
	m.Start(context.Background())
	if m.Status().Phase != "unsupported" || m.Install() == nil {
		t.Fatalf("development build supports automatic installation: %+v", m.Status())
	}
}

func TestStatusRefreshesHelperOutcomeAfterNewProcessStarts(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	var written atomic.Bool
	m := managerTestInstance(t, Options{}, managerDependencies{
		now: func() time.Time { return time.Unix(0, now.Load()) },
		readOutcome: func(string) (Outcome, error) {
			if written.Load() {
				return Outcome{Version: "v0.1.0-preview.10", Status: "updated", Message: "Allow incoming connections in Firewall Options."}, nil
			}
			return Outcome{}, os.ErrNotExist
		},
	})
	if m.Status().LastUpdate != nil {
		t.Fatal("outcome existed before helper completed")
	}
	written.Store(true)
	now.Add(int64(2 * time.Second))
	outcome := m.Status().LastUpdate
	if outcome == nil || outcome.Status != "updated" || outcome.Message == "" {
		t.Fatalf("helper result was not refreshed: %+v", outcome)
	}
	outcome.Message = "caller changed the snapshot"
	if m.Status().LastUpdate.Message == outcome.Message {
		t.Fatal("status snapshot exposed mutable manager state")
	}
}

func TestGitHubRateLimitBackoff(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	var calls atomic.Int32
	m := managerTestInstance(t, Options{}, managerDependencies{
		now: func() time.Time { return time.Unix(0, now.Load()) },
		client: &http.Client{Transport: managerRoundTripper(func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				resp := managerResponse("rate limited")
				resp.StatusCode = http.StatusTooManyRequests
				resp.Header.Set("Retry-After", "120")
				return resp, nil
			}
			return managerResponse("[]"), nil
		})},
	})
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	managerWaitPhase(t, m, "error")
	now.Add(int64(61 * time.Second))
	if err := m.Check(); err == nil || calls.Load() != 1 {
		t.Fatalf("rate-limit retry delay ignored: calls=%d err=%v", calls.Load(), err)
	}
	now.Add(int64(60 * time.Second))
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	managerWaitPhase(t, m, "idle")
	if calls.Load() != 2 {
		t.Fatalf("rate limit failed to recover: calls=%d", calls.Load())
	}
}
