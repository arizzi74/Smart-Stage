//go:build windows

package cloudflared

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) func() {
	// CREATE_NO_WINDOW also prevents a terminal flash for the console helper
	// when SmartStage is built with the Windows GUI subsystem.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return func() {}
}
