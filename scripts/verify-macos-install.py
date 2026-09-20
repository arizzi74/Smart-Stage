#!/usr/bin/env python3
"""Verify the published Mac app installer on an ephemeral native CI runner.

The quarantine test instruments a private copy of the installer to attach real
macOS quarantine attributes immediately before its normal cleanup. Production
download URLs and installation behavior are otherwise unchanged.
"""
import argparse
import ctypes
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request
import zipfile

from macos_app_checks import (background_launch_checks, launch_snapshot,
                              quit_background_app, stop_core)


QUARANTINE = "com.apple.quarantine"
CUSTOM_ATTRIBUTE = "com.smartstage.ci.keep"
QUARANTINE_VALUE = "0083;65000000;SmartStageInstallVerification;"
CUSTOM_VALUE = "preserve-this-attribute"
BUNDLE_NAME = "Smart Stage.app"
BUNDLE_ID = "com.github.arizzi74.smartstage"
ROOT = Path(__file__).resolve().parents[1]
FIREWALL_TOOL = "/usr/libexec/ApplicationFirewall/socketfilterfw"
FIREWALL_AUTH = "do shell script command with administrator privileges"


def gateway_default(version):
    legacy = re.fullmatch(r"v0[.]1[.]0-preview[.](\d+)", version)
    return not legacy or int(legacy[1]) >= 15


def run(command, **kwargs):
    return subprocess.run(command, check=True, text=True, capture_output=True,
                          timeout=180, **kwargs)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def fetch(url, no_cache=False):
    headers = {"User-Agent": "Smart-Stage-Mac-installer-verification"}
    if no_cache:
        headers["Cache-Control"] = "no-cache"
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request, timeout=120) as response:
        return response.read()


def xattr_write(path, attribute, value):
    run(["/usr/bin/xattr", "-w", attribute, value, str(path)])


def xattr_read(path, attribute):
    result = subprocess.run(["/usr/bin/xattr", "-p", attribute, str(path)],
                            text=True, capture_output=True, timeout=15)
    return result.stdout.rstrip("\n") if result.returncode == 0 else None


def assert_no_quarantine(bundle):
    for path in (bundle, *bundle.rglob("*")):
        if xattr_read(path, QUARANTINE) is not None:
            raise AssertionError(f"Quarantine remains on {path.relative_to(bundle.parent)}")


def install(script, directory, version, launch=False, expect_failure=False, configure_firewall=False):
    environment = os.environ.copy()
    environment["SMARTSTAGE_INSTALL_DIR"] = str(directory)
    if version is None:
        environment.pop("SMARTSTAGE_VERSION", None)
    else:
        environment["SMARTSTAGE_VERSION"] = version
    if launch:
        environment.pop("SMARTSTAGE_NO_LAUNCH", None)
    else:
        environment["SMARTSTAGE_NO_LAUNCH"] = "1"
    environment.pop("SMARTSTAGE_SKIP_FIREWALL", None)
    if configure_firewall:
        environment["SMARTSTAGE_CONFIGURE_LAN_FIREWALL"] = "1"
    else:
        environment.pop("SMARTSTAGE_CONFIGURE_LAN_FIREWALL", None)
        selected_version = version or re.search(r'version=\$\{SMARTSTAGE_VERSION:-(v[0-9A-Za-z._-]+)\}', script)[1]
        if not gateway_default(selected_version):
            environment["SMARTSTAGE_SKIP_FIREWALL"] = "1"
    # stdin deliberately reproduces the documented curl | sh entry point.
    result = subprocess.run(["/bin/sh"], input=script, env=environment,
                            text=True, capture_output=True, timeout=240)
    if expect_failure:
        if result.returncode == 0:
            raise AssertionError("Installer accepted a destination that must be refused")
    elif result.returncode != 0:
        raise AssertionError(f"Installer failed ({result.returncode}):\n{result.stdout}\n{result.stderr}")
    return result


def firewall_query(option, path=None):
    command = [FIREWALL_TOOL, option]
    if path is not None:
        command.append(str(path))
    environment = os.environ.copy()
    environment["LC_ALL"] = "C"
    return run(command, env=environment).stdout.strip()


