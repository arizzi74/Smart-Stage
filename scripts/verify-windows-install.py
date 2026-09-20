#!/usr/bin/env python3
"""Exercise the Windows installer on an ephemeral native Windows runner.

Public mode uses the installer's real GitHub downloads. --archive adapts only
the download function in a private script copy to use a candidate release ZIP;
the production installer has no arbitrary download-origin override. Negative
fixtures likewise leave all validation/replacement/shortcut code intact.
"""
import argparse
import base64
import ctypes
from ctypes import wintypes
import hashlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shutil
import socket
import struct
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
DOWNLOAD_LINE = '        Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination -TimeoutSec 180 -MaximumRedirection 10'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def quote(value):
    return "'" + str(value).replace("'", "''") + "'"


def powershell(shell, command, env=None, timeout=360):
    command = "[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $ErrorActionPreference='Stop'; " + command
    encoded = base64.b64encode(command.encode('utf-16le')).decode()
    result = subprocess.run([shell, '-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
                             '-EncodedCommand', encoded], env=env, capture_output=True, timeout=timeout)
    return result.returncode, result.stdout.decode('utf-8', 'replace'), result.stderr.decode('utf-8', 'replace')


def psjson(shell, command):
    code, out, err = powershell(shell, command + ' | ConvertTo-Json -Compress -Depth 8')
    assert code == 0, (out, err)
    # An empty PowerShell pipeline emits no JSON. This is expected while the
    # asynchronously created WebView2 child process has not appeared yet.
    return json.loads(out) if out.strip() else None


def native_architecture():
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.IsWow64Process2.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.USHORT), ctypes.POINTER(wintypes.USHORT)]
    machine, native = wintypes.USHORT(), wintypes.USHORT()
    assert kernel.IsWow64Process2(wintypes.HANDLE(-1), ctypes.byref(machine), ctypes.byref(native)), ctypes.get_last_error()
    return {0x8664: 'amd64', 0xaa64: 'arm64'}[native.value]


def listening(port):
    with socket.socket() as probe:
        probe.settimeout(.4)
        return probe.connect_ex(('127.0.0.1', port)) == 0


def wait_process_exit(pid, timeout=30):
    """HTTP can close before STA/WebView teardown releases the executable."""
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
    kernel.WaitForSingleObject.restype = wintypes.DWORD
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    handle = kernel.OpenProcess(0x00100000, False, pid)
    if not handle:
        assert ctypes.get_last_error() == 87, f'Cannot observe installed process {pid}: {ctypes.get_last_error()}'
        return
    try:
        assert kernel.WaitForSingleObject(handle, timeout * 1000) == 0, 'Installed app did not exit after Admin Quit'
    finally:
        kernel.CloseHandle(handle)


def remove_fixture_tree(path):
    """Allow brief Windows file-release delays, but never report stale cleanup."""
    deadline = time.monotonic() + 10
    while path.exists():
        shutil.rmtree(path, ignore_errors=True)
        if not path.exists():
            return
        if time.monotonic() >= deadline:
            raise AssertionError(f'Installer verification could not remove its fixture directory: {path}')
        time.sleep(.2)


def until(check, description, timeout=60):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            value = check()
            if value:
                return value
        except (OSError, http.client.HTTPException):
            pass
        time.sleep(.2)
    raise AssertionError(description)


def load_network_helpers():
    spec = importlib.util.spec_from_file_location('smartstage_network_checks', ROOT / 'scripts/verify-network-mode.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def native_windows(pid):
    user = ctypes.WinDLL('user32', use_last_error=True)
    user.GetWindowThreadProcessId.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.DWORD)]
    user.IsWindowVisible.argtypes = [wintypes.HWND]
    user.GetWindowTextW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    user.GetClassNameW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    callback_type = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
    user.EnumWindows.argtypes = [callback_type, wintypes.LPARAM]
    windows = []

    @callback_type
    def observe(hwnd, _):
        owner = wintypes.DWORD()
        user.GetWindowThreadProcessId(hwnd, ctypes.byref(owner))
        if owner.value == pid and user.IsWindowVisible(hwnd):
            title, kind = ctypes.create_unicode_buffer(512), ctypes.create_unicode_buffer(256)
            user.GetWindowTextW(hwnd, title, len(title))
            user.GetClassNameW(hwnd, kind, len(kind))
            windows.append({'hwnd': int(hwnd), 'title': title.value, 'class': kind.value})
        return True

    user.EnumWindows(observe, 0)
    return windows


