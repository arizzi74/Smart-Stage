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
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"smartstage.exe", "Café's Smart Stage.exe", "Café's $(Write-Error injected).exe"} {
		t.Run(name, func(t *testing.T) {
			program := filepath.Join(t.TempDir(), name)
			// Firewall application resolution must see a real executable, not
			// arbitrary text with an .exe extension.
			if err := copyFile(context.Background(), self, program, 0700); err != nil {
				t.Fatal(err)
			}
			verifyWindowsFirewallRule(t, program)
		})
	}
}

func verifyWindowsFirewallRule(t *testing.T, program string) {
	t.Helper()
	digest := sha256.Sum256([]byte(strings.ToLower(program)))
	name := fmt.Sprintf("SmartStage-LAN-%x", digest[:12])
	powershell, err := windowsPowerShellPath()
	if err != nil {
		t.Fatal(err)
	}
	run := func(script string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-OutputFormat", "Text", "-EncodedCommand", encodePowerShell("$ProgressPreference='SilentlyContinue'\n"+script)).CombinedOutput()
	}
	t.Cleanup(func() {
		if output, err := run("$ErrorActionPreference='Stop'; $rule=Get-NetFirewallRule -Name '" + name + "' -ErrorAction SilentlyContinue; if ($rule) { $rule | Remove-NetFirewallRule }; exit 0"); err != nil {
			t.Errorf("clean scoped firewall fixture: %v: %s", err, output)
		}
	})
	script := `$before = Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, DefaultOutboundAction | ConvertTo-Json -Compress
` + windowsFirewallScript(program) + `
$after = Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, DefaultOutboundAction | ConvertTo-Json -Compress
if ($before -ne $after) { throw 'Global firewall settings changed.' }
exit 0
`
	if output, err := run(script); err != nil {
		if filepath.Base(program) == "smartstage.exe" {
			// Keep the failing assertion, but isolate application resolution from
			// the scope parameters in the same CI run. These diagnostic rules are
			// disabled, blocking, and removed before the subprocess exits.
			diagnostic := "$testProgram = '" + strings.ReplaceAll(program, "'", "''") + "'\n" + `$ErrorActionPreference='Stop'
Import-Module (Join-Path $PSHOME 'Modules\NetSecurity\NetSecurity.psd1')
foreach ($kind in @('no-program-full-scope', 'system-powershell-full-scope', 'copied-program-minimal', 'copied-program-full-scope')) {
  $probeName = 'SmartStage-Diagnostic-' + [guid]::NewGuid().ToString('N')
  $parameters = @{Name=$probeName;DisplayName='Smart Stage disabled diagnostic';Direction='Inbound';Action='Block';Enabled='False'}
  if ($kind -ne 'copied-program-minimal') { $parameters.Profile='Private'; $parameters.Protocol='TCP'; $parameters.RemoteAddress='LocalSubnet'; $parameters.EdgeTraversalPolicy='Block' }
  if ($kind -eq 'system-powershell-full-scope') { $parameters.Program=Join-Path $PSHOME 'powershell.exe' }
  if ($kind -like 'copied-program-*') { $parameters.Program=$testProgram }
  try { New-NetFirewallRule @parameters | Out-Null; Write-Output ($kind + ': accepted') }
  catch { Write-Output ($kind + ': ' + $_.FullyQualifiedErrorId + ': ' + $_.Exception.Message) }
  finally { $probe=Get-NetFirewallRule -Name $probeName -ErrorAction SilentlyContinue; if ($probe) { $probe | Remove-NetFirewallRule } }
}
exit 0
`
			details, diagnosticErr := run(diagnostic)
			t.Logf("disabled firewall parameter probes: %v: %s", diagnosticErr, details)
		}
		t.Fatalf("configure and read back the scoped LAN rule for %q: %v: %s", filepath.Base(program), err, output)
	}
}
