#!/usr/bin/env python3
"""Verify gateway-default and explicit LAN restart using a real native executable.

The actual restart helper, native lifecycle, ports, authentication, and saved
configuration are exercised. Firewall elevation is explicitly skipped here;
scoped OS rule verification is a separate check.
"""
import argparse
import ctypes
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import platform
import re
import signal
import socket
import subprocess
import tempfile
import time
import urllib.parse
import wave


def until(check, message, timeout=45):
    end = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < end:
        try:
            value = check()
            if value:
                return value
        except (OSError, http.client.HTTPException) as error:
            last_error = error
        time.sleep(0.1)
    raise AssertionError(f"{message}{': ' + str(last_error) if last_error else ''}")


def unused_port():
    with socket.socket() as candidate:
        candidate.bind(('127.0.0.1', 0))
        return candidate.getsockname()[1]


def listening(port):
    with socket.socket() as connection:
        connection.settimeout(0.5)
        return connection.connect_ex(('127.0.0.1', port)) == 0


class Client:
    def __init__(self, port):
        self.port = port
        self.origin = f'http://127.0.0.1:{port}'
        self.cookies = SimpleCookie()
        self.csrf = ''

    def request(self, method, path, body=None, expected=200):
        connection = http.client.HTTPConnection('127.0.0.1', self.port, timeout=5)
        headers = {'Origin': self.origin, 'X-CSRF-Token': self.csrf}
        if self.cookies:
            headers['Cookie'] = '; '.join(f'{key}={item.value}' for key, item in self.cookies.items())
        payload = None if body is None else json.dumps(body).encode()
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        try:
            connection.request(method, path, body=payload, headers=headers)
            response = connection.getresponse()
            for key, value in response.getheaders():
                if key.lower() == 'set-cookie':
                    self.cookies.load(value)
            value = json.loads(response.read())
            assert response.status == expected, (method, path, response.status, value)
            return value
        finally:
            connection.close()

    def authenticate(self):
        value = self.request('POST', '/api/local-session', {})
        assert value['role'] == 'admin'
        self.csrf = value['csrfToken']
        return self


def socket_owner(port):
    if os.name == 'nt':
        # Use numeric endpoints and PID fields; no localized state word parsing.
        result = subprocess.check_output(['netstat', '-ano', '-p', 'tcp'], text=True, timeout=15)
        pids = {int(parts[-1]) for line in result.splitlines()
                if len(parts := line.split()) >= 5 and parts[0] == 'TCP'
                and parts[1] == f'127.0.0.1:{port}' and parts[2] == '0.0.0.0:0'}
    else:
        result = subprocess.run(['/usr/sbin/lsof', '-nP', f'-iTCP:{port}', '-sTCP:LISTEN', '-t'],
                                capture_output=True, text=True, timeout=15)
        pids = {int(value) for value in result.stdout.split() if value.isdigit()}
    assert len(pids) == 1, f'Expected one native Admin listener PID, found {pids}'
    return next(iter(pids))


def windows_process_image(pid):
    from ctypes import wintypes
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.QueryFullProcessImageNameW.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR,
                                                ctypes.POINTER(wintypes.DWORD)]
    kernel.QueryFullProcessImageNameW.restype = wintypes.BOOL
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    handle = kernel.OpenProcess(0x1000, False, pid)
    if not handle:
        return ''
    try:
        size = wintypes.DWORD(32768)
        buffer = ctypes.create_unicode_buffer(size.value)
        if not kernel.QueryFullProcessImageNameW(handle, 0, buffer, ctypes.byref(size)):
            return ''
        return buffer.value
    finally:
        kernel.CloseHandle(handle)


def process_image(pid):
    if os.name == 'nt':
        return windows_process_image(pid)
    result = subprocess.run(['/bin/ps', '-p', str(pid), '-o', 'comm='],
                            capture_output=True, text=True, timeout=5)
    return result.stdout.strip() if result.returncode == 0 else ''


def same_executable(pid, executable):
    image = process_image(pid)
    return bool(image) and os.path.normcase(str(Path(image).resolve())) == os.path.normcase(str(executable))


def terminate_verified(pid, executable):
    if not pid or not same_executable(pid, executable):
        return
    if os.name == 'nt':
        subprocess.run(['taskkill', '/PID', str(pid), '/F'], capture_output=True, timeout=10)
    else:
        os.kill(pid, signal.SIGTERM)
        try:
            until(lambda: not same_executable(pid, executable), 'Native process did not terminate', 10)
        except AssertionError:
            if same_executable(pid, executable):
                os.kill(pid, signal.SIGKILL)


