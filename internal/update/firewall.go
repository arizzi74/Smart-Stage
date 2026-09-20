package update

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
)

// lanFirewallEnabled deliberately defaults to false. Missing, invalid, or
// unreadable settings must never cause an unsolicited administrator prompt.
func lanFirewallEnabled(configDir string) bool {
	path := filepath.Join(configDir, "gateway.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var settings struct {
		Mode string `json:"mode"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, 64*1024+1))
	if decoder.Decode(&settings) != nil || settings.Mode != "lan" {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

func refreshFirewall(target Target, configDir string, logger *log.Logger) string {
	if !lanFirewallEnabled(configDir) {
		logger.Printf("Public gateway mode: incoming-connection firewall setup is not needed")
		return ""
	}
	return ConfigureLANFirewall(target, logger)
}

// ConfigureLANFirewall refreshes only this application's incoming allowance.
// Call only after the user explicitly selects local network remote control.
func ConfigureLANFirewall(target Target, logger *log.Logger) string {
	if logger == nil {
		logger = log.Default()
	}
	return configureLANFirewall(target, logger)
}
