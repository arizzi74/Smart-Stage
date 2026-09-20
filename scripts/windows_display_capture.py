"""Development-only Windows desktop capture using documented GDI APIs.

No application hooks, synthetic frames, driver installation or desktop changes.
"""
import ctypes
from ctypes import wintypes
import os
from pathlib import Path
import struct
import zlib

def desktop_context(harness_pid):
    """Read desktop/session/window identity without switching or dismissing UI."""
    user = ctypes.WinDLL('user32',use_last_error=True)
    kernel = ctypes.WinDLL('kernel32',use_last_error=True)
    dwm = ctypes.WinDLL('dwmapi',use_last_error=True)
    def signature(library,name,arguments,result):
        function=getattr(library,name)
        function.argtypes, function.restype=arguments,result
        return function
    signature(kernel,'GetCurrentThreadId',[],wintypes.DWORD)
    signature(kernel,'WTSGetActiveConsoleSessionId',[],wintypes.DWORD)
    signature(kernel,'ProcessIdToSessionId',[wintypes.DWORD,ctypes.POINTER(wintypes.DWORD)],wintypes.BOOL)
    signature(user,'GetThreadDesktop',[wintypes.DWORD],wintypes.HANDLE)
    signature(user,'GetProcessWindowStation',[],wintypes.HANDLE)
    signature(user,'GetUserObjectInformationW',[wintypes.HANDLE,ctypes.c_int,ctypes.c_void_p,
        wintypes.DWORD,ctypes.POINTER(wintypes.DWORD)],wintypes.BOOL)
    signature(user,'OpenInputDesktop',[wintypes.DWORD,wintypes.BOOL,wintypes.DWORD],wintypes.HANDLE)
    signature(user,'CloseDesktop',[wintypes.HANDLE],wintypes.BOOL)
    signature(user,'GetWindowThreadProcessId',[wintypes.HWND,ctypes.POINTER(wintypes.DWORD)],wintypes.DWORD)
    signature(user,'IsWindowVisible',[wintypes.HWND],wintypes.BOOL)
    signature(user,'GetForegroundWindow',[],wintypes.HWND)
    signature(user,'GetWindowRect',[wintypes.HWND,ctypes.POINTER(wintypes.RECT)],wintypes.BOOL)
    signature(user,'GetWindowTextW',[wintypes.HWND,wintypes.LPWSTR,ctypes.c_int],ctypes.c_int)
    signature(user,'GetClassNameW',[wintypes.HWND,wintypes.LPWSTR,ctypes.c_int],ctypes.c_int)
    signature(dwm,'DwmGetWindowAttribute',[wintypes.HWND,wintypes.DWORD,ctypes.c_void_p,wintypes.DWORD],ctypes.c_long)
    def session(pid):
        value=wintypes.DWORD()
        return value.value if kernel.ProcessIdToSessionId(pid,ctypes.byref(value)) else {'error':ctypes.get_last_error()}
    def identity(handle):
        if not handle: return {'error':ctypes.get_last_error()}
        name=ctypes.create_unicode_buffer(1024)
        needed=wintypes.DWORD()
        if not user.GetUserObjectInformationW(handle,2,name,ctypes.sizeof(name),ctypes.byref(needed)):
            return {'error':ctypes.get_last_error()}
        receiving=wintypes.BOOL()
        valid=user.GetUserObjectInformationW(handle,6,ctypes.byref(receiving),ctypes.sizeof(receiving),ctypes.byref(needed))
        return {'name':name.value,'receivingInput':bool(receiving.value) if valid else None}
    result={'observerPid':os.getpid(),'observerSession':session(os.getpid()),
        'harnessPid':harness_pid,'harnessSession':session(harness_pid),
        'activeConsoleSession':kernel.WTSGetActiveConsoleSessionId(),
        'windowStation':identity(user.GetProcessWindowStation()),
        'observerDesktop':identity(user.GetThreadDesktop(kernel.GetCurrentThreadId())),'windows':[]}
    # DESKTOP_READOBJECTS only; never request switching/writing permissions.
    desktop=user.OpenInputDesktop(0,False,0x0001)
    try: result['inputDesktop']=identity(desktop)
    finally:
        if desktop: user.CloseDesktop(desktop)
    foreground=user.GetForegroundWindow()
    callback_type=ctypes.WINFUNCTYPE(wintypes.BOOL,wintypes.HWND,wintypes.LPARAM)
    @callback_type
    def visit(window,unused):
        pid=wintypes.DWORD()
        thread=user.GetWindowThreadProcessId(window,ctypes.byref(pid))
        visible=bool(user.IsWindowVisible(window))
        if not visible and pid.value!=harness_pid: return True
        title,class_name=ctypes.create_unicode_buffer(256),ctypes.create_unicode_buffer(256)
        user.GetWindowTextW(window,title,len(title)); user.GetClassNameW(window,class_name,len(class_name))
        rectangle=wintypes.RECT(); user.GetWindowRect(window,ctypes.byref(rectangle))
        cloaked=wintypes.DWORD()
        hr=dwm.DwmGetWindowAttribute(window,14,ctypes.byref(cloaked),ctypes.sizeof(cloaked))
        result['windows'].append({'pid':pid.value,'thread':thread,'visible':visible,
            'foreground':window==foreground,'title':title.value,'class':class_name.value,
            'bounds':[rectangle.left,rectangle.top,rectangle.right,rectangle.bottom],
            'cloaked':cloaked.value if hr==0 else None,
            'desktop':identity(user.GetThreadDesktop(thread))})
        return len(result['windows'])<256
    signature(user,'EnumWindows',[callback_type,wintypes.LPARAM],wintypes.BOOL)
    user.EnumWindows(visit,0)
    return result

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