def verify(executable, report, first_log, restart_log):
    version = subprocess.check_output([str(executable), '--version'], text=True, timeout=15).strip()
    assert re.search(r', (darwin|windows)/(arm64|amd64)$', version), 'Use a supported native executable'
    report['application'] = version
    command_port = unused_port()
    with tempfile.TemporaryDirectory(prefix='smartstage-network-mode-') as scratch:
        config = Path(scratch).resolve()
        media = config / 'Original media'; media.mkdir()
        sound = media / 'Restart fixture.wav'
        with wave.open(str(sound), 'wb') as fixture:
            fixture.setparams((1, 2, 8000, 800, 'NONE', 'not compressed'))
            fixture.writeframes(b'\0\0' * 800)
        environment = os.environ.copy()
        for name in ('SMARTSTAGE_APP_LAUNCH', 'SMARTSTAGE_APP_ICON', 'SMARTSTAGE_LOG_PATH'):
            environment.pop(name, None)
        environment['SMARTSTAGE_SKIP_FIREWALL'] = '1'
        process = None
        new_pid = None
        admin = None
        admin_port = None
        with first_log.open('wb') as output:
            try:
                process = subprocess.Popen([
                    str(executable), '--admin-port', '0', '--port', str(command_port), '--bind', '0.0.0.0',
                    '--config-dir', str(config), '--media-root', str(media), '--no-auto-update', '--no-browser',
                ], stdout=output, stderr=subprocess.STDOUT, env=environment)

                def started():
                    if process.poll() is not None:
                        raise AssertionError(f'Initial native process exited with {process.returncode}')
                    match = re.search(r'^Admin: http://127\.0\.0\.1:(\d+)/admin$',
                                      first_log.read_text(encoding='utf-8', errors='replace'), re.M)
                    return int(match[1]) if match else None

                admin_port = until(started, 'Initial native Admin did not start')
                assert admin_port != command_port
                admin = until(lambda: Client(admin_port).authenticate(), 'Local Admin did not authenticate')
                initial = admin.request('GET', '/api/state')['state']
                gateway = admin.request('GET', '/api/gateway')
                assert gateway['mode'] == 'gateway' and gateway['status'] == 'unconfigured', gateway
                assert not gateway['hasToken'] and not gateway['remoteURL']
                assert not listening(command_port), 'Fresh gateway mode opened an incoming LAN listener'
                remote = admin.request('GET', '/api/remote-control')
                assert remote['mode'] == 'gateway' and not remote['links'] and not remote['token']
                assert socket_owner(admin_port) == process.pid
                report.update(gatewayDefault=True, unconfiguredHasNoFallback=True,
                              initialAdminPID=process.pid, assignedAdminPort=admin_port,
                              configuredCommandPort=command_port, defaultLANListenerClosed=True)
                saved = admin.request('PUT', '/api/playlist', {
                    'expectedRevision': initial['playlistRevision'],
                    'cues': [{'id': '', 'label': 'Preserved across network restart', 'path': str(sound)}],
                })
                cue_id = saved['cues'][0]['id']
                changed = admin.request('PUT', '/api/gateway', {'mode': 'lan', 'url': ''})
                assert changed['mode'] == 'lan' and changed['restart'] is True, changed
                process.wait(timeout=45)
                assert process.returncode == 0, f'Previous process failed during native cleanup: {process.returncode}'
                until(lambda: listening(admin_port), 'Restarted Admin did not reclaim its assigned port', 60)
                new_pid = socket_owner(admin_port)
                assert new_pid != process.pid and same_executable(new_pid, executable), 'Restart did not launch this native executable as a new process'
                admin.request('GET', '/api/state', expected=401)
                admin = Client(admin_port).authenticate()
                current = admin.request('GET', '/api/state')['state']
                assert current['instanceId'] != initial['instanceId'], 'Restart retained the old host instance'
                assert current['state'] == 'stopped' and not current['stageEnabled'] and not current['activeCueId']
                assert current['cues'][0]['id'] == cue_id and current['cues'][0]['label'] == 'Preserved across network restart'
                current_gateway = admin.request('GET', '/api/gateway')
                assert current_gateway['mode'] == 'lan' and current_gateway['status'] == 'disabled'
                assert not current_gateway.get('restart') and not current_gateway['remoteURL']
                assert json.loads((config / 'gateway.json').read_text())['mode'] == 'lan'
                listing = admin.request('GET', '/api/files')
                assert [str(Path(root).resolve()) for root in listing['roots']] == [str(media)], 'Restart lost the original media root argument'
                assert any(entry['name'] == sound.name for entry in listing['entries'])
                assert listening(command_port), 'Explicit LAN selection did not start the requested command port'
                remote = admin.request('GET', '/api/remote-control')
                assert remote['mode'] == 'lan' and re.fullmatch(r'\d{8}', remote['token'])
                assert remote['links'], 'LAN mode did not publish a network URL'
                for link in remote['links']:
                    parsed = urllib.parse.urlsplit(link['url'])
                    assert parsed.port == command_port and parsed.path == '/command'
                    assert parsed.fragment == 'token=' + remote['token']
                command = Client(command_port)
                session = command.request('POST', '/api/pair', {'key': remote['token']})
                assert session['role'] == 'command'
                command.csrf = session['csrfToken']
                assert command.request('GET', '/api/state')['state']['instanceId'] == current['instanceId']
                command.request('GET', '/api/gateway', expected=403)
                command.request('POST', '/api/local-session', {}, expected=404)
                report.update(explicitLANChoiceAcknowledged=True, previousNativeProcessExited=True,
                              restartedNativePID=new_pid, instanceRotated=True, oldAdminSessionRejected=True,
                              assignedAdminPortPreserved=True, commandPortPreserved=True,
                              mediaRootAndPlaylistPreserved=True, savedLANModeRestored=True,
                              configuredModeStatusRestored=True,
                              LANCommandPairingVerified=True, LANCannotConfigureGateway=True,
                              nativeFirewallElevationVerified=False, firewallSkipInherited=True)
                # The skip marker proves the detached helper inherited test settings;
                # it does not claim that an OS permission prompt was displayed.
                helper_log = until(lambda: (config / 'restart.log').read_text(encoding='utf-8', errors='replace')
                                   if (config / 'restart.log').exists() else '', 'Restart helper did not write its log')
                assert 'Firewall setup skipped' in helper_log
                quit_result = admin.request('POST', '/api/quit', {}, expected=202)
                assert quit_result['quitting'] is True
                until(lambda: not listening(admin_port) and not listening(command_port),
                      'Quit did not close both native listeners', 30)
                until(lambda: not same_executable(new_pid, executable), 'Restarted native process did not exit after Quit', 30)
                report.update(authenticatedQuitVerified=True, noListenersAfterQuit=True, restartedNativeProcessExited=True)
            finally:
                if admin_port and listening(admin_port):
                    try:
                        cleanup = Client(admin_port).authenticate()
                        cleanup.request('POST', '/api/quit', {}, expected=202)
                        until(lambda: not listening(admin_port), 'Cleanup Quit timed out', 10)
                    except Exception:
                        pass
                if new_pid:
                    terminate_verified(new_pid, executable)
                elif admin_port and listening(admin_port):
                    try:
                        terminate_verified(socket_owner(admin_port), executable)
                    except (OSError, AssertionError, subprocess.SubprocessError):
                        pass
                if process and process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        process.kill(); process.wait(timeout=5)
                if (config / 'restart.log').exists():
                    restart_log.write_bytes((config / 'restart.log').read_bytes())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('executable', type=Path)
    args = parser.parse_args()
    executable = args.executable.resolve()
    assert executable.is_file()
    report_path = executable.with_name(executable.name + '.network-mode.json')
    first_log = executable.with_name(executable.name + '.network-mode-initial.log')
    restart_log = executable.with_name(executable.name + '.network-mode-restart.log')
    report = {'status': 'incomplete', 'hostOS': platform.system(), 'physicalPlaybackVerified': False}
    try:
        verify(executable, report, first_log, restart_log)
        report['status'] = 'passed'
        print('Native network-mode restart passed: gateway default, explicit LAN restart, preserved ports/configuration, new process/session, pairing and Quit.')
    except Exception as error:
        report.update(status='failed', error=str(error))
        for log in (first_log, restart_log):
            if log.exists():
                print(f'{log.name}:\n{log.read_text(encoding="utf-8", errors="replace")}')
        raise
    finally:
        report_path.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
