"""Development-only Windows desktop capture using documented GDI APIs.

No application hooks, synthetic frames, driver installation or desktop changes.
"""
import ctypes
from ctypes import wintypes
from pathlib import Path
import struct
import zlib

def capture_display(path, display):
    user = ctypes.WinDLL('user32',use_last_error=True)
    gdi = ctypes.WinDLL('gdi32',use_last_error=True)
    user.SetProcessDpiAwarenessContext.argtypes=[wintypes.HANDLE]
    user.SetProcessDpiAwarenessContext.restype=wintypes.BOOL
    user.SetProcessDpiAwarenessContext(wintypes.HANDLE(-4))
    user.GetDC.argtypes=[wintypes.HWND]
    user.GetDC.restype=wintypes.HDC
    user.ReleaseDC.argtypes=[wintypes.HWND,wintypes.HDC]
    user.ReleaseDC.restype=ctypes.c_int
    gdi.CreateCompatibleDC.argtypes=[wintypes.HDC]
    gdi.CreateCompatibleDC.restype=wintypes.HDC
    gdi.CreateCompatibleBitmap.argtypes=[wintypes.HDC,ctypes.c_int,ctypes.c_int]
    gdi.CreateCompatibleBitmap.restype=wintypes.HANDLE
    gdi.SelectObject.argtypes=[wintypes.HDC,wintypes.HANDLE]
    gdi.SelectObject.restype=wintypes.HANDLE
    gdi.BitBlt.argtypes=[wintypes.HDC,ctypes.c_int,ctypes.c_int,ctypes.c_int,ctypes.c_int,
                        wintypes.HDC,ctypes.c_int,ctypes.c_int,wintypes.DWORD]
    gdi.BitBlt.restype=wintypes.BOOL
    gdi.GetDIBits.argtypes=[wintypes.HDC,wintypes.HANDLE,wintypes.UINT,wintypes.UINT,
                           ctypes.c_void_p,ctypes.c_void_p,wintypes.UINT]
    gdi.GetDIBits.restype=ctypes.c_int
    gdi.DeleteObject.argtypes=[wintypes.HANDLE]
    gdi.DeleteObject.restype=wintypes.BOOL
    gdi.DeleteDC.argtypes=[wintypes.HDC]
    gdi.DeleteDC.restype=wintypes.BOOL

    class Header(ctypes.Structure):
        _fields_=[('size',wintypes.DWORD),('width',wintypes.LONG),('height',wintypes.LONG),
                  ('planes',wintypes.WORD),('bits',wintypes.WORD),('compression',wintypes.DWORD),
                  ('imageBytes',wintypes.DWORD),('xPixelsPerMeter',wintypes.LONG),
                  ('yPixelsPerMeter',wintypes.LONG),('colorsUsed',wintypes.DWORD),('importantColors',wintypes.DWORD)]
    width,height=display['width'],display['height']
    if not (0 < width <= 16384 and 0 < height <= 16384):
        raise RuntimeError('Unsupported desktop capture dimensions')
    screen=memory=bitmap=previous=None
    selected=False
    try:
        screen=user.GetDC(None)
        if not screen: raise ctypes.WinError(ctypes.get_last_error())
        memory=gdi.CreateCompatibleDC(screen)
        if not memory: raise ctypes.WinError(ctypes.get_last_error())
        bitmap=gdi.CreateCompatibleBitmap(screen,width,height)
        if not bitmap: raise ctypes.WinError(ctypes.get_last_error())
        previous=gdi.SelectObject(memory,bitmap)
        if not previous: raise ctypes.WinError(ctypes.get_last_error())
        selected=True
        # SRCCOPY | CAPTUREBLT includes layered windows in the composed desktop.
        if not gdi.BitBlt(memory,0,0,width,height,screen,display['x'],display['y'],0x00CC0020|0x40000000):
            raise ctypes.WinError(ctypes.get_last_error())
        gdi.SelectObject(memory,previous); selected=False
        # GetDIBits requires the bitmap not to be selected into a device context.
        header=Header(size=ctypes.sizeof(Header),width=width,height=-height,planes=1,bits=32)
        pixels=ctypes.create_string_buffer(width*height*4)
        if gdi.GetDIBits(memory,bitmap,0,height,pixels,ctypes.byref(header),0) != height:
            raise ctypes.WinError(ctypes.get_last_error())
        raw=pixels.raw
    finally:
        if selected: gdi.SelectObject(memory,previous)
        if bitmap: gdi.DeleteObject(bitmap)
        if memory: gdi.DeleteDC(memory)
        if screen: user.ReleaseDC(None,screen)
    # The desktop DIB is BGRX; its fourth byte is unused, not transparency.
    rgba=bytearray(len(raw))
    rgba[0::4],rgba[1::4],rgba[2::4],rgba[3::4]=raw[2::4],raw[1::4],raw[0::4],b'\xff'*(width*height)
    scanlines=b''.join(b'\0'+rgba[y*width*4:(y+1)*width*4] for y in range(height))
    def chunk(kind,data):
        return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data))
    Path(path).write_bytes(b'\x89PNG\r\n\x1a\n'+
        chunk(b'IHDR',struct.pack('>IIBBBBB',width,height,8,6,0,0,0))+
        chunk(b'IDAT',zlib.compress(scanlines,3))+chunk(b'IEND',b''))
