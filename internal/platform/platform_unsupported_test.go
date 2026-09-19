//go:build (!windows && !darwin) || !cgo

package platform

import (
	"testing"

	"smartstage/internal/playback"
)

func TestUnsupportedBuildCannotStartWithASimulatedBackend(t *testing.T) {
	called := false
	err := Run(func(playback.Backend) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("unsupported native initialization: err=%v applicationStarted=%v", err, called)
	}
}
