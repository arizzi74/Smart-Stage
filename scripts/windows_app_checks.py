#!/usr/bin/env python3
"""Read-only checks for the shipped Windows GUI and its retained Admin window.

Window checks inspect Win32 state, not WebView JavaScript or physical display
output. Native chooser, drag-and-drop and page behavior have a separate probe.
"""
import ctypes
import os
from pathlib import Path
import re
import struct
import time


ADMIN_CLASS = "SmartStageAdmin-View"
ADMIN_TITLE = "Smart Stage — Admin"


def dedicated_windows_release(version):
    """Whether this release is expected to include the Windows dedicated app."""
    if version == "dev":
        return True
    match = re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?", version)
    if not match:
        raise ValueError(f"Unrecognized Smart Stage version: {version!r}")
    core = tuple(int(match[index]) for index in (1, 2, 3))
    if core != (0, 1, 0):
        return core > (0, 1, 0)
    preview = re.fullmatch(r"preview\.(\d+)(?:\.[0-9A-Za-z.-]+)?", match[4] or "")
    if preview:
        return int(preview[1]) >= 17
    return match[4] is None or match[4].split(".")[0] > "preview"


def inspect_gui_executable(path):
    """Assert GUI-subsystem PE32+ metadata without launching the executable."""
    path = Path(path)
    with path.open("rb") as source:
        length = os.fstat(source.fileno()).st_size
        header = source.read(64)
        assert len(header) == 64 and header[:2] == b"MZ", "Windows executable is missing its DOS header"
        offset = struct.unpack_from("<I", header, 0x3C)[0]
        assert 64 <= offset <= length - 24, "Windows PE header lies outside the executable"
        source.seek(offset)
        coff = source.read(24)
        assert coff[:4] == b"PE\0\0", "Windows executable has an invalid PE signature"
        machine, sections = struct.unpack_from("<HH", coff, 4)
        optional_size, characteristics = struct.unpack_from("<HH", coff, 20)
        assert machine in (0x8664, 0xAA64), "Expected an amd64 or ARM64 Windows executable"
        assert sections and characteristics & 0x0002 and not characteristics & 0x2000, "Expected an executable image, not a DLL"
        assert 136 <= optional_size <= length - offset - 24, "Windows optional header is truncated"
        optional = source.read(optional_size)
        magic = struct.unpack_from("<H", optional)[0]
        subsystem = struct.unpack_from("<H", optional, 68)[0]
        directories = struct.unpack_from("<I", optional, 108)[0]
        assert magic == 0x20B, "Expected a 64-bit PE32+ executable"
        assert subsystem == 2, f"Expected Windows GUI subsystem 2; got {subsystem}"
        assert directories >= 3, "Executable has no resource directory for its app icon"
        resource_rva, resource_size = struct.unpack_from("<II", optional, 128)
        assert resource_rva and resource_size, "Executable has no embedded resource data"
    return {"format": "PE32+", "architecture": "amd64" if machine == 0x8664 else "arm64",
            "subsystem": "Windows GUI", "subsystemID": subsystem, "guiSubsystemVerified": True,
            "consoleSubsystem": False, "embeddedResourceDirectoryPresent": True,
            "resourceDirectoryRVA": resource_rva, "resourceDirectoryBytes": resource_size}


def _windows_api():
    if os.name != "nt":
        raise RuntimeError("Native Windows window inspection must run on Windows")
    from ctypes import wintypes
    user = ctypes.WinDLL("user32", use_last_error=True)
    gdi = ctypes.WinDLL("gdi32", use_last_error=True)
    callback_type = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
    user.EnumWindows.argtypes = [callback_type, wintypes.LPARAM]
    user.EnumWindows.restype = wintypes.BOOL
    user.GetWindowThreadProcessId.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.DWORD)]
    user.GetWindowThreadProcessId.restype = wintypes.DWORD
    user.GetClassNameW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    user.GetClassNameW.restype = ctypes.c_int
    user.GetWindowTextW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    user.GetWindowTextW.restype = ctypes.c_int
    for name in ("IsWindowVisible", "IsIconic"):
        function = getattr(user, name)
        function.argtypes = [wintypes.HWND]
        function.restype = wintypes.BOOL
    user.GetClientRect.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.RECT)]
    user.GetClientRect.restype = wintypes.BOOL
    user.SendMessageTimeoutW.argtypes = [wintypes.HWND, wintypes.UINT, wintypes.WPARAM,
                                        wintypes.LPARAM, wintypes.UINT, wintypes.UINT,
                                        ctypes.POINTER(ctypes.c_size_t)]
    user.SendMessageTimeoutW.restype = ctypes.c_ssize_t
    user.GetClassLongPtrW.argtypes = [wintypes.HWND, ctypes.c_int]
    user.GetClassLongPtrW.restype = ctypes.c_size_t
    gdi.DeleteObject.argtypes = [wintypes.HANDLE]
    gdi.DeleteObject.restype = wintypes.BOOL
    return user, gdi, callback_type


