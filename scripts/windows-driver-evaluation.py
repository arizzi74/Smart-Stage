#!/usr/bin/env python3
"""One-off hosted-runner evaluation of the verified vendor driver installer.

This is not shipped with Smart Stage or called by its installers. It only clicks
an explicitly named native button belonging to the verified setup process.
Security prompts, policies, certificates and reboot state are not changed.
"""
import ctypes
from ctypes import wintypes
import json
import os
from pathlib import Path
import subprocess
import sys
import time

assert os.name == 'nt' and os.environ.get('GITHUB_ACTIONS') == 'true'
assert os.environ.get('SMARTSTAGE_EPHEMERAL_AUDIO_EVALUATION') == '1'
setup,harness,directory=map(lambda value:Path(value).resolve(),sys.argv[1:4])
directory.mkdir(parents=True,exist_ok=True)
report={'status':'incomplete','physicalAudioVerified':False,'rebootPerformed':False,
        'vendorRequiresReboot':True,'securitySettingsChanged':False,'uiSnapshots':[]}
user=ctypes.WinDLL('user32',use_last_error=True)
callback_type=ctypes.WINFUNCTYPE(wintypes.BOOL,wintypes.HWND,wintypes.LPARAM)
def signature(name,arguments,result):
    function=getattr(user,name); function.argtypes=arguments; function.restype=result
signature('EnumWindows',[callback_type,wintypes.LPARAM],wintypes.BOOL)
signature('EnumChildWindows',[wintypes.HWND,callback_type,wintypes.LPARAM],wintypes.BOOL)
signature('GetWindowThreadProcessId',[wintypes.HWND,ctypes.POINTER(wintypes.DWORD)],wintypes.DWORD)
signature('GetWindowTextW',[wintypes.HWND,wintypes.LPWSTR,ctypes.c_int],ctypes.c_int)
signature('GetClassNameW',[wintypes.HWND,wintypes.LPWSTR,ctypes.c_int],ctypes.c_int)
signature('IsWindowEnabled',[wintypes.HWND],wintypes.BOOL)
signature('IsWindowVisible',[wintypes.HWND],wintypes.BOOL)
signature('PostMessageW',[wintypes.HWND,wintypes.UINT,wintypes.WPARAM,wintypes.LPARAM],wintypes.BOOL)

def devices():
    return json.loads(subprocess.check_output([str(harness),'--list'],text=True,timeout=20))
def save():
    (directory/'driver-evaluation.json').write_text(json.dumps(report,indent=2)+'\n')
def own_windows(pid):
    rows=[]
    def row(window):
        owner=wintypes.DWORD(); user.GetWindowThreadProcessId(window,ctypes.byref(owner))
        if owner.value!=pid: return None
        title,kind=ctypes.create_unicode_buffer(2048),ctypes.create_unicode_buffer(256)
        user.GetWindowTextW(window,title,len(title)); user.GetClassNameW(window,kind,len(kind))
        return {'handle':window,'class':kind.value,'text':title.value,
                'enabled':bool(user.IsWindowEnabled(window)),'visible':bool(user.IsWindowVisible(window))}
    @callback_type
    def child(window,unused):
        item=row(window)
        if item: rows.append(item)
        return True
    @callback_type
    def top(window,unused):
        item=row(window)
        if item:
            rows.append(item); user.EnumChildWindows(window,child,0)
        return True
    user.EnumWindows(top,0)
    return rows

report['before']=devices()
assert not report['before']['audio'], 'Evaluation requires a fresh runner without audio endpoints'
process=subprocess.Popen([str(setup)],cwd=setup.parent)
started=time.monotonic(); clicked=False; previous=None
try:
    while time.monotonic()-started < 90 and process.poll() is None:
        rows=own_windows(process.pid)
        view=[{k:v for k,v in row.items() if k!='handle'} for row in rows]
        if view!=previous:
            report['uiSnapshots'].append({'seconds':round(time.monotonic()-started,2),'windows':view})
            previous=view; save()
        main=any(row['class']=='VBCABLE0Installer0MainWindow0' for row in rows)
        buttons=[row for row in rows if row['class'].lower()=='button' and
                 row['text'].replace('&','')=='Install Driver' and row['visible'] and row['enabled']]
        if main and len(buttons)==1 and not clicked:
            # BM_CLICK, targeted only at this verified installer's named button.
            if not user.PostMessageW(buttons[0]['handle'],0x00F5,0,0):
                raise ctypes.WinError(ctypes.get_last_error())
            clicked=True; report['installButtonRequested']=True; save()
        complete=any('Installation Complete and Successful' in row['text'] for row in rows)
        if complete:
            report['after']=devices()
            if any('CABLE' in item['name'].upper() for item in report['after']['audio']):
                report['status']='endpoint-available-before-reboot'
                break
        time.sleep(.5)
    if report['status']=='incomplete':
        report.update(status='unavailable',reason='Documented installer UI did not yield an observable completed installation and audio endpoint')
        report['after']=devices()
finally:
    report['elapsedSeconds']=round(time.monotonic()-started,2)
    save()
    # This is our setup process on a disposable VM, never an operator application.
    if process.poll() is None:
        process.terminate(); process.wait(timeout=10)
    if os.environ.get('GITHUB_OUTPUT'):
        with Path(os.environ['GITHUB_OUTPUT']).open('a') as output:
            output.write('available='+str(report['status']=='endpoint-available-before-reboot').lower()+'\n')
print(json.dumps(report,indent=2))
