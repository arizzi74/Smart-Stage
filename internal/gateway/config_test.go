package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateConfigAndTokenGeneration(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(first) || first == second {
		t.Fatal("tokens must be distinct 256-bit secrets")
	}
	config := Config{PublicURL: "https://example.com/smartstage", Token: first}
	data, _ := json.Marshal(config)
	path := filepath.Join(t.TempDir(), "gateway.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Token != first || loaded.Listen != "127.0.0.1:8790" {
		t.Fatal("private config did not load/default correctly")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0640); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err != nil {
			t.Fatal("dedicated service group cannot read config:", err)
		}
		for _, mode := range []os.FileMode{0644, 0660, 0604, 0700} {
			os.Chmod(path, mode)
			if _, err := LoadConfig(path); err == nil {
				t.Errorf("accepted unsafe mode %o", mode)
			}
		}
		os.Chmod(path, 0600)
	}
	for _, bad := range []string{string(data) + " {}", `{"token":"secret","unexpected":true}`, strings.Repeat("x", 17000)} {
		os.WriteFile(path, []byte(bad), 0600)
		if _, err := LoadConfig(path); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	if !strings.Contains(Licenses, "Permission to use") {
		t.Fatal("gateway binary is missing dependency license")
	}
}