def _icon_metadata(user, gdi, hwnd):
    from ctypes import wintypes

    class IconInfo(ctypes.Structure):
        _fields_ = [("fIcon", wintypes.BOOL), ("xHotspot", wintypes.DWORD),
                    ("yHotspot", wintypes.DWORD), ("hbmMask", wintypes.HBITMAP),
                    ("hbmColor", wintypes.HBITMAP)]

    class Bitmap(ctypes.Structure):
        _fields_ = [("bmType", wintypes.LONG), ("bmWidth", wintypes.LONG),
                    ("bmHeight", wintypes.LONG), ("bmWidthBytes", wintypes.LONG),
                    ("bmPlanes", wintypes.WORD), ("bmBitsPixel", wintypes.WORD),
                    ("bmBits", ctypes.c_void_p)]

    user.GetIconInfo.argtypes = [wintypes.HICON, ctypes.POINTER(IconInfo)]
    user.GetIconInfo.restype = wintypes.BOOL
    gdi.GetObjectW.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p]
    gdi.GetObjectW.restype = ctypes.c_int
    handles = []
    for kind, name in ((1, "windowBig"), (0, "windowSmall"), (2, "windowSmall2")):
        value = ctypes.c_size_t()
        if user.SendMessageTimeoutW(hwnd, 0x007F, kind, 0, 0x0003, 1000, ctypes.byref(value)) and value.value:
            handles.append((value.value, name))
    for index, name in ((-14, "classBig"), (-34, "classSmall")):
        handle = user.GetClassLongPtrW(hwnd, index)
        if handle:
            handles.append((handle, name))
    for handle, source in handles:
        info = IconInfo()
        if not user.GetIconInfo(handle, ctypes.byref(info)):
            continue
        try:
            bitmap = Bitmap()
            image = info.hbmColor or info.hbmMask
            if info.fIcon and image and gdi.GetObjectW(image, ctypes.sizeof(bitmap), ctypes.byref(bitmap)):
                width, height = bitmap.bmWidth, bitmap.bmHeight
                if not info.hbmColor:
                    height //= 2
                if width > 0 and height > 0:
                    return {"present": True, "source": source, "width": width, "height": height,
                            "artworkIdentityVerified": False}
        finally:
            for bitmap_handle in (info.hbmMask, info.hbmColor):
                if bitmap_handle:
                    gdi.DeleteObject(bitmap_handle)
    return None


def wait_for_admin_window(pid, timeout=60):
    """Return the sole visible, responsive, titled Admin HWND with a real icon."""
    assert isinstance(pid, int) and pid > 0, "Expected a live native process ID"
    user, gdi, callback_type = _windows_api()
    from ctypes import wintypes
    deadline = time.monotonic() + timeout
    last = []
    while time.monotonic() < deadline:
        windows = []

        @callback_type
        def visit(hwnd, _context):
            owner = wintypes.DWORD()
            user.GetWindowThreadProcessId(hwnd, ctypes.byref(owner))
            if owner.value == pid:
                class_name = ctypes.create_unicode_buffer(256)
                user.GetClassNameW(hwnd, class_name, len(class_name))
                if class_name.value == ADMIN_CLASS:
                    title = ctypes.create_unicode_buffer(512)
                    user.GetWindowTextW(hwnd, title, len(title))
                    windows.append({"pid": pid, "hwnd": int(hwnd), "className": class_name.value,
                                    "title": title.value, "visible": bool(user.IsWindowVisible(hwnd)),
                                    "minimized": bool(user.IsIconic(hwnd))})
            return True

        if not user.EnumWindows(visit, 0):
            raise ctypes.WinError(ctypes.get_last_error())
        assert len(windows) <= 1, f"Multiple native Admin windows exist for PID {pid}: {windows}"
        last = windows
        if windows:
            window = windows[0]
            hwnd = window["hwnd"]
            rectangle, response = wintypes.RECT(), ctypes.c_size_t()
            if (window["visible"] and not window["minimized"] and window["title"] == ADMIN_TITLE
                    and user.GetClientRect(hwnd, ctypes.byref(rectangle))
                    and rectangle.right > rectangle.left and rectangle.bottom > rectangle.top
                    and user.SendMessageTimeoutW(hwnd, 0, 0, 0, 0x0003, 1000, ctypes.byref(response))):
                icon = _icon_metadata(user, gdi, hwnd)
                if icon:
                    return {**window, "responsive": True, "singleAdminWindow": True,
                            "clientWidth": rectangle.right - rectangle.left,
                            "clientHeight": rectangle.bottom - rectangle.top,
                            "icon": icon, "nativePageJavaScriptVerified": False}
        time.sleep(0.1)
    raise AssertionError(f"Timed out waiting for visible {ADMIN_CLASS} with icon for PID {pid}: {last}")