def firewall_globals():
    return {option: firewall_query(option) for option in
            ("--getglobalstate", "--getblockall", "--getallowsigned", "--getstealthmode")}


def firewall_apps():
    applications = {}
    current = None
    output = firewall_query("--listapps")
    for line in output.splitlines():
        match = re.match(r"^\s*\d+\s*:\s*(.+?)\s*$", line)
        if match:
            current = match[1]
        elif current and re.fullmatch(r"\s*\(.*\)\s*", line):
            applications[current] = line.strip()
            current = None
    count = re.search(r"total number of apps\s*=\s*(\d+)", output, re.IGNORECASE)
    assert count and len(applications) == int(count[1]), f"Unrecognized firewall rule listing: {output}"
    return applications


def firewall_state(path):
    text = firewall_query("--getappblocked", path)
    if re.search(r" is (?:not blocked|permitted)\.?\s*$|^\s*\(\s*Allow incoming connections\s*\)\s*$", text, re.MULTILINE):
        return "allowed", text
    if re.search(r" is blocked\.?\s*$|^\s*\(\s*Block incoming connections\s*\)\s*$", text, re.MULTILINE):
        return "blocked", text
    return "unknown", text


def firewall_checks(script, destination, version, report):
    """Use real ALF rules; replace only interactive elevation on the CI runner."""
    assert script.count(FIREWALL_AUTH) == 1, "Update the test-only elevation adapter for the installer"
    bundle = destination / BUNDLE_NAME
    core = bundle / "Contents/MacOS/smartstage"
    targets = (bundle, core)
    before_globals = firewall_globals()
    before_apps = firewall_apps()
    assert all(str(path) not in before_apps for path in targets), "Use unique fresh paths for firewall fixtures"
    original_hash = digest(core.read_bytes())
    created = []
    report["firewallGlobalSettingsBefore"] = before_globals
    try:
        # These are the same two identities a user's existing block may refer
        # to: the app bundle and the core that actually owns the TCP listener.
        for path in targets:
            created.append(path)
            run(["/usr/bin/sudo", "-n", FIREWALL_TOOL, "--add", str(path)])
            run(["/usr/bin/sudo", "-n", FIREWALL_TOOL, "--blockapp", str(path)])
        blocked = {str(path): firewall_state(path) for path in targets}
        report["firewallBlockedFixture"] = blocked
        assert all(state[0] == "blocked" for state in blocked.values()), blocked

        cancelled = script.replace(FIREWALL_AUTH, 'error "User canceled." number -128', 1)
        result = install(cancelled, destination, version, expect_failure=True, configure_firewall=True)
        assert "firewall approval was cancelled or denied" in result.stderr.lower(), result.stderr
        assert "System Settings > Network > Firewall > Options" in result.stderr
        assert digest(core.read_bytes()) == original_hash
        assert all(firewall_state(path)[0] == "blocked" for path in targets)
        assert not list(destination.glob(".smartstage-install*"))
        report.update(firewallCancellationKeptInstalledApp=True,
                      firewallCancellationExplainedRecovery=True,
                      firewallCancellationKeptExistingRules=True)

        # The CI account has passwordless sudo. AppleScript still constructs
        # and quotes the exact production commands, and socketfilterfw itself
        # performs and reports the real mutations. No production test hook.
        elevated = script.replace(FIREWALL_AUTH,
            'do shell script "/usr/bin/sudo -n /bin/sh -c " & quoted form of command', 1)
        install(elevated, destination, version, configure_firewall=True)
        allowed = {str(path): firewall_state(path) for path in targets}
        report["firewallAllowedAfterInstaller"] = allowed
        assert all(state[0] == "allowed" for state in allowed.values()), allowed
        assert digest(core.read_bytes()) == original_hash

        no_prompt = script.replace(FIREWALL_AUTH, 'error "Unexpected administrator prompt" number 99', 1)
        result = install(no_prompt, destination, version, configure_firewall=True)
        assert "already allowed by the macOS firewall" in result.stdout
        assert firewall_globals() == before_globals, "Installer changed a global firewall setting"
        remaining_apps = {path: state for path, state in firewall_apps().items()
                          if path not in {str(target) for target in targets}}
        assert remaining_apps == before_apps, "Installer changed an unrelated application rule"
        report.update(realBlockedFirewallRulesUnblocked=True,
                      actualListeningExecutableAllowed=True,
                      existingBlockedBundleRuleUnblocked=True,
                      alreadyAllowedIdenticalAppSkippedElevation=True,
                      firewallGlobalSettingsUnchanged=True,
                      unrelatedFirewallRulesUnchanged=True,
                      firewallElevationTestMethod="AppleScript with test-only passwordless sudo adapter")
    finally:
        report["firewallFinalReadbackBeforeCleanup"] = {str(path): firewall_state(path) for path in created}
        for path in reversed(created):
            run(["/usr/bin/sudo", "-n", FIREWALL_TOOL, "--remove", str(path)])
        assert firewall_globals() == before_globals, "Global firewall settings changed during verification"
        assert firewall_apps() == before_apps, "Firewall fixture cleanup changed unrelated rules"
        report["firewallTestRulesRemoved"] = True


