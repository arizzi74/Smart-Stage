//go:build !darwin && !windows

package update

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Linux supports the process tests, but DetectTarget never enables installation
// there: Smart Stage publishes native release assets only for macOS and Windows.
type parentProcess struct{ pid int }

func openParent(pid int) (*parentProcess, error) {
	if !processIsRunning(pid) {
		return nil, errors.New("update parent no longer exists")
	}
	return &parentProcess{pid: pid}, nil
}
func (p *parentProcess) Close() error { return nil }
func (p *parentProcess) Wait(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processIsRunning(p.pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("previous Smart Stage process did not exit; update was not applied")
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
	actual, err := os.Readlink("/proc/" + itoa(pid) + "/exe")
	if err != nil {
		return err
	}
	if !samePath(actual, expected) {
		return errors.New("candidate PID belongs to a different executable")
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
func itoa(pid int) string {
	if pid == 0 {
		return "0"
	}
	b := make([]byte, 0, 12)
	for pid > 0 {
		b = append([]byte{byte('0' + pid%10)}, b...)
		pid /= 10
	}
	return string(b)
}
func restartCommand(target Target, args []string) *exec.Cmd {
	return exec.Command(target.Path, args...)
}
func verifyNativeSignature(context.Context, Target, string) error {
	return errors.New("unsupported update platform")
}
func refreshFirewall(Target, *log.Logger) string { return "" }
