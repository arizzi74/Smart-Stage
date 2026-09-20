package update

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(bytes[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func windowsFirewallScript(program string) string {
	// A PowerShell single-quoted literal treats $, backticks, and parentheses
	// literally. Doubling apostrophes is its only required escape.
	quoted := "'" + strings.ReplaceAll(program, "'", "''") + "'"
	digest := sha256.Sum256([]byte(strings.ToLower(program)))
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSHOME 'Modules\NetSecurity\NetSecurity.psd1') -ErrorAction Stop
$program = %s
$name = 'SmartStage-LAN-%x'
$existing = Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue
if ($existing) { $existing | Remove-NetFirewallRule }
New-NetFirewallRule -Name $name -DisplayName 'Smart Stage local network remote control' -Description 'Allow this Smart Stage executable on trusted private networks only.' -Program $program -Direction Inbound -Action Allow -Enabled True -Profile Private -Protocol TCP -RemoteAddress LocalSubnet -EdgeTraversalPolicy Block | Out-Null
$rule = Get-NetFirewallRule -PolicyStore ActiveStore -Name $name
$app = $rule | Get-NetFirewallApplicationFilter
$port = $rule | Get-NetFirewallPortFilter
$address = $rule | Get-NetFirewallAddressFilter
if ($app.Program -ne $program -or $rule.Enabled -ne 'True' -or $rule.Direction -ne 'Inbound' -or $rule.Action -ne 'Allow' -or $rule.Profile -ne 'Private' -or $port.Protocol -notin @('TCP', '6') -or @($address.RemoteAddress).Count -ne 1 -or $address.RemoteAddress -ne 'LocalSubnet') { throw 'The Smart Stage firewall rule could not be verified.' }
`, quoted, digest[:12])
}

func windowsFirewallElevation(script string) string {
	return `$ErrorActionPreference = 'Stop'
try {
  $process = Start-Process -FilePath "$PSHOME\powershell.exe" -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand ` + encodePowerShell(script) + `'
  exit $process.ExitCode
} catch { Write-Error 'Smart Stage firewall approval was cancelled or denied.'; exit 1 }
`
}
