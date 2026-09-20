#!/usr/bin/env python3
"""Run an actual native Linux gateway executable without installing services."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()
    version = subprocess.check_output([binary, "version"], text=True).strip()
    licenses = subprocess.check_output([binary, "licenses"], text=True)
    assert "github.com/coder/websocket" in licenses and "Permission to use" in licenses
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    with tempfile.TemporaryDirectory(prefix="smartstage-gateway-check-") as directory:
        config = Path(directory) / "config.json"
        initialized = subprocess.run(
            [binary, "init", "--config", config, "--public-url", "https://gateway.example.invalid/smartstage", "--listen", f"127.0.0.1:{port}"],
            text=True, capture_output=True, timeout=5, check=True,
        )
        settings = json.loads(config.read_text())
        assert len(settings["token"]) == 64 and settings["token"] in initialized.stdout
        assert config.stat().st_mode & 0o777 == 0o600
        duplicate = subprocess.run([binary, "init", "--config", config, "--public-url", "https://other.example.invalid/smartstage"], capture_output=True, timeout=5)
        assert duplicate.returncode != 0 and json.loads(config.read_text())["token"] == settings["token"]
        # This is the installer's dedicated-group service configuration mode.
        config.chmod(0o640)
        daemon = subprocess.Popen([binary, "serve", "--config", config], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        health_url = f"http://127.0.0.1:{port}/smartstage/health"
        try:
            deadline = time.monotonic() + 5
            while True:
                assert daemon.poll() is None, "daemon exited before readiness"
                try:
                    with opener.open(health_url, timeout=0.2) as response:
                        assert response.status == 200 and response.read() == b"ok\n"
                    break
                except (urllib.error.URLError, TimeoutError):
                    if time.monotonic() >= deadline:
                        raise AssertionError("gateway health never became ready")
                    time.sleep(0.02)
            try:
                opener.open(f"http://127.0.0.1:{port}/smartstage/e/{'a' * 32}/command", timeout=1)
                raise AssertionError("plaintext request without trusted proxy origin was accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 403
            daemon.send_signal(signal.SIGTERM)
            stdout, stderr = daemon.communicate(timeout=5)
            assert daemon.returncode == 0, stderr
            assert settings["token"] not in stdout + stderr, "daemon logged its registration secret"
        finally:
            if daemon.poll() is None:
                daemon.kill()
                daemon.communicate(timeout=5)
        config.chmod(0o644)
        unsafe = subprocess.run([binary, "serve", "--config", config], text=True, capture_output=True, timeout=5)
        assert unsafe.returncode != 0 and "private" in unsafe.stderr
    report = {
        "binary": binary.name,
        "sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "version": version,
        "actualDaemonProcess": True,
        "privateConfigurationAndExistingTokenPreserved": True,
        "dedicatedGroupConfigurationAccepted": True,
        "worldReadableConfigurationRejected": True,
        "loopbackHealthReady": True,
        "plaintextWithoutProxyOriginRejected": True,
        "cleanSIGTERMShutdown": True,
        "registrationSecretNotLogged": True,
        "embeddedDependencyLicense": True,
    }
    encoded = json.dumps(report, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded)
    print(encoded, end="")


if __name__ == "__main__":
    main()
