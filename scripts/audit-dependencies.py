#!/usr/bin/env python3
"""Reject application/compiler runtime dylibs and DLLs in release imports."""
import os
import pathlib
import re
import shutil
import subprocess
import sys

path = pathlib.Path(sys.argv[1])
if path.suffix == '.exe':
    tool = os.environ.get('OBJDUMP') or shutil.which('llvm-objdump') or shutil.which('x86_64-w64-mingw32-objdump')
    if not tool:
        raise SystemExit('Install llvm-objdump from the pinned LLVM-MinGW toolchain to audit imports.')
    output = subprocess.check_output([tool, '-p', str(path)], text=True)
    imports = re.findall(r'DLL Name:\s*(\S+)', output)
    os_dlls = {'kernel32.dll', 'user32.dll', 'gdi32.dll', 'advapi32.dll', 'ole32.dll',
               'oleaut32.dll', 'mf.dll', 'mfplat.dll', 'mfreadwrite.dll', 'evr.dll',
               'propsys.dll', 'shcore.dll', 'shell32.dll', 'ntdll.dll', 'bcrypt.dll',
               'ws2_32.dll', 'winmm.dll', 'ucrtbase.dll', 'msvcrt.dll', 'crypt32.dll',
               'secur32.dll', 'iphlpapi.dll', 'setupapi.dll', 'dwmapi.dll',
               'dcomp.dll'}  # Windows DirectComposition hosts the native Admin WebView.
    bad = [i for i in imports if i.lower() not in os_dlls and not i.lower().startswith(('api-ms-win-', 'ext-ms-win-'))]
else:
    output = subprocess.check_output(['otool', '-L', str(path)], text=True)
    imports = [line.strip().split(' (')[0] for line in output.splitlines()[1:] if line.strip()]
    bad = [i for i in imports if not i.startswith(('/System/Library/', '/usr/lib/'))]
path.with_name(path.name + '.imports.txt').write_text(output)
if not imports:
    raise SystemExit('No dynamic imports found; dependency audit cannot establish correctness.')
if bad:
    raise SystemExit('Non-OS dependencies are forbidden: ' + ', '.join(bad))
print(f'{path}: imports only OS libraries ({len(imports)} entries). Physical clean-machine test still required.')
