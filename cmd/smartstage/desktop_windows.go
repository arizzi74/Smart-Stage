//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// GUI executables have no console when opened from Explorer. Preserve pipes
// supplied by a parent process (including verification and the updater), and
// send only missing standard streams to the per-user application log.
func usableStandardFile(file *os.File) bool {
	if file == nil {
		return false
	}
	kind, err := syscall.GetFileType(syscall.Handle(file.Fd()))
	return err == nil && kind != syscall.FILE_TYPE_UNKNOWN
}

func prepareDesktopLog(configDir string) (func(), error) {
	stdoutMissing, stderrMissing := !usableStandardFile(os.Stdout), !usableStandardFile(os.Stderr)
	if !stdoutMissing && !stderrMissing {
		return func() {}, nil
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("create Smart Stage log directory: %w", err)
	}
	path := filepath.Join(configDir, "smartstage.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 4<<20 {
		// Rotation is best effort; a simultaneous launch may still have the
		// previous log open. Never prevent app startup for a rotation failure.
		_ = os.Remove(path + ".previous")
		_ = os.Rename(path, path+".previous")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open Smart Stage log: %w", err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	if stdoutMissing {
		os.Stdout = file
	}
	if stderrMissing {
		os.Stderr = file
	}
	return func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = file.Close()
	}, nil
}

// Explicit CLI version queries may attach to their existing parent console;
// normal app launches never create or attach a terminal window.
func prepareVersionOutput() {
	if usableStandardFile(os.Stdout) {
		return
	}
	attach := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if ok, _, _ := attach.Call(uintptr(^uint32(0))); ok == 0 {
		return
	}
	if handle, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE); err == nil {
		os.Stdout = os.NewFile(uintptr(handle), "stdout")
	}
}