def post_escape(hwnd):
    """Deliver Escape to this app's HWND, without global keyboard injection."""
    user = ctypes.WinDLL('user32', use_last_error=True)
    user.PostMessageW.argtypes = [wintypes.HWND, wintypes.UINT, wintypes.WPARAM, wintypes.LPARAM]
    user.PostMessageW.restype = wintypes.BOOL
    # Repeat count one, Escape scan code one; key-up carries previous/up bits.
    for message, flags in ((0x0100, 0x00010001), (0x0101, 0xC0010001)):
        assert user.PostMessageW(hwnd, message, 0x1B, flags), f'Could not post native Escape: {ctypes.get_last_error()}'


def fixture_script(source, directory):
    assert source.count(DOWNLOAD_LINE) == 1
    adapter = (
        "        if ($Uri.StartsWith('https://github.com/arizzi74/Smart-Stage/releases/download/')) {\n"
        f"            Copy-Item -LiteralPath (Join-Path {quote(directory)} ([Uri]$Uri).Segments[-1]) -Destination $Destination; return\n"
        '        }\n' + DOWNLOAD_LINE)
    return source.replace(DOWNLOAD_LINE, adapter)


def development_fixture(source):
    """Allow only local dev artifacts; production still accepts release tags only."""
    guard = "if ($version -notmatch '^v[0-9]+\\.[0-9]+\\.[0-9]+(?:-[a-zA-Z0-9.]+)?$')"
    metadata = "'^Smart Stage v[0-9]+\\.[0-9]+\\.[0-9]+(?:-[a-zA-Z0-9.]+)? "
    assert source.count(guard) == 1 and source.count(metadata) == 1
    return source.replace(guard, "if ($version -ne 'dev')").replace(metadata, "'^Smart Stage dev ")


def write_archive(directory, arch, data=None, name='smartstage.exe', raw=None):
    directory.mkdir(parents=True, exist_ok=True)
    archive = directory / f'smartstage-windows-{arch}.zip'
    if raw is None:
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, 'w', compression=zipfile.ZIP_DEFLATED) as result:
            entry = zipfile.ZipInfo(name)
            entry.external_attr = 0o100755 << 16
            result.writestr(entry, data)
        raw = buffer.getvalue()
    archive.write_bytes(raw)
    archive.with_suffix('.zip.sha256').write_text(f'{digest(raw)}  {archive.name}\n')
    return archive


def install(shell, source, work, version, *, launch=False, failure=None, override=True, bootstrap_url=None):
    script = work / ('installer-' + str(time.time_ns()) + '.ps1')
    script.write_text(source, encoding='utf-8')
    env = os.environ.copy()
    env.pop('SMARTSTAGE_INSTALL_DIR', None)
    env.pop('SMARTSTAGE_VERSION', None)
    if override:
        env['SMARTSTAGE_VERSION'] = version
    if launch:
        env.pop('SMARTSTAGE_NO_LAUNCH', None)
    else:
        env['SMARTSTAGE_NO_LAUNCH'] = '1'
    command = 'Invoke-Expression ([IO.File]::ReadAllText(' + quote(script) + '))'
    if bootstrap_url:
        command = (
            f'$source = Invoke-RestMethod -Uri {quote(bootstrap_url)} -TimeoutSec 180; '
            "if ($source -isnot [string]) { throw 'The public irm bootstrap was not script text' }; "
            '$hash = [BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($source))).Replace("-", "").ToLowerInvariant(); '
            f'if ($hash -ne {quote(digest(source.encode()))}) {{ throw "Public irm bootstrap changed: $hash" }}; '
            'Invoke-Expression $source')
    result = powershell(shell, command, env=env)
    text = result[1] + '\n' + result[2]
    if failure:
        assert result[0] != 0 and failure.lower() in text.lower(), (failure, result)
    else:
        assert result[0] == 0, result
    return text