def quarantined_installer(script):
    # This one exact source anchor intentionally makes an installer refactor
    # require an explicit update of the quarantine test instrumentation.
    anchor = 'staged="$work/extracted/Smart Stage.app"'
    if script.count(anchor) != 1:
        raise AssertionError("Installer staging changed; update the test-only injection anchor")
    injection = "\n".join([
        anchor,
        f'/usr/bin/xattr -w {QUARANTINE} "{QUARANTINE_VALUE}" "$staged"',
        f'/usr/bin/xattr -w {QUARANTINE} "{QUARANTINE_VALUE}" "$staged/Contents/MacOS/smartstage"',
        f'/usr/bin/xattr -w {QUARANTINE} "{QUARANTINE_VALUE}" "$staged/Contents/Resources/Start Smart Stage.command"',
        f'/usr/bin/xattr -w {CUSTOM_ATTRIBUTE} "{CUSTOM_VALUE}" "$staged"',
        f'/usr/bin/xattr -w {CUSTOM_ATTRIBUTE} "{CUSTOM_VALUE}" "$staged/Contents/MacOS/smartstage"',
    ])
    return script.replace(anchor, injection, 1)


def official_bundle(scratch, version, architecture, report):
    name = f"smartstage-darwin-{architecture}.app.zip"
    base = f"https://github.com/arizzi74/Smart-Stage/releases/download/{version}"
    archive = scratch / name
    archive.write_bytes(fetch(f"{base}/{name}"))
    checksum = fetch(f"{base}/{name}.sha256").decode("ascii").strip()
    match = re.fullmatch(r"([0-9a-fA-F]{64})\s+\*?([^\r\n]+)", checksum)
    if not match or match[2] != name or digest(archive.read_bytes()) != match[1].lower():
        raise AssertionError("Published app archive checksum is invalid")
    prefix = f"{BUNDLE_NAME}/Contents/"
    with zipfile.ZipFile(archive) as source:
        expected = {
            "core": digest(source.read(prefix + "MacOS/smartstage")),
            "launcher": digest(source.read(prefix + "MacOS/SmartStageLauncher")),
            "icon": digest(source.read(prefix + "Resources/smartstage.icns")),
        }
    report.update(archive=name, archiveSHA256=match[1].lower(),
                  archiveChecksumVerified=True, downloadURL=f"{base}/{name}")
    return expected


