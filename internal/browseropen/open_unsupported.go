//go:build !darwin && !windows

package browseropen

import "errors"

func open(string) error { return errors.New("system browser launch is available on macOS and Windows") }
