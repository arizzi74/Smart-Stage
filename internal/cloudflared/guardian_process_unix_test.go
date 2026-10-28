//go:build !windows

package cloudflared

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func guardianTestProcessExited(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return true
	}
	// Minimal Linux containers may not reap adopted orphans promptly. Zombies
	// have exited and cannot retain a tunnel or open executable/runtime files.
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if _, rest, ok := strings.Cut(string(data), ") "); ok {
			return strings.HasPrefix(rest, "Z ")
		}
	}
	return false
}
