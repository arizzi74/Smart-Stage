# Install for the current Windows user. Run in a normal PowerShell window:
# irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
# Everything is defined before invocation, so a truncated response cannot start installation.
function Install-SmartStage {
    [CmdletBinding()]
    param()
    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version 2.0
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'This installer requires Windows.' }
    if ($PSVersionTable.PSVersion -lt [Version]'5.1') { throw 'PowerShell 5.1 or newer is required.' }

    function Get-NativeArchitecture {
        if (-not ('SmartStageInstaller.Native' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace SmartStageInstaller {
    public static class Native {
        [DllImport("kernel32.dll", SetLastError = true)]
        static extern bool IsWow64Process2(IntPtr process, out ushort processMachine, out ushort nativeMachine);
        public static ushort Architecture() {
            ushort processMachine, nativeMachine;
            if (!IsWow64Process2(new IntPtr(-1), out processMachine, out nativeMachine))
                throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error());
            return nativeMachine;
        }
    }
}
'@
        }
        # Unlike GetNativeSystemInfo, this reports ARM64 even from emulated x86/x64 PowerShell.
        # Windows 10 version 1709 or newer is required.
        switch ([SmartStageInstaller.Native]::Architecture()) {
            34404 { return 'amd64' }
            43620 { return 'arm64' }
            default { throw 'Smart Stage requires 64-bit Intel/AMD or ARM64 Windows.' }
        }
    }

    function Assert-PlainPath([string]$Path) {
        $item = [IO.Path]::GetFullPath($Path)
        while ($item) {
            if (Test-Path -LiteralPath $item) {
                if ((Get-Item -Force -LiteralPath $item).Attributes -band [IO.FileAttributes]::ReparsePoint) {
                    throw "Refusing a symbolic link or junction in the installation path: $item"
                }
            }
            $item = [IO.Path]::GetDirectoryName($item)
        }
    }

    function Assert-NotRunning([string]$Executable) {
        foreach ($process in @(Get-Process -Name smartstage -ErrorAction SilentlyContinue)) {
            try { $path = $process.MainModule.FileName } catch { throw 'Smart Stage may be running. Quit Smart Stage before installing.' }
            if (-not $path -or [string]::Equals($path, $Executable, [StringComparison]::OrdinalIgnoreCase)) {
                throw 'Smart Stage is running. Choose Quit Smart Stage in Admin, then run this installer again.'
            }
        }
        if (Test-Path -LiteralPath $Executable) {
            try { $handle = [IO.File]::Open($Executable, 'Open', 'ReadWrite', 'None'); $handle.Dispose() }
            catch { throw 'Smart Stage is in use or is not writable. Quit Smart Stage and retry.' }
        }
    }

    function Receive-InstallerFile([string]$Uri, [string]$Destination) {
        # URLs are constructed below from fixed GitHub/Microsoft origins, never environment input.
        Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination -TimeoutSec 180 -MaximumRedirection 10
    }

    function Get-WebView2Version {
        # https://learn.microsoft.com/microsoft-edge/webview2/concepts/distribution
        $keyName = 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
        foreach ($hive in @([Microsoft.Win32.RegistryHive]::CurrentUser, [Microsoft.Win32.RegistryHive]::LocalMachine)) {
            foreach ($view in @([Microsoft.Win32.RegistryView]::Registry32, [Microsoft.Win32.RegistryView]::Registry64)) {
                $base = $null; $key = $null
                try {
                    $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey($hive, $view)
                    $key = $base.OpenSubKey($keyName)
                    if ($key) {
                        $version = $null
                        if ([Version]::TryParse([string]$key.GetValue('pv'), [ref]$version) -and $version -gt [Version]'0.0.0.0') {
                            return $version.ToString()
                        }
                    }
                } finally { if ($key) { $key.Dispose() }; if ($base) { $base.Dispose() } }
            }
        }
        return $null
    }

    function Ensure-WebView2([string]$Work) {
        $runtime = Get-WebView2Version
        if ($runtime) { Write-Host "WebView2 Runtime $runtime is available."; return }
        Write-Host 'Installing Microsoft WebView2 for the dedicated Smart Stage window...'
        $bootstrap = Join-Path $Work 'MicrosoftEdgeWebview2Setup.exe'
        # Official Microsoft evergreen bootstrapper; it selects the native device architecture.
        Receive-InstallerFile 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' $bootstrap
        $signature = Get-AuthenticodeSignature -LiteralPath $bootstrap
        if ($signature.Status -ne 'Valid' -or -not $signature.SignerCertificate -or
            $signature.SignerCertificate.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false) -ne 'Microsoft Corporation') {
            throw 'The WebView2 installer does not have a valid Microsoft Corporation signature. Nothing was executed.'
        }
        # No RunAs/elevation. Microsoft installs per-user from a normal PowerShell window.
        $setup = Start-Process -FilePath $bootstrap -ArgumentList '/silent', '/install' -WindowStyle Hidden -PassThru
        if (-not $setup.WaitForExit(240000)) { throw 'WebView2 installation is still running. Let it finish, then run this installer again.' }
        if ($setup.ExitCode -ne 0 -and $setup.ExitCode -ne 3010) { throw "Microsoft WebView2 setup failed (exit $($setup.ExitCode))." }
        for ($attempt = 0; $attempt -lt 20; $attempt++) {
            $runtime = Get-WebView2Version
            if ($runtime) { Write-Host "WebView2 Runtime $runtime installed."; return }
            Start-Sleep -Milliseconds 500
        }
        throw 'WebView2 was not registered. Restart Windows if requested by Microsoft, then run this installer again.'
    }

    function Assert-Executable([string]$Path, [string]$Architecture) {
        $file = [IO.File]::OpenRead($Path)
        $reader = New-Object IO.BinaryReader($file)
        try {
            if ($file.Length -lt 512 -or $reader.ReadUInt16() -ne 0x5a4d) { throw 'The downloaded executable is not a Windows PE file.' }
            $file.Position = 0x3c
            $offset = $reader.ReadUInt32()
            if ($offset -gt $file.Length - 94) { throw 'The downloaded PE header is invalid.' }
            $file.Position = $offset
            if ($reader.ReadUInt32() -ne 0x4550) { throw 'The downloaded PE signature is invalid.' }
            $machine = $reader.ReadUInt16()
            $expected = 0x8664
            if ($Architecture -eq 'arm64') { $expected = 0xaa64 }
            if ($machine -ne $expected) { throw 'The downloaded executable has the wrong CPU architecture.' }
            $file.Position = $offset + 24
            if ($reader.ReadUInt16() -ne 0x20b) { throw 'The downloaded executable is not 64-bit.' }
        } finally { $reader.Dispose(); $file.Dispose() }
    }

    function Get-ExecutableVersion([string]$Path) {
        $start = New-Object Diagnostics.ProcessStartInfo
        $start.FileName = $Path
        $start.Arguments = '--version'
        $start.UseShellExecute = $false
        $start.CreateNoWindow = $true
        $start.RedirectStandardOutput = $true
        $start.RedirectStandardError = $true
        $process = [Diagnostics.Process]::Start($start)
        try {
            if (-not $process.WaitForExit(15000)) { $process.Kill(); throw 'The executable version check timed out.' }
            $output = $process.StandardOutput.ReadToEnd().Trim()
            if ($process.ExitCode -ne 0 -or $output -notmatch '^Smart Stage v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.]+)? \(.+\), .+ windows/(?:amd64|arm64)$') {
                throw 'The executable did not report a recognized Smart Stage release.'
            }
            return $output
        } finally { $process.Dispose() }
    }

    $version = 'v0.1.0-preview.16'
    if ($env:SMARTSTAGE_VERSION) { $version = $env:SMARTSTAGE_VERSION }
    if ($version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.]+)?$') { throw 'SMARTSTAGE_VERSION must be a release version, for example v0.1.0-preview.16.' }
    $architecture = Get-NativeArchitecture
    $parent = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs'
    if ($env:SMARTSTAGE_INSTALL_DIR) { $parent = $env:SMARTSTAGE_INSTALL_DIR }
    if (-not [IO.Path]::IsPathRooted($parent)) { throw 'SMARTSTAGE_INSTALL_DIR must be an absolute directory.' }
    $parent = [IO.Path]::GetFullPath($parent)
    $directory = Join-Path $parent 'SmartStage'
    $executable = Join-Path $directory 'smartstage.exe'
    Assert-PlainPath $directory
    Assert-PlainPath $executable
    Assert-NotRunning $executable
    if (Test-Path -LiteralPath $executable) { $null = Get-ExecutableVersion $executable }
    [IO.Directory]::CreateDirectory($directory) | Out-Null
    $lock = $null; $work = $null; $installed = $false; $committed = $false
    $backup = $null
    $shortcuts = @()
    $oldTLS = [Net.ServicePointManager]::SecurityProtocol
    try {
        try { $lock = [IO.File]::Open((Join-Path $directory '.install.lock'), 'OpenOrCreate', 'ReadWrite', 'None') }
        catch { throw 'Another Smart Stage installation is running, or the installation directory is not writable.' }
        $work = Join-Path $directory ('.install-' + [Guid]::NewGuid().ToString('N'))
        [IO.Directory]::CreateDirectory($work) | Out-Null
        [Net.ServicePointManager]::SecurityProtocol = $oldTLS -bor [Net.SecurityProtocolType]::Tls12
        $asset = "smartstage-windows-$architecture.zip"
        $url = "https://github.com/arizzi74/Smart-Stage/releases/download/$version/$asset"
        $archivePath = Join-Path $work $asset
        $checksumPath = "$archivePath.sha256"
        Write-Host "Downloading Smart Stage $version for Windows $architecture..."
        Receive-InstallerFile $url $archivePath
        Receive-InstallerFile "$url.sha256" $checksumPath
        $checksum = [IO.File]::ReadAllText($checksumPath).Trim()
        $pattern = '^([0-9a-fA-F]{64})\s+\*?' + [Regex]::Escape($asset) + '$'
        if ($checksum -notmatch $pattern) { throw 'The release checksum file is invalid.' }
        $expectedHash = $Matches[1]
        if ((Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash -ne $expectedHash) { throw 'The release ZIP checksum does not match. Nothing was installed.' }
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $archive = [IO.Compression.ZipFile]::OpenRead($archivePath)
        $staged = Join-Path $work 'smartstage.exe'
        try {
            if ($archive.Entries.Count -ne 1 -or $archive.Entries[0].FullName -cne 'smartstage.exe') {
                throw 'The release ZIP must contain exactly one smartstage.exe file.'
            }
            $entry = $archive.Entries[0]
            $unixType = ($entry.ExternalAttributes -shr 16) -band 0xf000
            if (($unixType -ne 0 -and $unixType -ne 0x8000) -or $entry.Length -lt 512 -or $entry.Length -gt 268435456) {
                throw 'The release ZIP executable entry is invalid.'
            }
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $staged, $false)
        } finally { $archive.Dispose() }
        Assert-Executable $staged $architecture
        $reportedVersion = Get-ExecutableVersion $staged
        if ($reportedVersion -notmatch ('^Smart Stage ' + [Regex]::Escape($version) + ' ') -or -not $reportedVersion.EndsWith("windows/$architecture")) {
            throw 'The release executable reports an unexpected version or architecture.'
        }
        $binaryHash = (Get-FileHash -LiteralPath $staged -Algorithm SHA256).Hash
        Ensure-WebView2 $work

        $shell = New-Object -ComObject WScript.Shell
        foreach ($folder in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('DesktopDirectory'))) {
            if (-not $folder) { throw 'The current user has no Start menu or Desktop folder.' }
            [IO.Directory]::CreateDirectory($folder) | Out-Null
            $link = Join-Path $folder 'Smart Stage.lnk'
            Assert-PlainPath $link
            $previous = $null
            if (Test-Path -LiteralPath $link) {
                if (-not [string]::Equals($shell.CreateShortcut($link).TargetPath, $executable, [StringComparison]::OrdinalIgnoreCase)) {
                    throw "An unrelated shortcut already exists: $link. Rename it before installing."
                }
                $previous = [IO.File]::ReadAllBytes($link)
            }
            $shortcuts += [PSCustomObject]@{ Path = $link; Previous = $previous; Written = $false }
        }
        Assert-PlainPath $executable
        Assert-NotRunning $executable
        if (Test-Path -LiteralPath $executable) {
            $backup = Join-Path $work 'previous.exe'
            [IO.File]::Replace($staged, $executable, $backup)
        } else { [IO.File]::Move($staged, $executable) }
        $installed = $true
        if ((Get-FileHash -LiteralPath $executable -Algorithm SHA256).Hash -ne $binaryHash) { throw 'The installed executable differs from the verified release.' }
        foreach ($record in $shortcuts) {
            $record.Written = $true
            $shortcut = $shell.CreateShortcut($record.Path)
            $shortcut.TargetPath = $executable
            $shortcut.WorkingDirectory = $directory
            $shortcut.IconLocation = "$executable,0"
            $shortcut.Description = 'Smart Stage show control'
            $shortcut.Arguments = ''
            $shortcut.WindowStyle = 1
            $shortcut.Save()
        }
        $committed = $true
        Write-Host "Installed $reportedVersion"
        Write-Host "Application: $executable"
        Write-Host 'Desktop and Start menu shortcuts are ready. Your settings and media are preserved.'
        Write-Host 'Public gateway access needs no administrator approval or incoming firewall rule.'
        $gatewayConfig = Join-Path ([Environment]::GetFolderPath('ApplicationData')) 'SmartStage\gateway.json'
        if (Test-Path -LiteralPath $gatewayConfig) {
            try {
                $settings = [IO.File]::ReadAllText($gatewayConfig) | ConvertFrom-Json
                if ($settings.mode -eq 'lan') { Write-Host 'Local network access is selected: allow Smart Stage on your private network in Windows Firewall if requested by the app.' }
            } catch { Write-Warning 'Remote access settings could not be read; review Remote control in Admin.' }
        }
        if ($env:SMARTSTAGE_NO_LAUNCH -ne '1') {
            Start-Process -FilePath $executable -WorkingDirectory $directory -WindowStyle Hidden
        }
    } catch {
        $failure = $_
        if (-not $committed) {
            foreach ($record in $shortcuts) {
                if ($record.Written) {
                    if ($null -ne $record.Previous) { [IO.File]::WriteAllBytes($record.Path, $record.Previous) }
                    elseif (Test-Path -LiteralPath $record.Path) { Remove-Item -LiteralPath $record.Path -Force }
                }
            }
            if ($installed) {
                if ($backup -and (Test-Path -LiteralPath $backup)) {
                    # PowerShell 5.1 binds $null to an empty String here, which
                    # File.Replace rejects. Keep the failed candidate in the
                    # private work directory while restoring the previous file.
                    $failedCandidate = Join-Path $work 'failed-install.exe'
                    [IO.File]::Replace($backup, $executable, $failedCandidate)
                }
                elseif (Test-Path -LiteralPath $executable) { Remove-Item -LiteralPath $executable -Force }
            }
        }
        throw $failure
    } finally {
        [Net.ServicePointManager]::SecurityProtocol = $oldTLS
        if ($backup -and (Test-Path -LiteralPath $backup) -and -not $committed) {
            Write-Warning "Installation could not complete rollback. Your previous executable is preserved at $backup."
        } elseif ($work -and (Test-Path -LiteralPath $work)) { Remove-Item -LiteralPath $work -Recurse -Force }
        if ($lock) { $lock.Dispose() }
    }
}

Install-SmartStage
