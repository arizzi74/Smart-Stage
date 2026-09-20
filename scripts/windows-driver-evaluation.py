#!/usr/bin/env python3
"""One-off hosted-runner evaluation of the verified vendor driver installer.

This is not shipped with Smart Stage or called by its installers. The pinned
Pack45 setup recognizes -i (install) and -h (hide its own UI), confirmed in the
verified installer's command parser and install-command handler. OS security
prompts, policies, certificates and reboot state are not changed.
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
        'vendorRequiresReboot':True,'securitySettingsChanged':False,
        'installerArguments':['-i','-h'],'uiSnapshots':[]}
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
process=subprocess.Popen([str(setup),*report['installerArguments']],cwd=setup.parent)
started=time.monotonic(); previous=None
try:
    while time.monotonic()-started < 90 and process.poll() is None:
        rows=own_windows(process.pid)
        view=[{k:v for k,v in row.items() if k!='handle'} for row in rows]
        if view!=previous:
            report['uiSnapshots'].append({'seconds':round(time.monotonic()-started,2),'windows':view})
            previous=view; save()
        time.sleep(.5)
    report['installerExitCode']=process.poll()
    report['installerTimedOut']=process.poll() is None
    if report['installerExitCode']==0:
        # Device enumeration can settle after the vendor installer exits. This
        # bounded observation does not reboot or alter any OS security controls.
        endpoint_deadline=time.monotonic()+30
        while True:
            report['after']=devices()
            if any('CABLE' in item['name'].upper() for item in report['after']['audio']):
                report['status']='endpoint-available-before-reboot'
                break
            if time.monotonic()>=endpoint_deadline:
                break
            time.sleep(.5)
    if report['status']=='incomplete':
        report.update(status='unavailable',reason='No successful installer exit with an active CABLE endpoint before reboot')
        report['after']=devices()
except Exception as error:
    report.update(status='evaluation-error',reason=str(error))
    raise
finally:
    report['elapsedSeconds']=round(time.monotonic()-started,2)
    save()
    # This is our setup process on a disposable VM, never an operator application.
    if process.poll() is None:
        process.terminate(); process.wait(timeout=10)
    if os.environ.get('GITHUB_OUTPUT'):
        with Path(os.environ['GITHUB_OUTPUT']).open('a') as output:
            output.write('available='+str(report['status']=='endpoint-available-before-reboot').lower()+'\n')
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with Path(os.environ['GITHUB_STEP_SUMMARY']).open('a') as output:
            output.write('Virtual audio evaluation: **'+report['status']+'**.\n\n')
            output.write('Vendor reboot requirement remains unmet; no physical audio was verified. ')
            if report['status']!='endpoint-available-before-reboot':
                output.write('Native audio tests are skipped because the endpoint is unavailable. ')
            output.write('\n')
print(json.dumps(report,indent=2))