def verify_bundle(bundle, expected, architecture, version, scratch, report):
    contents = bundle / "Contents"
    info = plistlib.loads((contents / "Info.plist").read_bytes())
    assert info["CFBundleIdentifier"] == BUNDLE_ID
    assert info["CFBundlePackageType"] == "APPL"
    assert info["CFBundleExecutable"] == "SmartStageLauncher"
    assert info["CFBundleIconFile"] == "smartstage.icns"
    assert info["SmartStageVersion"] == version
    # Preview 7 remains a valid selectable installer target. Only newer app
    # metadata promises the visible Dock behavior checked after real launch.
    if info.get("SmartStageDockIcon"):
        assert info.get("LSUIElement", False) is False
        assert info.get("LSBackgroundOnly", False) is False
        report["installedBundleDeclaresVisibleDockApp"] = True
    if info.get("SmartStageNativeAdminWindow"):
        assert info["NSAppTransportSecurity"] == {
            "NSAllowsLocalNetworking": True,
            "NSExceptionDomains": {"127.0.0.1": {"NSExceptionAllowsInsecureHTTPLoads": True}},
        }
        report["installedBundleDeclaresDedicatedAdminWindow"] = True
    core = contents / "MacOS/smartstage"
    launcher = contents / "MacOS/SmartStageLauncher"
    icon = contents / "Resources/smartstage.icns"
    command = contents / "Resources/Start Smart Stage.command"
    assert digest(core.read_bytes()) == expected["core"], "Installed core differs from the release archive"
    assert digest(launcher.read_bytes()) == expected["launcher"], "Installed launcher differs from the release archive"
    assert digest(icon.read_bytes()) == expected["icon"] == digest((ROOT / "assets/icon/smartstage.icns").read_bytes())
    assert all(os.access(path, os.X_OK) for path in (core, launcher, command))
    run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(bundle)])
    decoded = scratch / "decoded.iconset"
    run(["/usr/bin/iconutil", "--convert", "iconset", "--output", str(decoded), str(icon)])
    assert len(list(decoded.glob("*.png"))) >= 8
    actual = run([str(core), "--version"]).stdout.strip()
    assert f" {version} " in actual and actual.endswith(f"darwin/{architecture}"), actual
    assert run([str(command), "--version"]).stdout.strip() == actual
    report.update(bundleIdentifier=BUNDLE_ID, installedCoreSHA256=expected["core"],
                  installedLauncherSHA256=expected["launcher"], installedReleaseBytesMatch=True,
                  iconSHA256=expected["icon"], iconDecodedByMacOS=True,
                  adHocSignatureVerified=True, executablePermissionsPreserved=True,
                  spacesQuotesUnicodePathPassed=True, version=actual)


def get(port, path):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
    try:
        connection.request("GET", path)
        response = connection.getresponse()
        return response.status, response.read()
    finally:
        connection.close()


def require_free_default_ports():
    for port in (8787, 8788):
        with socket.socket() as probe:
            if probe.connect_ex(("127.0.0.1", port)) == 0:
                raise AssertionError(f"Default launch verification needs unused port {port}")


def find_core_pid(bundle):
    library = ctypes.CDLL("/usr/lib/libproc.dylib")
    library.proc_pidpath.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32]
    library.proc_pidpath.restype = ctypes.c_int
    expected = (bundle / "Contents/MacOS/smartstage").resolve()
    listeners = subprocess.run(["/usr/sbin/lsof", "-nP", "-iTCP:8787", "-sTCP:LISTEN", "-Fp"],
                               text=True, capture_output=True, timeout=15)
    for line in listeners.stdout.splitlines():
        if line.startswith("p"):
            candidate = int(line[1:])
            executable = ctypes.create_string_buffer(4096)
            if library.proc_pidpath(candidate, executable, len(executable)) > 0:
                if Path(os.fsdecode(executable.value)).resolve() == expected:
                    return candidate
    return None


