// Package update discovers official Smart Stage releases and stages verified
// updates. Automatic installation happens only during startup, before playback
// can begin. Later checks never interrupt an active session.
package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	backgroundCheckInterval = 6 * time.Hour
	minimumCheckInterval    = time.Minute
	maximumCheckAge         = 12 * time.Hour
	checkTimeout            = 10 * time.Second
	installTimeout          = 10 * time.Minute
)

type Options struct {
	CurrentVersion string
	Executable     string
	Args           []string
	ConfigDir      string
	AutoInstall    bool
	// Reserve atomically requires stopped playback and an inactive stage and
	// prevents either starting until the returned release function is called.
	Reserve func() (release func(), err error)
	// Ready takes ownership of the prepared update and must return promptly.
	// The caller shuts the app down gracefully, then calls Prepared.Launch.
	Ready func(*Prepared)
}

type Status struct {
	CurrentVersion string   `json:"currentVersion"`
	LatestVersion  string   `json:"latestVersion"`
	Phase          string   `json:"phase"`
	Available      bool     `json:"available"`
	CanInstall     bool     `json:"canInstall"`
	Message        string   `json:"message"`
	ReleaseURL     string   `json:"releaseURL"`
	CheckedAt      string   `json:"checkedAt"`
	LastUpdate     *Outcome `json:"lastUpdate,omitempty"`
}

type managerDependencies struct {
	client         *http.Client
	detectTarget   func(string) (Target, error)
	stage          func(context.Context, Target, string, string, []string, string) (*Prepared, error)
	now            func() time.Time
	readOutcome    func(string) (Outcome, error)
	cleanupResidue func(string) error
}

type Manager struct {
	mu              sync.Mutex
	options         Options
	target          Target
	client          *http.Client
	stage           func(context.Context, Target, string, string, []string, string) (*Prepared, error)
	now             func() time.Time
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	started         bool
	closed          bool
	unsupported     bool
	status          Status
	candidate       *candidate
	lastAttempt     time.Time
	lastCheck       time.Time
	retryAfter      time.Time
	lastOutcomeRead time.Time
	readOutcome     func(string) (Outcome, error)
	cleanupResidue  func(string) error
}

func New(options Options) *Manager {
	return newManager(options, managerDependencies{})
}

func newManager(options Options, dependencies managerDependencies) *Manager {
	if dependencies.client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 20 * time.Second
		dependencies.client = &http.Client{Transport: transport, Timeout: installTimeout, CheckRedirect: githubRedirect}
	}
	if dependencies.detectTarget == nil {
		dependencies.detectTarget = DetectTarget
	}
	if dependencies.stage == nil {
		dependencies.stage = Stage
	}
	if dependencies.now == nil {
		dependencies.now = time.Now
	}
	if dependencies.readOutcome == nil {
		dependencies.readOutcome = ReadOutcome
	}
	if dependencies.cleanupResidue == nil {
		dependencies.cleanupResidue = CleanupResidue
	}
	options.Args = append([]string(nil), options.Args...)
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{options: options, client: dependencies.client, stage: dependencies.stage, now: dependencies.now, ctx: ctx, cancel: cancel,
		readOutcome: dependencies.readOutcome, cleanupResidue: dependencies.cleanupResidue, lastOutcomeRead: dependencies.now(),
		status: Status{CurrentVersion: options.CurrentVersion, Phase: "idle", Message: "Updates are checked automatically."}}
	_ = dependencies.cleanupResidue(options.ConfigDir)
	if outcome, err := dependencies.readOutcome(options.ConfigDir); err == nil {
		m.status.LastUpdate = &outcome
	}
	if _, err := parseVersion(options.CurrentVersion); err != nil {
		m.unsupported = true
		m.status.Phase = "unsupported"
		m.status.Message = "Development builds cannot update automatically. Install an official release first."
		return m
	}
	target, err := dependencies.detectTarget(options.Executable)
	if err == nil {
		_, err = archiveName(target)
	}
	if err != nil {
		m.unsupported = true
		m.status.Phase = "unsupported"
		m.status.Message = err.Error()
		return m
	}
	m.target = target
	return m
}

// Start reserves the application synchronously when automatic installation is
// enabled. The first check runs asynchronously, but playback cannot race it.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || m.closed || m.unsupported {
		return
	}
	m.started = true
	if ctx.Err() != nil {
		m.cancel()
		return
	}
	var unreserve func()
	if m.options.AutoInstall && m.options.Reserve != nil && m.options.Ready != nil {
		// Failure means somebody has already started using the app. Check only;
		// never stop playback or turn off an active stage to force an update.
		unreserve, _ = m.options.Reserve()
	}
	if !m.busyLocked() && m.lastAttempt.IsZero() {
		m.startCheckLocked(unreserve)
	} else if unreserve != nil {
		unreserve()
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(backgroundCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				m.cancel()
				return
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				_ = m.Check()
			}
		}
	}()
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	// The helper writes its final result after the new process has initialized
	// this manager. Refresh it so firewall warnings and rollback results reach
	// the Admin UI without requiring another restart.
	if m.now().Sub(m.lastOutcomeRead) >= time.Second {
		m.lastOutcomeRead = m.now()
		if outcome, err := m.readOutcome(m.options.ConfigDir); err == nil {
			m.status.LastUpdate = &outcome
		}
	}
	status := m.status
	if status.LastUpdate != nil {
		outcome := *status.LastUpdate
		status.LastUpdate = &outcome
	}
	status.Available = m.candidate != nil
	status.CanInstall = status.Available && !m.closed && m.ctx.Err() == nil && !m.busyLocked() && m.options.Reserve != nil && m.options.Ready != nil && m.now().Sub(m.lastCheck) <= maximumCheckAge
	return status
}

