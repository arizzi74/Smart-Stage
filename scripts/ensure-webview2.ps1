# Native CI prerequisite. Reuse the self-contained public installer's exact
# runtime detection, official download, and Authenticode checks, without
# invoking Install-SmartStage or installing/changing Smart Stage itself.
$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'WebView2 requires Windows.' }
$tokens = $null
$errors = $null
$path = Join-Path $PSScriptRoot '../install.ps1'
$ast = [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw "Cannot parse installer: $errors" }
foreach ($name in @('Receive-InstallerFile', 'Get-WebView2Version', 'Ensure-WebView2')) {
    $functions = @($ast.FindAll({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name
    }, $true))
    if ($functions.Count -ne 1) { throw "Expected exactly one installer function: $name" }
    . ([ScriptBlock]::Create($functions[0].Extent.Text))
}
$work = Join-Path ([IO.Path]::GetTempPath()) ('smartstage-webview2-' + [Guid]::NewGuid().ToString('N'))
$oldTLS = [Net.ServicePointManager]::SecurityProtocol
try {
    [IO.Directory]::CreateDirectory($work) | Out-Null
    [Net.ServicePointManager]::SecurityProtocol = $oldTLS -bor [Net.SecurityProtocolType]::Tls12
    Ensure-WebView2 $work
    [ordered]@{ runtime = 'Microsoft Edge WebView2 Evergreen'; version = (Get-WebView2Version); installer = $path } | ConvertTo-Json
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $oldTLS
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
}
