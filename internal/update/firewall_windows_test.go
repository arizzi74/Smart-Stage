//go:build windows

package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The CI opt-in invokes only the rule payload using the runner's existing
// administrator account. It does not claim to exercise an interactive UAC UI.
func TestWindowsLANFirewallRuleScoped(t *testing.T) {
	if os.Getenv("SMARTSTAGE_TEST_FIREWALL") != "1" {
		t.Skip("requires an administrator Windows runner and SMARTSTAGE_TEST_FIREWALL=1")
	}
	program := filepath.Join(t.TempDir(), "Café's $(Write-Error injected).exe")
	if err := os.WriteFile(program, []byte("firewall path fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(strings.ToLower(program)))
	name := fmt.Sprintf("SmartStage-LAN-%x", digest[:12])
	powershell, err := windowsPowerShellPath()
	if err != nil {
		t.Fatal(err)
	}
	run := func(script string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script)).CombinedOutput()
	}
	t.Cleanup(func() {
		if output, err := run("$ErrorActionPreference='Stop'; Get-NetFirewallRule -Name '" + name + "' -ErrorAction SilentlyContinue | Remove-NetFirewallRule"); err != nil {
			t.Errorf("clean scoped firewall fixture: %v: %s", err, output)
		}
	})
	script := `$before = Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, DefaultOutboundAction | ConvertTo-Json -Compress
` + windowsFirewallScript(program) + `
$after = Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, DefaultOutboundAction | ConvertTo-Json -Compress
if ($before -ne $after) { throw 'Global firewall settings changed.' }
`
	if output, err := run(script); err != nil {
		t.Fatalf("configure and read back the scoped LAN rule: %v: %s", err, output)
	}
}
