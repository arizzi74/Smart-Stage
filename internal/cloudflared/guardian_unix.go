//go:build !windows

package cloudflared

import (
	"os/signal"
	"syscall"
)

func ignoreGuardianBrokenPipe() {
	// The guardian must finish cleanup when its parent's stdout pipe disappears
	// concurrently with a readiness message. Default SIGPIPE would skip defers.
	signal.Ignore(syscall.SIGPIPE)
}
