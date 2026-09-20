package update

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestFirewallRequiresExplicitSavedLANMode(t *testing.T) {
	for _, tc := range []struct {
		name, contents string
		want           bool
	}{
		{"missing", "", false},
		{"empty settings", `{}`, false},
		{"gateway", `{"mode":"gateway","token":"secret"}`, false},
		{"explicit LAN", `{"mode":"lan","url":"https://example.com/smartstage"}`, true},
		{"invalid mode", `{"mode":"LAN"}`, false},
		{"truncated", `{"mode":"lan"`, false},
		{"trailing content", `{"mode":"lan"} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.contents != "" {
				if err := os.WriteFile(filepath.Join(dir, "gateway.json"), []byte(tc.contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := lanFirewallEnabled(dir); got != tc.want {
				t.Fatalf("firewall enabled = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWindowsFirewallScriptQuotesPathAndScopesRule(t *testing.T) {
	path := `C:\Users\Café's $(Write-Error injected)\Smart Stage.exe`
	script := windowsFirewallScript(path)
	if !strings.Contains(script, "$program = 'C:\\Users\\Café''s $(Write-Error injected)\\Smart Stage.exe'") {
		t.Fatal("executable path was not a literal PowerShell string")
	}
	for _, scope := range []string{"-Program $program", "-Direction Inbound", "-Profile Private", "-Protocol TCP", "-RemoteAddress LocalSubnet", "-EdgeTraversalPolicy Block"} {
		if !strings.Contains(script, scope) {
			t.Fatalf("missing firewall scope %s", scope)
		}
	}
	if strings.Contains(script, "Set-NetFirewallProfile") {
		t.Fatal("firewall setup must not modify global firewall settings")
	}
	encoded := encodePowerShell(script)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2*i:])
	}
	if string(utf16.Decode(units)) != script {
		t.Fatal("Unicode PowerShell script changed during encoding")
	}
	if launcher := windowsFirewallElevation(script); !strings.Contains(launcher, "-Verb RunAs") || strings.Contains(launcher, path) || !strings.Contains(launcher, encoded) {
		t.Fatal("elevation must pass only the encoded script to the privileged process")
	}
}
