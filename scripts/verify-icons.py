#!/usr/bin/env python3
"""Check icons with native OS readers; optionally exercise Finder on a fresh CI Mac."""
import argparse
import hashlib
import json
from pathlib import Path
import plistlib
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import time
import urllib.request


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def windows_icon(executable, source):
    import ctypes
    from ctypes import wintypes

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.LoadLibraryExW.argtypes = [wintypes.LPCWSTR, wintypes.HANDLE, wintypes.DWORD]
    kernel.LoadLibraryExW.restype = wintypes.HMODULE
    kernel.FindResourceW.argtypes = [wintypes.HMODULE, ctypes.c_void_p, ctypes.c_void_p]
    kernel.FindResourceW.restype = wintypes.HANDLE
    kernel.SizeofResource.argtypes = [wintypes.HMODULE, wintypes.HANDLE]
    kernel.SizeofResource.restype = wintypes.DWORD
    kernel.LoadResource.argtypes = [wintypes.HMODULE, wintypes.HANDLE]
    kernel.LoadResource.restype = wintypes.HANDLE
    kernel.LockResource.argtypes = [wintypes.HANDLE]
    kernel.LockResource.restype = ctypes.c_void_p
    kernel.FreeLibrary.argtypes = [wintypes.HMODULE]
    module = kernel.LoadLibraryExW(str(executable), None, 0x00000002 | 0x00000020)
    if not module:
        raise ctypes.WinError(ctypes.get_last_error())

    def resource(identifier, kind):
        entry = kernel.FindResourceW(module, identifier, kind)
        if not entry:
            raise ctypes.WinError(ctypes.get_last_error())
        length = kernel.SizeofResource(module, entry)
        pointer = kernel.LockResource(kernel.LoadResource(module, entry))
        if not pointer or not length:
            raise RuntimeError("Empty icon resource")
        return ctypes.string_at(pointer, length)

    try:
        group = resource(1, 14)  # RT_GROUP_ICON
        original = source.read_bytes()
        assert group[:6] == original[:6], "Icon group differs from source ICO"
        reserved, kind, count = struct.unpack_from("<HHH", group)
        assert (reserved, kind, count) == (0, 1, 9)
        sizes = []
        for i in range(count):
            actual = struct.unpack_from("<BBBBHHIH", group, 6 + 14 * i)
            expected = struct.unpack_from("<BBBBHHII", original, 6 + 16 * i)
            # Pillow leaves PNG planes unspecified (0); windres correctly
            # normalizes that field to one plane in RT_GROUP_ICON.
            assert expected[4] in (0, 1) and actual[4] == 1
            assert actual[:4] + actual[5:7] == expected[:4] + expected[5:7], "Embedded icon dimensions differ"
            assert resource(actual[7], 3) == original[expected[7]:expected[7] + expected[6]], "Embedded icon pixels differ"
            sizes.append(actual[0] or 256)
    finally:
        kernel.FreeLibrary(module)

    shell = ctypes.WinDLL("shell32", use_last_error=True)
    shell.ExtractIconExW.argtypes = [wintypes.LPCWSTR, ctypes.c_int, ctypes.POINTER(wintypes.HICON), ctypes.POINTER(wintypes.HICON), wintypes.UINT]
    shell.ExtractIconExW.restype = wintypes.UINT
    user = ctypes.WinDLL("user32", use_last_error=True)
    user.DestroyIcon.argtypes = [wintypes.HICON]
    large, small = wintypes.HICON(), wintypes.HICON()
    try:
        # Request each size separately: requesting both can return two icons,
        # although nIcons is one (one large/small pair).
        large_count = shell.ExtractIconExW(str(executable), 0, ctypes.byref(large), None, 1)
        small_count = shell.ExtractIconExW(str(executable), 0, None, ctypes.byref(small), 1)
        assert large_count == small_count == 1 and large.value and small.value, (
            f"Windows Shell icon extraction failed: large={large_count}/{large.value}, "
            f"small={small_count}/{small.value}, error={ctypes.get_last_error()}"
        )
    finally:
        if large.value:
            user.DestroyIcon(large)
        if small.value:
            user.DestroyIcon(small)
    return {"embeddedSizes": sizes, "sourcePixelsMatch": True, "shellExtractedLargeAndSmall": True,
            "shellLargeExtractCount": large_count, "shellSmallExtractCount": small_count}


