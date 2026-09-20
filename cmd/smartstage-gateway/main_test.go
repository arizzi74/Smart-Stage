//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"smartstage/internal/gateway"
)

func TestInitializeCreatesPrivateConfigWithoutReplacingSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "config.json")
	if err := initialize(path, "https://example.com/smartstage/", "127.0.0.1:8790"); err != nil {
		t.Fatal(err)
	}
	cfg, err := gateway.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://example.com/smartstage" || len(cfg.Token) != 64 {
		t.Fatal("initial configuration invalid")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("registration token is not private")
	}
	if err := initialize(path, "https://other.example/smartstage", "127.0.0.1:8790"); err == nil {
		t.Fatal("existing registration token overwritten")
	}
	after, err := gateway.LoadConfig(path)
	if err != nil || after.Token != cfg.Token || after.PublicURL != cfg.PublicURL {
		t.Fatal("initialization changed existing settings")
	}
}