def default_launch(script, directory, version, report):
    require_free_default_ports()
    bundle = directory / BUNDLE_NAME
    info = plistlib.loads((bundle / "Contents/Info.plist").read_bytes())
    background = bool(info.get("SmartStageBackgroundLaunch"))
    snapshot = launch_snapshot() if background else None
    install(script, directory, version, launch=True)
    pid = None
    try:
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            pid = find_core_pid(bundle)
            if pid:
                try:
                    status, page = get(8787, "/admin")
                    if status == 200 and b"Smart Stage" in page:
                        break
                except OSError:
                    pass
            time.sleep(0.5)
        else:
            raise AssertionError("Installer did not launch the installed core through its app bundle")
        listeners = run(["/usr/sbin/lsof", "-nP", "-a", "-p", str(pid),
                         "-iTCP:8787", "-sTCP:LISTEN", "-Fn"]).stdout.splitlines()
        addresses = [line[1:] for line in listeners if line.startswith("n")]
        assert addresses == ["127.0.0.1:8787"], addresses
        if gateway_default(version):
            with socket.socket() as remote_probe:
                remote_probe.settimeout(2)
                assert remote_probe.connect_ex(("127.0.0.1", 8788)) != 0, \
                    "Fresh installation must not open the LAN listener"
            report.update(gatewayModeDefault=True, lanListenerClosedByDefault=True)
        else:
            assert get(8788, "/admin")[0] in (403, 404)
            report.update(legacyLANRelease=True, remoteListenerRejectsAdmin=True)
        original = (bundle / "Contents/MacOS/smartstage").read_bytes()
        refusal = install(script, directory, version, expect_failure=True)
        assert "running" in (refusal.stdout + refusal.stderr).lower(), "Refusal must explain the running app"
        assert (bundle / "Contents/MacOS/smartstage").read_bytes() == original
        assert find_core_pid(bundle) == pid
        assert get(8787, "/admin")[0] == 200
        if background:
            report.update(background_launch_checks(bundle, pid, snapshot, find_core_pid))
            report.update(quit_background_app(pid))
            pid = None
        report.update(installerLaunchedBundleAndCore=True, kernelExecutablePathMatched=True,
                      adminServedOnLoopback=True, adminListenerOnly127001=True,
                      runningAppReplacementRefused=True,
                      runningAppLeftServing=True)
    finally:
        stop_core(pid or find_core_pid(bundle))


def refusal_checks(script, scratch, bundle, version, report):
    symlink_parent = scratch / "symlink destination"
    symlink_parent.mkdir()
    link = symlink_parent / BUNDLE_NAME
    link.symlink_to(bundle, target_is_directory=True)
    before = digest((bundle / "Contents/MacOS/smartstage").read_bytes())
    install(script, symlink_parent, version, expect_failure=True)
    assert link.is_symlink() and link.resolve() == bundle.resolve()
    assert digest((bundle / "Contents/MacOS/smartstage").read_bytes()) == before

    wrong_parent = scratch / "unrelated application"
    unrelated = wrong_parent / BUNDLE_NAME
    (unrelated / "Contents").mkdir(parents=True)
    (unrelated / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "org.example.other-app"}))
    marker = unrelated / "keep-me.txt"
    marker.write_text("unrelated application must survive\n")
    install(script, wrong_parent, version, expect_failure=True)
    assert marker.read_text() == "unrelated application must survive\n"
    assert plistlib.loads((unrelated / "Contents/Info.plist").read_bytes())["CFBundleIdentifier"] == "org.example.other-app"
    report.update(symlinkDestinationRefused=True, symlinkTargetPreserved=True,
                  unrelatedApplicationRefused=True, unrelatedApplicationPreserved=True)