def firewall_snapshot(shell):
    return psjson(shell, "@(Get-NetFirewallRule -PolicyStore ActiveStore | Select-Object Name,Enabled,Direction,Action,Profile | Sort-Object Name)")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--arch', choices=('amd64', 'arm64'), required=True)
    parser.add_argument('--version', help='Release version; dev is permitted only with a local --archive fixture')
    parser.add_argument('--archive', type=Path, help='Candidate release ZIP (only downloads are adapted in a private script copy)')
    parser.add_argument('--powershell', default='powershell.exe')
    parser.add_argument('--bootstrap-url', help='Public install.ps1 URL; public-release mode defaults to raw GitHub main')
    parser.add_argument('--allow-legacy-browser', action='store_true', help='Only for checking old releases before the Windows app window')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    assert os.name == 'nt', 'This verification requires a real Windows runner'
    assert native_architecture() == args.arch, 'The verifier must run on the target native OS architecture'
    source = (ROOT / 'install.ps1').read_text(encoding='utf-8')
    default = re.search(r"^    \$version = '(v[^']+)'$", source, re.M)[1]
    version = args.version or default
    assert re.fullmatch(r'v\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?', version) or (version == 'dev' and args.archive)
    test_source = development_fixture(source) if version == 'dev' else source
    assert not re.search(r'-Verb\s+[\'\"]?RunAs|\bnetsh\b|New-NetFirewallRule', '\n'.join(
        line for line in source.splitlines() if not line.lstrip().startswith('#')), re.I), 'Installer requests elevation/firewall changes'
    paths = psjson(args.powershell, "[ordered]@{ local=[Environment]::GetFolderPath('LocalApplicationData'); config=[Environment]::GetFolderPath('ApplicationData'); desktop=[Environment]::GetFolderPath('DesktopDirectory'); programs=[Environment]::GetFolderPath('Programs'); shell=$PSVersionTable.PSVersion.ToString(); process=$env:PROCESSOR_ARCHITECTURE }")
    directory = Path(paths['local']) / 'Programs/SmartStage'
    executable = directory / 'smartstage.exe'
    config = Path(paths['config']) / 'SmartStage'
    links = [Path(paths[key]) / 'Smart Stage.lnk' for key in ('desktop', 'programs')]
    assert not directory.exists() and not config.exists() and not any(link.exists() for link in links), 'Use a clean ephemeral Windows user profile'
    assert not listening(8787) and not listening(8788), 'Default ports must be unused'
    network = load_network_helpers()
    report = {'status': 'incomplete', 'target': f'windows/{args.arch}', 'release': version,
              'installerSHA256': digest(source.encode()), 'defaultVersionUsed': args.version is None,
              'productionDownloads': args.archive is None, 'shell': paths, 'installedPath': str(executable)}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
    bootstrap_verification_url = None
    if args.archive:
        assert not args.bootstrap_url, '--bootstrap-url is only for unmodified public installation checks'
    else:
        bootstrap_url = args.bootstrap_url or 'https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1'
        parsed = urllib.parse.urlsplit(bootstrap_url)
        assert parsed.scheme == 'https', 'Public bootstrap verification requires HTTPS'
        expected = (ROOT / 'install.ps1').read_bytes()
        for attempt in range(10):
            query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
            query.append(('smartstage_verify', digest(expected) + '-' + str(time.time_ns())))
            bootstrap_verification_url = urllib.parse.urlunsplit(parsed._replace(query=urllib.parse.urlencode(query)))
            request = urllib.request.Request(bootstrap_verification_url, headers={'User-Agent': 'Smart-Stage-Windows-installer-verification', 'Cache-Control': 'no-cache'})
            with urllib.request.urlopen(request, timeout=120) as response:
                published = response.read()
            if published == expected:
                break
            time.sleep(3)
        assert published == expected, f'Published bootstrap differs from checkout: expected {digest(expected)}, got {digest(published)}'
        source = published.decode('utf-8')
        test_source = source
        report.update(bootstrapURL=bootstrap_url, bootstrapVerificationURL=bootstrap_verification_url,
                      bootstrapSHA256=digest(published), bootstrapFetchAttempts=attempt + 1,
                      publishedBootstrapMatchesCheckout=True)
    original_firewall = firewall_snapshot(args.powershell)
    pid = None
    with tempfile.TemporaryDirectory(prefix='smartstage-windows-installer-') as temporary:
        work = Path(temporary)
        try:
            config.mkdir(parents=True)
            sentinel = config / 'installer-preservation-sentinel'
            sentinel.write_bytes(b'existing settings and media stay here\n')
            adapted = test_source
            if args.archive:
                write_archive(work / 'candidate', args.arch, raw=args.archive.read_bytes())
                adapted = fixture_script(test_source, work / 'candidate')
            report['installOutput'] = install(args.powershell, adapted, work, version, override=args.version is not None,
                                              bootstrap_url=bootstrap_verification_url)
            report['publicIRMEntryPointVerified'] = bootstrap_verification_url is not None
            binary = executable.read_bytes()
            report['executableSHA256'] = digest(binary)
            if args.archive:
                with zipfile.ZipFile(args.archive) as candidate:
                    assert binary == candidate.read('smartstage.exe'), 'Installed bytes differ from the candidate archive'
                report['candidateArchiveSHA256'] = digest(args.archive.read_bytes())
            offset = struct.unpack_from('<I', binary, 0x3c)[0]
            assert struct.unpack_from('<H', binary, offset + 4)[0] == {'amd64': 0x8664, 'arm64': 0xaa64}[args.arch]
            if not args.allow_legacy_browser:
                assert struct.unpack_from('<H', binary, offset + 24 + 68)[0] == 2, 'Release executable must use the Windows GUI subsystem'
            report['shortcuts'] = []
            for link in links:
                info = psjson(args.powershell, f"$s=(New-Object -ComObject WScript.Shell).CreateShortcut({quote(link)}); [ordered]@{{ path={quote(link)}; target=$s.TargetPath; icon=$s.IconLocation; arguments=$s.Arguments; workingDirectory=$s.WorkingDirectory }}")
                assert os.path.samefile(info['target'], executable), info
                assert info['icon'].lower() == str(executable).lower() + ',0' and info['arguments'] == '', info
                report['shortcuts'].append(info)
            assert sentinel.read_bytes() == b'existing settings and media stay here\n'

            # Hash corruption, path traversal, and wrong architecture all fail before replacement.
            for case, failure, name, payload in (
                ('checksum', 'checksum does not match', 'smartstage.exe', binary),
                ('traversal', 'exactly one smartstage.exe', '../smartstage.exe', binary),
                ('architecture', 'wrong CPU architecture', 'smartstage.exe', binary[:offset + 4] + b'\x4c\x01' + binary[offset + 6:]),
            ):
                folder = work / case
                archive = write_archive(folder, args.arch, data=payload, name=name)
                if case == 'checksum':
                    archive.write_bytes(archive.read_bytes() + b'corrupted')
                install(args.powershell, fixture_script(test_source, folder), work, version, failure=failure)
                assert executable.read_bytes() == binary
                assert not (directory.parent / 'smartstage.exe').exists()
            report['rejectedCorruptArchiveTraversalAndWrongArchitecture'] = True

            # Prove a failure after the first shortcut save restores the previous PE AND both links.
            old_binary = binary + b'\nSMARTSTAGE-ROLLBACK-VERIFICATION\n'
            executable.write_bytes(old_binary)
            old_links = {link: link.read_bytes() for link in links}
            marker = '            $shortcut.Save()'
            assert adapted.count(marker) == 1
            failing = adapted.replace(marker, marker + "\n            throw 'Injected shortcut failure'")
            install(args.powershell, failing, work, version, failure='Injected shortcut failure')
            assert executable.read_bytes() == old_binary
            assert all(link.read_bytes() == old_links[link] for link in links)
            report['rollbackRestoredPreviousBinaryAndShortcuts'] = True
            install(args.powershell, adapted, work, version)
            assert executable.read_bytes() == binary
            report['reinstallIsIdempotent'] = True

            # Force the missing-runtime branch in a private copy and supply an unsigned PE.
            unsigned = adapted.replace('        $runtime = Get-WebView2Version\n', '        $runtime = $null\n', 1)
            runtime_line = "        Receive-InstallerFile 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' $bootstrap"
            assert unsigned.count(runtime_line) == 1
            unsigned = unsigned.replace(runtime_line, f'        Copy-Item -LiteralPath {quote(executable)} -Destination $bootstrap')
            install(args.powershell, unsigned, work, version, failure='valid Microsoft Corporation signature')
            assert executable.read_bytes() == binary
            report['unsignedWebView2InstallerRejectedWithoutExecution'] = True

            report['launchOutput'] = install(args.powershell, adapted, work, version, launch=True)
            until(lambda: listening(8787), 'Installer did not start Admin')
            pid = network.socket_owner(8787)
            admin = network.Client(8787).authenticate()
            state = admin.request('GET', '/api/state')['state']
            report.update(appPID=pid, instanceID=state['instanceId'])
            assert not listening(8788), 'Default gateway mode unexpectedly opened the LAN listener'
            update = admin.request('GET', '/api/update')
            report['updateStatus'] = update
            assert update['currentVersion'] == version, update
            if version == 'dev':
                assert update['phase'] == 'unsupported' and 'Development builds' in update['message'], update
                report['automaticUpdateCheckSkipped'] = 'Development build; installer update compatibility is verified with published releases.'
            else:
                assert update['phase'] != 'unsupported', update
                report['installedLocationSupportsAutomaticUpdates'] = True
            if not args.allow_legacy_browser:
                windows = until(lambda: [window for window in native_windows(pid) if window['class'] == 'SmartStageAdmin-View'], 'Installer did not show the dedicated app window')
                assert not any(window['class'] == 'ConsoleWindowClass' for window in native_windows(pid))
                report['nativeWindows'] = windows
                children = until(lambda: psjson(args.powershell, f"@(Get-CimInstance Win32_Process -Filter \"Name='msedgewebview2.exe'\" | Where-Object ParentProcessId -eq {pid} | Select-Object ProcessId,ParentProcessId,ExecutablePath)"), 'No WebView2 child process belongs to the installed application')
                report['webView2Children'] = children
                assert len(windows) == 1, 'Installed app has multiple visible Admin windows'
                before_escape = admin.request('GET', '/api/state')['state']
                post_escape(windows[0]['hwnd'])

                def emergency_observed():
                    current = admin.request('GET', '/api/state')['state']
                    if current['stopEpoch'] > before_escape['stopEpoch'] and not current['stageEnabled']:
                        return current
                    return None

                after_escape = until(emergency_observed, 'Native Admin Escape did not reach the Go emergency-stop handler')
                assert after_escape['instanceId'] == before_escape['instanceId']
                report.update(nativeEscapeReachedEmergencyStop=True,
                              nativeEscapeMethod='WM_KEYDOWN/WM_KEYUP posted to the installed app Admin HWND; observed through authenticated HTTP state',
                              nativeEscapeStopEpochBefore=before_escape['stopEpoch'],
                              nativeEscapeStopEpochAfter=after_escape['stopEpoch'],
                              nativeEscapeStageDisabled=not after_escape['stageEnabled'])
            install(args.powershell, adapted, work, version, failure='Smart Stage is running')
            assert executable.read_bytes() == binary and network.socket_owner(8787) == pid
            report['runningAppRefusedWithoutReplacement'] = True
            # A second shortcut launch must activate the same app, with no extra server/window.
            subprocess.Popen(['cmd.exe', '/c', 'start', '', str(links[0])], creationflags=subprocess.CREATE_NO_WINDOW).wait(timeout=15)
            time.sleep(2)
            assert network.socket_owner(8787) == pid
            if not args.allow_legacy_browser:
                assert [window['hwnd'] for window in native_windows(pid) if window['class'] == 'SmartStageAdmin-View'] == [window['hwnd'] for window in report['nativeWindows']]
            report['shortcutRelaunchReusesRunningApp'] = True
            admin.request('POST', '/api/quit', {}, expected=202)
            until(lambda: not listening(8787), 'Admin Quit did not stop the installed app')
            wait_process_exit(pid)
            report['adminQuitWaitedForNativeProcessExit'] = True
            pid = None
            assert not listening(8788)
            assert sentinel.read_bytes() == b'existing settings and media stay here\n'
            assert firewall_snapshot(args.powershell) == original_firewall, 'Installer or default gateway app changed firewall rules'
            report.update(status='passed', configPreserved=True, defaultGatewayHasNoLANListener=True,
                          firewallRulesUnchanged=True, noElevationRequested=True, adminQuitClosedServer=True)
        finally:
            if pid:
                subprocess.run(['taskkill', '/PID', str(pid), '/T', '/F'], capture_output=True, timeout=20)
            try:
                for link in links:
                    link.unlink(missing_ok=True)
                remove_fixture_tree(directory)
                remove_fixture_tree(config)
                report['testInstallationAndConfigRemoved'] = True
            except Exception as cleanup_error:
                report.update(status='failed', cleanupError=str(cleanup_error))
                raise
            finally:
                args.output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
