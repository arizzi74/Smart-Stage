# Smart Stage Windows installer. Runs in the current user's profile without elevation.
& {
    $ErrorActionPreference = 'Stop'
    $version = if ($env:SMARTSTAGE_VERSION) { $env:SMARTSTAGE_VERSION } else { 'v0.1.0-preview.2' }
    if ($version -notmatch '^[a-zA-Z0-9._-]+$') { throw 'Invalid SMARTSTAGE_VERSION' }
    $nativeArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($nativeArch.ToUpperInvariant()) {
        'ARM64' { 'arm64' }
        'AMD64' { 'amd64' }
        default { throw 'Smart Stage requires 64-bit Windows on AMD64 or ARM64.' }
    }
    $installDir = if ($env:SMARTSTAGE_INSTALL_DIR) { $env:SMARTSTAGE_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'SmartStage\bin' }
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    $tempDir = Join-Path $installDir ('.install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    try {
        $asset = "smartstage-windows-$arch.exe"
        $base = "https://github.com/arizzi74/Smart-Stage/releases/download/$version"
        $download = Join-Path $tempDir $asset
        Write-Host "Downloading Smart Stage $version for Windows $arch..."
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile $download
        $checksumFile = Join-Path $tempDir 'checksum.txt'
        Invoke-WebRequest -UseBasicParsing "$base/$asset.sha256" -OutFile $checksumFile
        $checksum = Get-Content -LiteralPath $checksumFile -Raw
        $expected = ($checksum.Trim() -split '\s+')[0].ToLowerInvariant()
        $actual = (Get-FileHash -Algorithm SHA256 $download).Hash.ToLowerInvariant()
        if ($expected -notmatch '^[0-9a-f]{64}$' -or $actual -ne $expected) { throw 'Checksum mismatch; nothing was installed.' }
        $destination = Join-Path $installDir 'smartstage.exe'
        if (Test-Path -LiteralPath $destination) {
            [IO.File]::Replace($download, $destination, (Join-Path $tempDir 'previous.exe'))
        } else { [IO.File]::Move($download, $destination) }
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (($userPath -split ';') -notcontains $installDir) {
            [Environment]::SetEnvironmentVariable('Path', ($installDir + ';' + $userPath), 'User')
        }
        if (($env:Path -split ';') -notcontains $installDir) { $env:Path = $installDir + ';' + $env:Path }
        & $destination --version
        if ($LASTEXITCODE -ne 0) { throw 'The executable was installed but could not start. Check Windows multimedia components and architecture.' }
        Write-Host "Installed: $destination"
        Write-Host 'Run: smartstage'
        Write-Host 'Preview: physical routing, clean-machine acceptance and the two-hour soak remain unverified.'
        Write-Host 'The app prints Admin/Command URLs and separate pairing keys when started.'
    } finally { Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue }
}