def failed_update_checks(script, destination, version, report):
    bundle = destination / BUNDLE_NAME
    core = bundle / "Contents/MacOS/smartstage"
    before = (bundle.stat().st_ino, core.stat().st_ino, digest(core.read_bytes()))

    def original_preserved():
        assert (bundle.stat().st_ino, core.stat().st_ino, digest(core.read_bytes())) == before
        assert xattr_read(bundle, CUSTOM_ATTRIBUTE) == CUSTOM_VALUE
        assert xattr_read(core, CUSTOM_ATTRIBUTE) == CUSTOM_VALUE
        assert not list(destination.glob(".smartstage-install*")), "Installer left a work directory or lock"

    # Corrupt the downloaded archive before the unmodified checksum check. Its
    # real published checksum remains intact, and the existing app must survive.
    anchor = '/usr/bin/awk -v name="$artifact" '
    assert script.count(anchor) == 1, "Installer checksum validation changed; update test injection"
    corrupted = script.replace(anchor, 'printf "verification-corruption" >> "$archive"\n' + anchor, 1)
    refused = install(corrupted, destination, version, expect_failure=True)
    assert "checksum does not match" in (refused.stdout + refused.stderr).lower()
    original_preserved()

    # Force a post-rename failure, while the old app is held for rollback. Inode
    # identity and retained attributes distinguish restoration from a fresh copy.
    anchor = '/bin/mv "$staged" "$destination"'
    assert script.count(anchor) == 1, "Installer replacement changed; update rollback test injection"
    rollback = script.replace(anchor, anchor + "\nfail 'Verification forced a failure after replacement.'", 1)
    refused = install(rollback, destination, version, expect_failure=True)
    assert "Verification forced a failure after replacement." in refused.stderr
    original_preserved()

    # A terminal closing between moving the previous app and recording any
    # subsequent state must also restore the original app from the filesystem.
    anchor = '/bin/mv "$destination" "$work/previous.app"'
    assert script.count(anchor) == 1, "Installer backup changed; update interruption test injection"
    interrupted = script.replace(anchor, anchor + "\nkill -HUP $$", 1)
    refused = install(interrupted, destination, version, expect_failure=True)
    assert refused.returncode == 129, f"Unexpected hangup exit: {refused.returncode}"
    original_preserved()
    report.update(corruptDownloadRefused=True, corruptDownloadLeftExistingAppUntouched=True,
                  failedReplacementRestoredPreviousApp=True, failedReplacementPreservedAttributes=True,
                  hangupAfterBackupRestoredPreviousApp=True, failedUpdatesCleanedWorkAndLock=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--installer", type=Path, default=ROOT / "install.sh")
    parser.add_argument("--version", help="Published release to verify; defaults to the installer's selected release")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--bootstrap-url", default="https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh")
    parser.add_argument("--launch", action="store_true", help="Launch the real default show; use only on a fresh ephemeral CI Mac")
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("Run installer verification on native macOS")
    architecture = {"arm64": "arm64", "x86_64": "amd64"}.get(platform.machine().lower())
    if not architecture:
        parser.error("Unsupported native Mac architecture")
    local_script = args.installer.read_text()
    default_match = re.search(r'version=\$\{SMARTSTAGE_VERSION:-(v[0-9A-Za-z._-]+)\}', local_script)
    if not default_match:
        parser.error("Could not read the installer's default release")
    default_version = default_match[1]
    args.version = args.version or default_version
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.]+)?", args.version):
        parser.error("Invalid release version")
    output = args.output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    report = {"status": "incomplete", "target": f"darwin/{architecture}",
              "release": args.version, "macOSVersion": platform.mac_ver()[0]}
    config_marker = None
    try:
        expected_bootstrap = args.installer.read_bytes()
        bootstrap_url = urllib.parse.urlsplit(args.bootstrap_url)
        # Raw GitHub branch updates can briefly lag the push in another region.
        # Keep exact-byte verification, but allow propagation and avoid caching
        # an earlier branch snapshot under the expected installer hash.
        for attempt in range(10):
            query = urllib.parse.parse_qsl(bootstrap_url.query, keep_blank_values=True)
            query.append(("smartstage_verify", f"{digest(expected_bootstrap)}-{time.time_ns()}"))
            verification_url = urllib.parse.urlunsplit(bootstrap_url._replace(query=urllib.parse.urlencode(query)))
            raw = fetch(verification_url, no_cache=True)
            if raw == expected_bootstrap:
                break
            if attempt < 9:
                time.sleep(3)
        assert raw == expected_bootstrap, (
            f"Published bootstrap differs from this checkout: expected {digest(expected_bootstrap)}, "
            f"received {digest(raw)} from {verification_url}"
        )
        script = raw.decode("utf-8")
        report.update(bootstrapURL=args.bootstrap_url, bootstrapSHA256=digest(raw),
                      bootstrapVerificationURL=verification_url,
                      bootstrapFetchAttempts=attempt + 1,
                      publishedBootstrapMatchesCheckout=True, pipedShellEntryPoint=True)
        with tempfile.TemporaryDirectory(prefix="smartstage-macos-install-") as temporary:
            scratch = Path(temporary).resolve()
            destination = scratch / "Operator's Applications – 演出"
            destination.mkdir()
            bundle = destination / BUNDLE_NAME
            sibling = destination / "Leave this file alone.txt"
            sibling.write_text("sibling survives installation\n")
            xattr_write(sibling, QUARANTINE, QUARANTINE_VALUE)
            xattr_write(sibling, CUSTOM_ATTRIBUTE, CUSTOM_VALUE)
            config = Path.home() / "Library/Application Support/SmartStage"
            config.mkdir(parents=True, exist_ok=True)
            with tempfile.NamedTemporaryFile(prefix="installer-preservation-", dir=config, delete=False) as f:
                config_marker = Path(f.name)
                f.write(b"operator configuration must survive\n")
            xattr_write(config_marker, CUSTOM_ATTRIBUTE, CUSTOM_VALUE)
            expected = official_bundle(scratch, args.version, architecture, report)
            default_firewall_globals = firewall_globals()
            default_firewall_apps = firewall_apps()
            first_install = install(script, destination, None if args.version == default_version else args.version)
            if gateway_default(args.version):
                assert "Public gateway mode needs no incoming-connection firewall rule" in first_install.stdout
                report["defaultGatewayInstallNeedsNoFirewallAuthorization"] = True
            else:
                assert "Firewall setup skipped (SMARTSTAGE_SKIP_FIREWALL=1)" in first_install.stdout
                report.update(legacyLANRelease=True, firewallSkipOptionRespected=True)
            assert firewall_globals() == default_firewall_globals, "Default install changed global firewall settings"
            assert firewall_apps() == default_firewall_apps, "Default install changed application firewall rules"
            report["defaultInstallFirewallRulesUnchanged"] = gateway_default(args.version)
            report["installationCheckFirewallRulesUnchanged"] = True
            assert bundle.is_dir() and not bundle.is_symlink()
            assert_no_quarantine(bundle)
            verify_bundle(bundle, expected, architecture, args.version, scratch, report)
            require_free_default_ports()
            report.update(noLaunchOptionRespected=True,
                          defaultReleaseSelectionTested=args.version == default_version)

            install(quarantined_installer(script), destination, args.version)
            assert_no_quarantine(bundle)
            assert xattr_read(bundle, CUSTOM_ATTRIBUTE) == CUSTOM_VALUE
            assert xattr_read(bundle / "Contents/MacOS/smartstage", CUSTOM_ATTRIBUTE) == CUSTOM_VALUE
            assert digest((bundle / "Contents/MacOS/smartstage").read_bytes()) == expected["core"]
            run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(bundle)])
            assert sibling.read_text() == "sibling survives installation\n"
            assert xattr_read(sibling, QUARANTINE) == QUARANTINE_VALUE
            assert xattr_read(sibling, CUSTOM_ATTRIBUTE) == CUSTOM_VALUE
            assert config_marker.read_bytes() == b"operator configuration must survive\n"
            assert xattr_read(config_marker, CUSTOM_ATTRIBUTE) == CUSTOM_VALUE
            report.update(reinstallationPassed=True, realQuarantineFixtureInjectedBeforeCleanup=True,
                          nestedQuarantineRemoved=True, unrelatedBundleAttributesPreserved=True,
                          siblingQuarantineUntouched=True, siblingFilePreserved=True,
                          existingConfigurationUntouched=True)
            refusal_checks(script, scratch, bundle, args.version, report)
            failed_update_checks(script, destination, args.version, report)
            assert sibling.read_text() == "sibling survives installation\n"
            assert xattr_read(sibling, QUARANTINE) == QUARANTINE_VALUE
            assert config_marker.read_bytes() == b"operator configuration must survive\n"
            firewall_checks(script, destination, args.version, report)
            if args.launch:
                default_launch(script, destination, args.version, report)
            else:
                report["installerLaunchNotRequested"] = True
        report["status"] = "passed"
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        if config_marker:
            config_marker.unlink(missing_ok=True)
        output.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
