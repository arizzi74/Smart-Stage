#!/usr/bin/env python3
"""Observe actual Mac runner display pixels through the native backend harness.

This is virtual-desktop evidence, not physical projector or latency acceptance.
Screen-capture permission is not changed. Unavailable capture is reported as such.
"""
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time

assert sys.platform == 'darwin', 'This diagnostic currently supports Mac runners'
exe, pixel_tool, destination = map(lambda s:Path(s).resolve(), sys.argv[1:4])
destination.mkdir(parents=True,exist_ok=True)
root = Path(__file__).resolve().parent.parent
report = {'status':'incomplete','physicalOutputsVerified':False,'latencyMeasured':False,'captures':[]}

def save():
    content=json.dumps(report,indent=2)+'\n'
    (destination/'result.json').write_text(content)
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        Path(os.environ['GITHUB_STEP_SUMMARY']).write_text('Native virtual-display observation\n\n```json\n'+content+'```\n')

def capture(name):
    path=destination/(name+'.png')
    result=subprocess.run(['screencapture','-x','-m','-t','png',str(path)],capture_output=True,text=True,timeout=10)
    if result.returncode:
        raise RuntimeError('Native screen capture unavailable: '+result.stderr.strip()[-500:])
    metrics=json.loads(subprocess.check_output([str(pixel_tool),str(path)],text=True,timeout=10))
    report['captures'].append({'image':path.name,**metrics})
    save()
    return metrics

report['harnessVersion']=subprocess.check_output([str(exe),'--version'],text=True,timeout=15).strip()
devices=json.loads(subprocess.check_output([str(exe),'--list'],text=True,timeout=30))
report['displayCount']=len(devices['displays'])
if len(devices['displays']) != 1:
    report.update(status='unavailable',reason='Diagnostic requires exactly one runner display')
    save(); sys.exit(0)
report['display']=devices['displays'][0]
try:
    capture('desktop-before')
except (RuntimeError,subprocess.SubprocessError,OSError) as error:
    report.update(status='unavailable',reason=str(error))
    save(); print('UNVERIFIED: '+str(error)); sys.exit(0)

events=queue.Queue()
errors=[]
process=subprocess.Popen([str(exe),'--file',str(root/'testdata/media/silent-1080p.mp4'),
    '--display',devices['displays'][0]['id'],'--exit-after','90s'],
    stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
def read_events():
    for line in process.stdout:
        try: events.put(json.loads(line))
        except json.JSONDecodeError: events.put({'kind':'invalid-output'})
def read_errors():
    for line in process.stderr:
        errors.append(line[-1000:])
        del errors[:-20]
threading.Thread(target=read_events,daemon=True).start()
threading.Thread(target=read_errors,daemon=True).start()

def wait_event(kind, timeout=45):
    deadline=time.monotonic()+timeout
    while time.monotonic()<deadline:
        if process.poll() is not None: raise AssertionError('Harness exited: '+''.join(errors))
        try: event=events.get(timeout=min(.2,max(0,deadline-time.monotonic())))
        except queue.Empty: continue
        if event['kind']=='error': raise AssertionError(event)
        if event['kind']==kind: return event
    raise AssertionError('No native '+kind+' event')

def command(value):
    process.stdin.write(value+'\n'); process.stdin.flush()

def observe_video(prefix):
    # Native playing may precede the first displayed frame by a short interval.
    for attempt in range(6):
        first=capture(prefix+f'-frame-{attempt}')
        if first['blackFraction']<.8: break
        time.sleep(.1)
    else: raise AssertionError('Captured output stayed black after native playing')
    for color in ('redFraction','greenFraction','blueFraction'):
        assert first[color]>.02, ('Missing fixture primary-color content',first)
    assert first['opaqueFraction']>.999, first
    time.sleep(.25)
    second=capture(prefix+'-later-frame')
    assert second['blackFraction']<.8 and second['opaqueFraction']>.999, second
    for color in ('redFraction','greenFraction','blueFraction'):
        assert second[color]>.02, ('Missing fixture primary-color content',second)
    assert first['sampledPixelSHA256']!=second['sampledPixelSHA256'], 'Captured test-pattern pixels did not advance'

def observe_black(prefix):
    for name in (prefix,prefix+'-persistent'):
        metrics=capture(name)
        assert metrics['blackFraction']>.999 and metrics['opaqueFraction']>.999, ('Stage is not black',metrics)
        time.sleep(.4)

try:
    wait_event('playing')
    observe_video('initial-play')
    command('stop')
    assert wait_event('stopped')['stageEnabled']
    observe_black('stop-black')
    command('play')
    wait_event('playing')
    observe_video('restarted-play')
    assert wait_event('ended')['stageEnabled']
    observe_black('natural-end-black')
    command('disable')
    assert not wait_event('stopped')['stageEnabled']
    command('quit'); process.wait(timeout=15)
    assert process.returncode==0, ''.join(errors)
    report['status']='passed'
    save()
    print('Native test-pattern pixels, STOP blackout, restarted video and natural-end blackout observed.')
except Exception as error:
    report.update(status='failed',reason=str(error))
    save(); raise
finally:
    if process.poll() is None:
        try: command('quit'); process.wait(timeout=15)
        except (OSError,subprocess.TimeoutExpired): process.kill(); process.wait(timeout=5)