// Check starts an asynchronous check. Repeated clicks reuse the last result for
// a minute; automatic checks occur every six hours to respect public API limits.
func (m *Manager) Check() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return fmt.Errorf("updater is shutting down")
	}
	if m.unsupported {
		return fmt.Errorf("%s", m.status.Message)
	}
	if m.busyLocked() {
		return fmt.Errorf("an update operation is already in progress")
	}
	if m.now().Before(m.retryAfter) {
		return &releaseRateLimitError{until: m.retryAfter}
	}
	if !m.lastAttempt.IsZero() && m.now().Sub(m.lastAttempt) < minimumCheckInterval {
		return nil
	}
	_ = m.cleanupResidue(m.options.ConfigDir)
	m.startCheckLocked(nil)
	return nil
}

func (m *Manager) startCheckLocked(unreserve func()) {
	m.lastAttempt = m.now()
	m.status.Phase = "checking"
	m.status.Message = "Checking GitHub for a newer release…"
	if unreserve != nil {
		m.status.Message = "Checking for a startup update before enabling playback…"
	}
	m.wg.Add(1)
	go m.check(unreserve)
}

func (m *Manager) check(unreserve func()) {
	defer m.wg.Done()
	defer func() {
		if unreserve != nil {
			unreserve()
		}
	}()
	ctx, cancel := context.WithTimeout(m.ctx, checkTimeout)
	defer cancel()
	release, err := m.discover(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return
	}
	m.status.CheckedAt = m.now().UTC().Format(time.RFC3339)
	m.candidate = nil
	m.status.LatestVersion = ""
	m.status.ReleaseURL = ""
	if err != nil {
		var limited *releaseRateLimitError
		if errors.As(err, &limited) {
			m.retryAfter = limited.until
		}
		m.status.Phase = "error"
		m.status.Message = "Update check failed: " + err.Error()
		return
	}
	m.lastCheck = m.now()
	m.candidate = release
	if release == nil {
		m.status.Phase = "idle"
		m.status.LatestVersion = m.options.CurrentVersion
		m.status.Message = "Smart Stage is up to date."
		return
	}
	m.status.Phase = "available"
	m.status.LatestVersion = release.version
	m.status.ReleaseURL = repositoryURL + "/releases/tag/" + release.version
	m.status.Message = "A new release is available. Stop playback and turn off the stage to install it."
	if m.options.AutoInstall {
		m.status.Message = "A new release is available and will install automatically on the next launch."
	}
	if outcome := m.status.LastUpdate; outcome != nil && outcome.Version == release.version && (outcome.Status == "rolled_back" || outcome.Status == "error") {
		m.status.Message = "The previous attempt to install this version failed. Automatic retry is paused; you can retry it manually."
		return
	}
	if unreserve != nil {
		m.startInstallLocked(*release, unreserve)
		unreserve = nil // The installation now owns this startup reservation.
	}
}

func (m *Manager) Install() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return fmt.Errorf("updater is shutting down")
	}
	if m.unsupported {
		return fmt.Errorf("%s", m.status.Message)
	}
	if m.busyLocked() {
		return fmt.Errorf("an update operation is already in progress")
	}
	if m.candidate == nil || m.now().Sub(m.lastCheck) > maximumCheckAge {
		return fmt.Errorf("check for updates before installing")
	}
	if m.options.Reserve == nil || m.options.Ready == nil {
		return fmt.Errorf("automatic installation is unavailable in this session")
	}
	release, err := m.options.Reserve()
	if err != nil {
		return err
	}
	if release == nil {
		return fmt.Errorf("cannot reserve the application for updating")
	}
	m.startInstallLocked(*m.candidate, release)
	return nil
}

func (m *Manager) startInstallLocked(release candidate, unreserve func()) {
	m.status.Phase = "downloading"
	m.status.Message = "Downloading and verifying the update. Smart Stage will restart when it is ready."
	m.wg.Add(1)
	go m.install(release, unreserve)
}

func (m *Manager) install(release candidate, unreserve func()) {
	defer m.wg.Done()
	transferred := false
	defer func() {
		if !transferred {
			unreserve()
		}
	}()
	ctx, cancel := context.WithTimeout(m.ctx, installTimeout)
	defer cancel()
	archive, cleanup, err := m.download(ctx, release)
	if err != nil {
		m.installError(err)
		return
	}
	defer cleanup()
	prepared, err := m.stage(ctx, m.target, archive, release.version, m.options.Args, m.options.ConfigDir)
	if err != nil {
		m.installError(err)
		return
	}
	if prepared == nil {
		m.installError(fmt.Errorf("update staging did not prepare an installer"))
		return
	}
	m.mu.Lock()
	if m.closed || ctx.Err() != nil {
		m.mu.Unlock()
		_ = prepared.Abort()
		return
	}
	m.status.Phase = "restarting"
	m.status.Message = "Update verified. Restarting Smart Stage…"
	m.mu.Unlock()
	transferred = true
	m.options.Ready(prepared)
}

func (m *Manager) installError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return
	}
	m.status.Phase = "error"
	m.status.Message = "Update could not be prepared: " + err.Error()
}

func (m *Manager) busyLocked() bool {
	return m.status.Phase == "checking" || m.status.Phase == "downloading" || m.status.Phase == "restarting"
}

// Close cancels network and staging work and allows at most two seconds for it
// to finish. A prepared update already handed to Ready belongs to the caller.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
