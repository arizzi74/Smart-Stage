//go:build windows

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopLoggingPreservesParentPipes(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	previousOut, previousErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	defer func() { os.Stdout, os.Stderr = previousOut, previousErr }()
	dir := t.TempDir()
	cleanup, err := prepareDesktopLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	prepareVersionOutput()
	if os.Stdout != writer || os.Stderr != writer {
		t.Fatal("GUI startup replaced a parent's redirected standard stream")
	}
	cleanup()
	if _, err := writer.WriteString("still owned by parent"); err != nil {
		t.Fatal("GUI cleanup closed a parent stream:", err)
	}
	writer.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "still owned by parent" {
		t.Fatalf("redirected output changed: %q (%v)", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "smartstage.log")); !os.IsNotExist(err) {
		t.Fatal("redirected invocation unnecessarily created a GUI log")
	}
}

func TestDesktopLoggingCapturesGUIOutputWithoutConsole(t *testing.T) {
	dir := t.TempDir()
	missing, err := os.Create(filepath.Join(dir, "closed-handle"))
	if err != nil {
		t.Fatal(err)
	}
	missing.Close()
	previousOut, previousErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = missing, missing
	defer func() { os.Stdout, os.Stderr = previousOut, previousErr }()
	config := filepath.Join(dir, "Café's Smart Stage")
	cleanup, err := prepareDesktopLog(config)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := os.Stdout.WriteString("Admin ready\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stderr.WriteString("diagnostic retained\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(config, "smartstage.log"))
	if err != nil || !strings.Contains(string(data), "Admin ready\n") || !strings.Contains(string(data), "diagnostic retained\n") {
		t.Fatalf("GUI launch lost its output: %q (%v)", data, err)
	}
}
