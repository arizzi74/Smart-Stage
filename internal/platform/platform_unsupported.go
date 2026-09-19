//go:build (!windows && !darwin) || !cgo

package platform

import (
	"fmt"
	"runtime"
	"smartstage/internal/playback"
)

func Run(func(playback.Backend) error) error {
	return fmt.Errorf("native playback requires Windows or macOS with cgo enabled; this build is %s/%s (no simulated backend is available)", runtime.GOOS, runtime.GOARCH)
}
