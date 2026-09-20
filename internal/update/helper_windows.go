//go:build windows

package update

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000
const synchronize = 0x00100000

var queryFullProcessImageName = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")

type parentProcess struct{ handle syscall.Handle }

func openParent(pid int) (*parentProcess, error) {
	h, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return nil, fmt.Errorf("observe update parent: %w", err)
	}
	return &parentProcess{handle: h}, nil
}
func (p *parentProcess) Close() error { return syscall.CloseHandle(p.handle) }
func (p *parentProcess) Wait(timeout time.Duration) error {
	event, err := syscall.WaitForSingleObject(p.handle, uint32(timeout.Milliseconds()))
	if err != nil {
		return err
	}
	if event != syscall.WAIT_OBJECT_0 {
		return errors.New("previous Smart Stage process did not exit; update was not applied")
	}
	return nil
}
func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000200}
}
func processIsRunning(pid int) bool {
	if pid <= 1 {
		return false
	}
	h, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(h)
	event, err := syscall.WaitForSingleObject(h, 0)
	return err != nil || event == syscall.WAIT_TIMEOUT
}
func stopCandidate(pid int, expected string) error {
	h, err := syscall.OpenProcess(synchronize|processQueryLimitedInformation|syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if !processIsRunning(pid) {
			return nil
		}
		return err
	}
	defer syscall.CloseHandle(h)
	event, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		return err
	}
	if event == syscall.WAIT_OBJECT_0 {
		return nil
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, callErr := queryFullProcessImageName.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return callErr
	}
	if !samePath(syscall.UTF16ToString(buffer[:size]), expected) {
		return errors.New("candidate PID belongs to a different executable")
	}
	if err := syscall.TerminateProcess(h, 1); err != nil {
		return err
	}
	event, err = syscall.WaitForSingleObject(h, 10000)
	if err != nil {
		return err
	}
	if event != syscall.WAIT_OBJECT_0 {
		return errors.New("candidate process did not exit")
	}
	return nil
}
func restartCommand(target Target, args []string) *exec.Cmd {
	return exec.Command(target.Path, args...)
}

// Release executables currently have no Authenticode signature. The manager
// verifies the published SHA-256, and Stage validates PE/Go identity and version.
func verifyNativeSignature(context.Context, Target, string) error { return nil }
func refreshFirewall(Target, *log.Logger) string                  { return "" }
