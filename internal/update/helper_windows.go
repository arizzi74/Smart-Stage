//go:build windows

package update

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func windowsPowerShellPath() (string, error) {
	buffer := make([]uint16, 32768)
	getSystemDirectory := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW")
	n, _, err := getSystemDirectory.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", fmt.Errorf("locate Windows system directory: %v", err)
	}
	return filepath.Join(syscall.UTF16ToString(buffer[:n]), "WindowsPowerShell", "v1.0", "powershell.exe"), nil
}

func canonicalWindowsFirewallProgram(program string) (string, error) {
	if !filepath.IsAbs(program) {
		return "", errors.New("executable path is not absolute")
	}
	// Expand filesystem aliases, including Windows 8.3 directory names, before
	// passing the executable to the firewall's application-path resolver.
	canonical, err := filepath.EvalSymlinks(program)
	if err != nil {
		return "", err
	}
	input, err := syscall.UTF16PtrFromString(canonical)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 32768)
	getLongPathName := syscall.NewLazyDLL("kernel32.dll").NewProc("GetLongPathNameW")
	n, _, callErr := getLongPathName.Call(uintptr(unsafe.Pointer(input)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", fmt.Errorf("expand executable path: %v", callErr)
	}
	canonical = syscall.UTF16ToString(buffer[:n])
	if strings.HasPrefix(canonical, `\\?\UNC\`) {
		canonical = `\\` + strings.TrimPrefix(canonical, `\\?\UNC\`)
	} else if strings.HasPrefix(canonical, `\\?\`) && len(canonical) > 6 && canonical[5] == ':' && canonical[6] == '\\' {
		canonical = strings.TrimPrefix(canonical, `\\?\`)
	}
	if !filepath.IsAbs(canonical) {
		return "", errors.New("resolved executable path is not absolute")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("executable is not a regular file")
	}
	return canonical, nil
}

func configureLANFirewall(target Target, logger *log.Logger) string {
	if os.Getenv("SMARTSTAGE_SKIP_FIREWALL") == "1" {
		logger.Printf("Firewall setup skipped (SMARTSTAGE_SKIP_FIREWALL=1)")
		return "Firewall setup was explicitly skipped; no incoming-connection allowance was verified."
	}
	program, err := canonicalWindowsFirewallProgram(corePath(target))
	if err != nil {
		logger.Printf("Windows firewall executable path could not be resolved: %v", err)
		return "Smart Stage could not resolve its executable path for the firewall rule. Move the executable to a local folder and try again."
	}
	logger.Printf("Requesting Windows administrator approval for this Smart Stage executable on private local networks: %s", program)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	powershell, err := windowsPowerShellPath()
	if err != nil {
		logger.Printf("Windows firewall setup unavailable: %v", err)
		return "Smart Stage could not locate Windows PowerShell to configure its incoming-connection rule."
	}
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(windowsFirewallElevation(windowsFirewallScript(program))))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("Windows firewall approval failed: %v: %s", err, strings.TrimSpace(string(output)))
		return "Windows firewall approval was cancelled, denied, or could not be verified. Allow Smart Stage on your trusted private network in Windows Security > Firewall & network protection."
	}
	logger.Printf("Verified scoped Windows firewall allowance for private local networks")
	return ""
}
