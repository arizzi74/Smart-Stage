//go:build !windows

package cloudflared

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func configureProcess(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var mu sync.Mutex
	var forceKill *time.Timer
	finished := false
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if finished || cmd.Process == nil {
			return os.ErrProcessDone
		}
		pid := cmd.Process.Pid
		// Give cloudflared time to forward origin WebSocket closure and
		// unregister its edge connection before the hard process deadline.
		err := syscall.Kill(-pid, syscall.SIGTERM)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		if err == nil {
			forceKill = time.AfterFunc(1500*time.Millisecond, func() {
				mu.Lock()
				defer mu.Unlock()
				if !finished {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
		}
		return err
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		finished = true
		if forceKill != nil {
			forceKill.Stop()
		}
	}
}
