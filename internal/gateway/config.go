package gateway

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
)

//go:embed licenses.txt
var Licenses string

// LoadConfig reads a private service configuration. Mode 0640 supports a
// root-owned file readable by the dedicated, unprivileged service group.
func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, errors.New("could not open gateway configuration")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 || runtime.GOOS != "windows" && info.Mode().Perm()&0137 != 0 {
		return Config{}, errors.New("gateway configuration must be a regular private file (mode 0600 or 0640)")
	}
	dec := json.NewDecoder(io.LimitReader(f, 16<<10))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, errors.New("invalid gateway configuration JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("gateway configuration must contain one JSON object")
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8790"
	}
	server, err := NewServer(cfg)
	if err != nil {
		return Config{}, err
	}
	server.Close()
	return cfg, nil
}
