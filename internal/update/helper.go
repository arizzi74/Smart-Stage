package update

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const helperReady = "SMARTSTAGE_UPDATE_READY"
const parentExitTimeout = 90 * time.Second
const startupTimeout = 90 * time.Second

type Outcome struct {
	Version   string    `json:"version"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Time      time.Time `json:"time"`
	Work      string    `json:"work,omitempty"`
	HelperPID int       `json:"helperPID,omitempty"`
}

type startupRequest struct {
	Version string `json:"version"`
	Nonce   string `json:"nonce"`
	Target  Target `json:"target"`
}
type startupReceipt struct {
	Version string `json:"version"`
	Nonce   string `json:"nonce"`
	PID     int    `json:"pid"`
}

func (p *Prepared) Launch() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.launched {
		return errors.New("update helper already launched")
	}
	if err := validatePlan(p.plan, filepath.Join(p.plan.Work, "plan.json")); err != nil {
		return err
	}
	logFile, err := openUpdateLog(p.plan.ConfigDir)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(p.plan.Helper, "--smartstage-apply-update", filepath.Join(p.plan.Work, "plan.json"))
	cmd.Env = cleanEnvironment()
	cmd.Dir = p.plan.Cwd
	cmd.Stderr = logFile
	configureDetached(cmd)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		pipe.Close()
		return fmt.Errorf("start update helper: %w", err)
	}
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		if scanner.Scan() && scanner.Text() == helperReady {
			ready <- nil
		} else {
			ready <- errors.New("update helper did not confirm readiness; see update.log")
		}
	}()
	select {
	case err = <-ready:
	case <-time.After(10 * time.Second):
		err = errors.New("update helper startup timed out")
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		pipe.Close()
		return err
	}
	p.launched = true
	// The parent exits immediately after Launch returns. Reap normally if it is
	// kept alive by a test; the detached helper itself owns all remaining work.
	go func() { _ = cmd.Wait(); pipe.Close() }()
	return nil
}

func RunHelper(planPath string) error {
	var p applyPlan
	if err := readJSON(planPath, &p); err != nil {
		return err
	}
	if err := validatePlan(p, planPath); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(self, p.Helper) {
		return errors.New("update plan must be run by its staged helper")
	}
	if err := transferInstallLock(p.Lock, p.Nonce, os.Getpid()); err != nil {
		return err
	}
	defer releaseInstallLock(p.Lock, p.Nonce)
	logFile, err := openUpdateLog(p.ConfigDir)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.LstdFlags|log.LUTC)
	logger.Printf("Preparing %s replacement for %s", p.Version, p.Target.Path)
	parent, err := openParent(p.ParentPID)
	if err != nil {
		return err
	}
	defer parent.Close()
	// In particular, Windows now owns a SYNCHRONIZE handle for the exact parent
	// process before the parent is allowed to exit and unlock its executable.
	fmt.Fprintln(os.Stdout, helperReady)
	if err := parent.Wait(parentExitTimeout); err != nil {
		return finishOutcome(p, "error", err.Error(), logger)
	}
	if err := checkTarget(p.Target); err != nil {
		return finishOutcome(p, "error", err.Error(), logger)
	}
	current, err := hashFile(context.Background(), corePath(p.Target))
	if err != nil {
		return finishOutcome(p, "error", err.Error(), logger)
	}
	if current != p.OldHash {
		return finishOutcome(p, "error", "The installed executable changed after the update was prepared; no files were replaced.", logger)
	}
	if err := retryRename(p.Target.Path, p.Backup); err != nil {
		resultErr := finishOutcome(p, "error", fmt.Sprintf("Could not preserve the current installation; the unchanged version was retained: %v", err), logger)
		if _, launchErr := launchInstalled(p, ""); launchErr != nil {
			return fmt.Errorf("%v; restart the unchanged version manually: %w", resultErr, launchErr)
		}
		return resultErr
	}
	if err := retryRename(p.Payload, p.Target.Path); err != nil {
		restoreErr := retryRename(p.Backup, p.Target.Path)
		if restoreErr != nil {
			return finishOutcome(p, "error", fmt.Sprintf("Installation failed (%v); restore %s to %s: %v", err, p.Backup, p.Target.Path, restoreErr), logger)
		}
		_ = finishOutcome(p, "rolled_back", fmt.Sprintf("Replacement failed; previous version restored: %v", err), logger)
		_, launchErr := launchInstalled(p, "")
		return launchErr
	}
	logger.Printf("Replaced installation; previous version retained at %s", p.Backup)
	warning := refreshFirewall(p.Target, logger)
	requestPath := filepath.Join(p.Work, "startup.json")
	if err := writeJSON(requestPath, startupRequest{Version: p.Version, Nonce: p.Nonce, Target: p.Target}); err != nil {
		return rollback(p, nil, requestPath, err, logger)
	}
	child, err := launchInstalled(p, requestPath)
	if err != nil {
		return rollback(p, nil, requestPath, err, logger)
	}
	if err := waitStartup(requestPath, p.Version, p.Nonce, child, startupTimeout); err != nil {
		return rollback(p, child, requestPath, err, logger)
	}
	logger.Printf("New version confirmed initialized listeners and storage")
	if err := os.RemoveAll(p.Backup); err != nil {
		warning = joinMessage(warning, "The update succeeded, but its previous-version backup could not be removed: "+err.Error())
	}
	if err := finishOutcome(p, "updated", warning, logger); err != nil {
		return err
	}
	// On Windows the running helper is locked. Remove everything possible now;
	// CleanupResidue removes its final small directory on a later check/start.
	_ = os.RemoveAll(filepath.Join(p.Work, "unpacked"))
	_ = os.Remove(filepath.Join(p.Work, "plan.json"))
	_ = os.Remove(requestPath)
	_ = os.Remove(requestPath + ".started")
	_ = os.Remove(requestPath + ".ready")
	if runtime.GOOS != "windows" {
		_ = os.RemoveAll(p.Work)
	}
	return nil
}

type launchedApp struct {
	cmd  *exec.Cmd
	done chan error
}

func launchInstalled(p applyPlan, receipt string) (*launchedApp, error) {
	args := append([]string(nil), p.Args...)
	if receipt != "" {
		args = append(args, "--update-receipt", receipt)
	}
	cmd := restartCommand(p.Target, args)
	cmd.Dir = p.Cwd
	cmd.Env = cleanEnvironment()
	configureDetached(cmd)
	output, err := openUpdateLog(p.ConfigDir)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = output
	cmd.Stderr = output
	if err = cmd.Start(); err != nil {
		output.Close()
		return nil, err
	}
	child := &launchedApp{cmd: cmd, done: make(chan error, 1)}
	go func() { err := cmd.Wait(); output.Close(); child.done <- err; close(child.done) }()
	return child, nil
}

func waitStartup(path, version, nonce string, child *launchedApp, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		var receipt startupReceipt
		if err := readJSON(path+".ready", &receipt); err == nil && receipt.Version == version && receipt.Nonce == nonce && receipt.PID > 1 {
			if !processIsRunning(receipt.PID) {
				return errors.New("new version exited immediately after reporting startup")
			}
			return nil
		}
		select {
		case <-deadline.C:
			return errors.New("new version did not confirm startup within 90 seconds")
		case err := <-child.done:
			if err != nil {
				return fmt.Errorf("new version exited before startup: %w", err)
			}
			return errors.New("new version exited before startup")
		case <-tick.C:
		}
	}
}

func rollback(p applyPlan, child *launchedApp, request string, cause error, logger *log.Logger) error {
	logger.Printf("Candidate startup failed: %v", cause)
	var started startupReceipt
	if err := readJSON(request+".started", &started); err == nil && started.Nonce == p.Nonce && started.Version == p.Version && started.PID > 1 {
		if err := stopCandidate(started.PID, corePath(p.Target)); err != nil {
			return finishOutcome(p, "error", fmt.Sprintf("Update startup failed (%v). The candidate could not be stopped safely (%v); the previous version remains at %s.", cause, err, p.Backup), logger)
		}
	} else if child != nil {
		// A bundle is launched through LaunchServices, so open's PID is not the
		// application's PID. Never kill an unrelated process or swap under an
		// unregistered app whose liveness cannot be established.
		select {
		case <-child.done:
		default:
			if p.Target.Kind == "bundle" {
				return finishOutcome(p, "error", fmt.Sprintf("Update startup could not be confirmed (%v). Quit Smart Stage and restore the previous version from %s.", cause, p.Backup), logger)
			}
			if err := stopCandidate(child.cmd.Process.Pid, corePath(p.Target)); err != nil {
				return finishOutcome(p, "error", fmt.Sprintf("Cannot stop failed update: %v. Previous version: %s", err, p.Backup), logger)
			}
		}
	}
	failed := filepath.Join(p.Work, "failed")
	if err := retryRename(p.Target.Path, failed); err != nil {
		return finishOutcome(p, "error", fmt.Sprintf("Cannot move failed update: %v. Previous version: %s", err, p.Backup), logger)
	}
	if err := retryRename(p.Backup, p.Target.Path); err != nil {
		_ = retryRename(failed, p.Target.Path)
		return finishOutcome(p, "error", fmt.Sprintf("Cannot restore the previous version: %v. Backup: %s", err, p.Backup), logger)
	}
	warning := refreshFirewall(p.Target, logger)
	message := joinMessage("The update did not start successfully; the previous version was restored. "+cause.Error(), warning)
	if err := finishOutcome(p, "rolled_back", message, logger); err != nil {
		return err
	}
	if _, err := launchInstalled(p, ""); err != nil {
		return finishOutcome(p, "error", message+" Restart the previous version manually: "+err.Error(), logger)
	}
	_ = os.RemoveAll(failed)
	if runtime.GOOS != "windows" {
		_ = os.RemoveAll(p.Work)
	}
	return nil
}

// RegisterStartup is called before native initialization, so a failed update can
// be identified and stopped without guessing a PID or terminating another copy.
func RegisterStartup(path, version string) error {
	request, err := loadStartupRequest(path, version)
	if err != nil {
		return err
	}
	return writeJSON(path+".started", startupReceipt{Version: version, Nonce: request.Nonce, PID: os.Getpid()})
}

// ConfirmStartup acknowledges only a process that initialized its real storage
// and HTTP listeners. The helper retains the previous installation until then.
func ConfirmStartup(path, version string) error {
	request, err := loadStartupRequest(path, version)
	if err != nil {
		return err
	}
	var started startupReceipt
	if err := readJSON(path+".started", &started); err != nil {
		return err
	}
	if started.Nonce != request.Nonce || started.Version != version || started.PID != os.Getpid() {
		return errors.New("update startup registration does not match this process")
	}
	return writeJSON(path+".ready", started)
}

func loadStartupRequest(path, version string) (startupRequest, error) {
	var r startupRequest
	if !filepath.IsAbs(path) || filepath.Base(path) != "startup.json" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), ".smartstage-update-") {
		return r, errors.New("invalid update startup receipt path")
	}
	if err := noSymlinks(path); err != nil {
		return r, err
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return r, err
	}
	if err := readJSON(path, &r); err != nil {
		return r, err
	}
	if r.Version != version || len(r.Nonce) != 64 {
		return r, errors.New("update startup version or nonce is invalid")
	}
	if filepath.Dir(r.Target.Path) != filepath.Dir(filepath.Dir(path)) {
		return r, errors.New("update startup receipt is outside the installation folder")
	}
	self, err := os.Executable()
	if err != nil {
		return r, err
	}
	if !samePath(self, corePath(r.Target)) {
		return r, errors.New("update startup receipt belongs to another executable")
	}
	return r, nil
}

func validatePlan(p applyPlan, planPath string) error {
	if !filepath.IsAbs(planPath) || planPath != filepath.Join(p.Work, "plan.json") || !strings.HasPrefix(filepath.Base(p.Work), ".smartstage-update-") {
		return errors.New("invalid update helper plan path")
	}
	if err := noSymlinks(planPath); err != nil {
		return err
	}
	if err := privateDirectory(p.Work); err != nil {
		return err
	}
	if filepath.Dir(p.Work) != filepath.Dir(p.Target.Path) || p.Backup != filepath.Join(p.Work, "previous") || p.ParentPID <= 1 || len(p.Nonce) != 64 || len(p.OldHash) != 64 || p.Version == "" {
		return errors.New("invalid update helper plan")
	}
	if p.Lock != filepath.Join(filepath.Dir(p.Target.Path), ".smartstage-install.lock") {
		return errors.New("invalid update installation lock path")
	}
	if err := checkInstallLock(p.Lock, p.Nonce); err != nil {
		return err
	}
	expectedPayload := "smartstage"
	if p.Target.GOOS == "windows" {
		expectedPayload += ".exe"
	}
	if p.Target.Kind == "bundle" {
		expectedPayload = "Smart Stage.app"
	}
	expectedHelper := "update-helper"
	if p.Target.GOOS == "windows" {
		expectedHelper += ".exe"
	}
	if p.Payload != filepath.Join(p.Work, "unpacked", expectedPayload) || p.Helper != filepath.Join(p.Work, expectedHelper) {
		return errors.New("update helper plan paths escape the staging directory")
	}
	if !filepath.IsAbs(p.ConfigDir) || !filepath.IsAbs(p.Cwd) {
		return errors.New("update helper requires absolute configuration and working directories")
	}
	if err := noSymlinks(p.ConfigDir); err != nil {
		return err
	}
	if err := noSymlinks(p.Payload); err != nil {
		return err
	}
	return checkTarget(p.Target)
}

func privateDirectory(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		return errors.New("update staging directory is not private")
	}
	return nil
}
func cleanEnvironment() []string {
	var result []string
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if name == "SMARTSTAGE_APP_LAUNCH" || name == "SMARTSTAGE_APP_ICON" || name == "SMARTSTAGE_LOG_PATH" {
			continue
		}
		result = append(result, entry)
	}
	return result
}
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return canonicalSystemPath(filepath.Clean(a)) == canonicalSystemPath(filepath.Clean(b))
}
func retryRename(from, to string) error {
	var err error
	deadline := time.Now().Add(8 * time.Second)
	for {
		err = os.Rename(from, to)
		if err == nil || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() > 1024*1024 {
		return errors.New("invalid update JSON file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 1024*1024))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}
func joinMessage(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + " " + b
}
func openUpdateLog(dir string) (*os.File, error) {
	path := filepath.Join(dir, "update.log")
	if st, err := os.Lstat(path); err == nil {
		if !st.Mode().IsRegular() {
			return nil, errors.New("update log is not a regular file")
		}
		if st.Size() > 2*1024*1024 {
			_ = os.Remove(path + ".1")
			if err := os.Rename(path, path+".1"); err != nil {
				return nil, err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}
func finishOutcome(p applyPlan, status, message string, logger *log.Logger) error {
	logger.Printf("Update result: %s: %s", status, message)
	err := writeJSON(filepath.Join(p.ConfigDir, "update-result.json"), Outcome{Version: p.Version, Status: status, Message: message, Time: time.Now().UTC(), Work: p.Work, HelperPID: os.Getpid()})
	if err != nil {
		return err
	}
	if status == "error" {
		return errors.New(message)
	}
	return nil
}
func ReadOutcome(configDir string) (Outcome, error) {
	var result Outcome
	err := readJSON(filepath.Join(configDir, "update-result.json"), &result)
	return result, err
}

// CleanupResidue never searches installation directories. It only removes the
// exact successful/rolled-back helper directory named in the durable result,
// after that helper has exited and no previous-version backup remains.
func CleanupResidue(configDir string) error {
	r, err := ReadOutcome(configDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Status != "updated" && r.Status != "rolled_back" {
		return nil
	}
	if r.Work == "" || !filepath.IsAbs(r.Work) || !strings.HasPrefix(filepath.Base(r.Work), ".smartstage-update-") {
		return errors.New("invalid recorded update cleanup directory")
	}
	if processIsRunning(r.HelperPID) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(r.Work, "previous")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(r.Work); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := noSymlinks(r.Work); err != nil {
		return err
	}
	return os.RemoveAll(r.Work)
}
