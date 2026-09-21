#!/usr/bin/env python3
"""Verify each gateway build's bootstrap downloads that exact release."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


def executable(path, contents):
    path.write_text(contents)
    path.chmod(0o755)


class GatewayBuildTest(unittest.TestCase):
    def test_bootstrap_pins_stable_and_prerelease_builds(self):
        template = (ROOT / "install-gateway.sh").read_bytes()
        for version in ("v0.1.0-preview.22", "v1.0.0"):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                tools = directory / "tools"
                tools.mkdir()
                output = directory / "release"
                payload = directory / "payload"
                executable(payload, '#!/bin/sh\nprintf "%s\\n" "$@" > "$TEST_INSTALL_ARGS"\n')
                executable(tools / "go", """#!/bin/sh
set -eu
if [ "$1" = version ]; then exit 0; fi
while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then shift; cp "$TEST_BUILD_PAYLOAD" "$1"; exit 0; fi
    shift
done
exit 1
""")
                executable(tools / "readelf", "#!/bin/sh\nexit 0\n")
                executable(tools / "curl", """#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) shift; destination=$1 ;;
        https://*) url=$1 ;;
    esac
    shift
done
printf "%s\\n" "$url" >> "$TEST_DOWNLOADS"
cp "$TEST_RELEASE_DIR/${url##*/}" "$destination"
""")
                executable(tools / "sudo", '#!/bin/sh\nexec "$@"\n')
                executable(tools / "uname", """#!/bin/sh
case "$1" in
    -s) printf "Linux\\n" ;;
    -m) printf "%s\\n" "$TEST_MACHINE" ;;
    *) exit 1 ;;
esac
""")
                environment = {
                    **os.environ,
                    "PATH": str(tools) + os.pathsep + os.environ["PATH"],
                    "VERSION": version,
                    "TEST_BUILD_PAYLOAD": str(payload),
                    "TEST_RELEASE_DIR": str(output),
                }
                subprocess.run(
                    ["sh", str(ROOT / "scripts/build-gateway.sh"), str(output)],
                    env=environment, check=True, capture_output=True, text=True, timeout=30,
                )
                bootstrap = output / "install-gateway.sh"
                subprocess.run(["sh", "-n", str(bootstrap)], check=True, timeout=10)
                self.assertEqual((ROOT / "install-gateway.sh").read_bytes(), template)
                for machine, architecture in (("x86_64", "amd64"), ("aarch64", "arm64")):
                    with self.subTest(architecture=architecture):
                        downloads = directory / ("downloads-" + architecture)
                        arguments = directory / ("arguments-" + architecture)
                        result = subprocess.run(
                            ["sh", str(bootstrap), "--list-hosts"],
                            env={
                                **environment, "TEST_MACHINE": machine,
                                "TEST_DOWNLOADS": str(downloads),
                                "TEST_INSTALL_ARGS": str(arguments),
                            },
                            check=True, capture_output=True, text=True, timeout=30,
                        )
                        asset = "smartstage-gateway-linux-" + architecture
                        base = "https://github.com/arizzi74/Smart-Stage/releases/download/" + version
                        self.assertEqual(downloads.read_text().splitlines(), [
                            base + "/" + asset, base + "/" + asset + ".sha256",
                        ])
                        self.assertEqual(arguments.read_text().splitlines(), ["install", "--list-hosts"])
                        self.assertIn(version + " for Linux " + architecture, result.stdout)
                        self.assertEqual(
                            (output / (asset + ".sha256")).read_text().split()[0],
                            hashlib.sha256((output / asset).read_bytes()).hexdigest(),
                        )


if __name__ == "__main__":
    unittest.main()