def finder_launch(bundle):
    import ctypes
    import os

    libproc = ctypes.CDLL("/usr/lib/libproc.dylib")
    libproc.proc_pidpath.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32]
    libproc.proc_pidpath.restype = ctypes.c_int
    expected_executable = (bundle / "Contents/MacOS/smartstage").resolve()
    # This deliberately uses the real default user configuration. Opt in only
    # on an ephemeral CI runner, never silently start an operator's saved show.
    for port in (8787, 8788):
        with socket.socket() as probe:
            if probe.connect_ex(("127.0.0.1", port)) == 0:
                raise RuntimeError(f"Finder launch check needs port {port} to be unused")
    subprocess.run(["open", "-n", str(bundle)], check=True, timeout=15)
    pid = None
    observed = {}
    try:
        for _ in range(45):
            listeners = subprocess.run(["lsof", "-nP", "-iTCP:8787", "-sTCP:LISTEN", "-Fp"], text=True, capture_output=True)
            for line in listeners.stdout.splitlines():
                if not line.startswith("p"):
                    continue
                candidate = int(line[1:])
                # Read the executable path from the kernel: ps display output
                # can truncate or escape long paths and non-ASCII characters.
                path = ctypes.create_string_buffer(4096)
                if libproc.proc_pidpath(candidate, path, len(path)) <= 0:
                    continue
                observed[candidate] = os.fsdecode(path.value)
                if Path(observed[candidate]).resolve() == expected_executable:
                    pid = candidate
            if pid:
                try:
                    with urllib.request.urlopen("http://127.0.0.1:8787/admin", timeout=2) as response:
                        assert response.status == 200 and b"Smart Stage" in response.read()
                    return {"finderLaunchedTerminalAndCore": True, "servedAdminPage": True,
                            "kernelExecutablePathMatched": True}
                except (OSError, AssertionError):
                    pass
            time.sleep(1)
        raise RuntimeError(f"Finder launch did not start the bundled Smart Stage server in Terminal; listeners: {observed}")
    finally:
        if pid:
            try:
                os.kill(pid, signal.SIGINT)
            except ProcessLookupError:
                pass


def mac_icon(executable, source, test_finder):
    archive = executable.with_name(executable.name + ".app.zip")
    with tempfile.TemporaryDirectory(prefix="smartstage-icon-") as temporary:
        parent = Path(temporary).resolve() / "Operator's show – 演出"
        parent.mkdir()
        subprocess.run(["ditto", "-x", "-k", str(archive), str(parent)], check=True)
        bundle = parent / "Smart Stage.app"
        contents = bundle / "Contents"
        with (contents / "Info.plist").open("rb") as f:
            info = plistlib.load(f)
        icon = contents / "Resources" / info["CFBundleIconFile"]
        assert digest(icon) == digest(source), "Bundled icon differs from the source ICNS"
        assert digest(contents / "MacOS/smartstage") == digest(executable), "Bundle must contain the standalone release bytes"
        assert info["CFBundleExecutable"] == "SmartStageLauncher" and info["CFBundlePackageType"] == "APPL"
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(bundle)], check=True)
        decoded = parent / "decoded.iconset"
        subprocess.run(["iconutil", "--convert", "iconset", "--output", str(decoded), str(icon)], check=True)
        assert len(list(decoded.glob("*.png"))) >= 8, "Missing standard/Retina icon images"
        command = contents / "Resources/Start Smart Stage.command"
        version = subprocess.check_output([str(command), "--version"], text=True, timeout=15).strip()
        expected = subprocess.check_output([str(executable), "--version"], text=True, timeout=15).strip()
        assert version == expected, "Terminal command must run the bundled executable and forward arguments"
        result = {"archive": archive.name, "archiveSha256": digest(archive), "iconDecodedByMacOS": True,
                  "adHocSignatureVerified": True, "standaloneBytesMatch": True,
                  "spacesQuotesUnicodePathPassed": True, "version": version}
        if test_finder:
            result.update(finder_launch(bundle))
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("executable", type=Path)
    parser.add_argument("--finder-launch", action="store_true", help="Launch Terminal and the default show on an ephemeral CI Mac")
    args = parser.parse_args()
    executable = args.executable.resolve()
    icons = Path(__file__).resolve().parents[1] / "assets/icon"
    report = {"executable": executable.name, "sha256": digest(executable), "platform": sys.platform, "status": "incomplete"}
    try:
        if sys.platform == "win32":
            report.update(windows_icon(executable, icons / "smartstage.ico"))
        elif sys.platform == "darwin":
            report.update(mac_icon(executable, icons / "smartstage.icns", args.finder_launch))
        else:
            raise RuntimeError("Run this check on the executable's target OS")
        report["status"] = "passed"
    except Exception as error:
        report.update(status="failed", error=str(error))
        raise
    finally:
        executable.with_name(executable.name + ".icon.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
