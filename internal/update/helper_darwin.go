//go:build darwin

package update

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type parentProcess struct{ queue int }

func openParent(pid int) (*parentProcess, error) {
	queue, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(queue)
	event := syscall.Kevent_t{Ident: uint64(pid), Filter: syscall.EVFILT_PROC, Flags: syscall.EV_ADD | syscall.EV_ENABLE | syscall.EV_ONESHOT, Fflags: syscall.NOTE_EXIT}
	_, err = syscall.Kevent(queue, []syscall.Kevent_t{event}, nil, nil)
	if err != nil {
		syscall.Close(queue)
		return nil, fmt.Errorf("observe update parent: %w", err)
	}
	return &parentProcess{queue: queue}, nil
}
func (p *parentProcess) Close() error { return syscall.Close(p.queue) }
func (p *parentProcess) Wait(timeout time.Duration) error {
	events := make([]syscall.Kevent_t, 1)
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("previous Smart Stage process did not exit; update was not applied")
		}
		timespec := syscall.NsecToTimespec(remaining.Nanoseconds())
		n, err := syscall.Kevent(p.queue, nil, events, &timespec)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 && events[0].Fflags&syscall.NOTE_EXIT != 0 {
			return nil
		}
	}
}
func configureDetached(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func processIsRunning(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
func stopCandidate(pid int, expected string) error {
	if !processIsRunning(pid) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		if !processIsRunning(pid) {
			return nil
		}
		return err
	}
	if !samePath(strings.TrimSpace(string(output)), expected) {
		return errors.New("candidate PID belongs to a different executable")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	for i := 0; i < 50; i++ {
		if !processIsRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	for i := 0; i < 50; i++ {
		if !processIsRunning(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("candidate process did not exit")
}
func restartCommand(target Target, args []string) *exec.Cmd {
	if target.Kind == "bundle" {
		return exec.Command("/usr/bin/open", append([]string{"-W", "-n", target.Path, "--args"}, args...)...)
	}
	return exec.Command(target.Path, args...)
}
func verifyNativeSignature(ctx context.Context, target Target, payload string) error {
	output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", payload).CombinedOutput()
	if err != nil {
		return fmt.Errorf("update code signature is invalid: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func firewallState(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getappblocked", path)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, _ := cmd.Output()
	text := strings.TrimSpace(string(output))
	if strings.Contains(text, " is blocked") || strings.Contains(text, "(Block incoming connections)") {
		return "blocked"
	}
	if strings.Contains(text, " is not blocked") || strings.Contains(text, " is permitted") || strings.Contains(text, "(Allow incoming connections)") {
		return "allowed"
	}
	return "unknown"
}
func configureLANFirewall(target Target, logger *log.Logger) string {
	if os.Getenv("SMARTSTAGE_SKIP_FIREWALL") == "1" {
		logger.Printf("Firewall setup skipped (SMARTSTAGE_SKIP_FIREWALL=1)")
		return "Firewall setup was explicitly skipped (SMARTSTAGE_SKIP_FIREWALL=1); no incoming-connection allowance was verified."
	}
	core := corePath(target)
	bundle := target.Path
	unblockBundle := "0"
	if target.Kind == "bundle" && firewallState(bundle) == "blocked" {
		unblockBundle = "1"
	}
	logger.Printf("Requesting macOS administrator approval to refresh only the incoming-connection rule for %s", core)
	// Paths are arguments, quoted by AppleScript itself; neither an elevated
	// downloaded script nor any global firewall switch is used.
	script := `on run arguments
    set firewallTool to "/usr/libexec/ApplicationFirewall/socketfilterfw"
    set corePath to quoted form of (item 1 of arguments)
    set command to "LC_ALL=C " & firewallTool & " --add " & corePath & " && LC_ALL=C " & firewallTool & " --unblockapp " & corePath
    if item 3 of arguments is "1" then
        set bundlePath to quoted form of (item 2 of arguments)
        set command to command & " && LC_ALL=C " & firewallTool & " --unblockapp " & bundlePath
    end if
    do shell script command with administrator privileges
end run
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-", core, bundle, unblockBundle)
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("Firewall approval failed: %v: %s", err, strings.TrimSpace(string(output)))
		return "macOS firewall approval was cancelled or denied. Allow incoming connections for Smart Stage in System Settings > Network > Firewall > Options; a managed Mac may require your administrator."
	}
	if firewallState(core) != "allowed" || (target.Kind == "bundle" && firewallState(bundle) == "blocked") {
		return "The incoming-connection firewall rule could not be verified. Check Smart Stage in System Settings > Network > Firewall > Options."
	}
	logger.Printf("Verified scoped macOS firewall allowance")
	return ""
}
