#!/usr/bin/env python3
"""End-to-end HTTP exercise against the real application/native backend.

Does not establish physical speaker routing, visible stage content or LAN phone
compatibility. Pairing keys and cookies are never written to test records.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import queue
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

exe = str(Path(sys.argv[1]).resolve())
root = Path(__file__).resolve().parent.parent
records = []
soak_seconds = int(sys.argv[2]) if len(sys.argv)>2 else 0
assert soak_seconds >= 0, 'Soak duration must be nonnegative'

def save_records():
    destination = Path(exe+'.http-smoke.json')
    temporary = destination.with_name(destination.name+'.tmp')
    temporary.write_text(json.dumps(records,indent=2))
    temporary.replace(destination)

def resources(pid):
    if os.name != 'nt':
        rss = int(subprocess.check_output(['ps','-o','rss=','-p',str(pid)],text=True).strip())*1024
        return {'residentBytes':rss}
    import ctypes
    from ctypes import wintypes
    class Counters(ctypes.Structure):
        _fields_ = [('cb',wintypes.DWORD),('PageFaultCount',wintypes.DWORD),
                    *[(name,ctypes.c_size_t) for name in ('PeakWorkingSetSize','WorkingSetSize','QuotaPeakPagedPoolUsage','QuotaPagedPoolUsage','QuotaPeakNonPagedPoolUsage','QuotaNonPagedPoolUsage','PagefileUsage','PeakPagefileUsage')]]
    kernel = ctypes.WinDLL('kernel32',use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD,wintypes.BOOL,wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel.K32GetProcessMemoryInfo.argtypes = [wintypes.HANDLE,ctypes.c_void_p,wintypes.DWORD]
    kernel.GetProcessHandleCount.argtypes = [wintypes.HANDLE,ctypes.POINTER(wintypes.DWORD)]
    handle = kernel.OpenProcess(0x400|0x10,False,pid)
    if not handle: raise ctypes.WinError(ctypes.get_last_error())
    try:
        counters = Counters(); counters.cb=ctypes.sizeof(counters); handles=wintypes.DWORD()
        if not kernel.K32GetProcessMemoryInfo(handle,ctypes.byref(counters),counters.cb): raise ctypes.WinError(ctypes.get_last_error())
        if not kernel.GetProcessHandleCount(handle,ctypes.byref(handles)): raise ctypes.WinError(ctypes.get_last_error())
        return {'residentBytes':counters.WorkingSetSize,'handles':handles.value}
    finally: kernel.CloseHandle(handle)

def wait_for(predicate, seconds=45):
    end = time.monotonic()+seconds
    while time.monotonic()<end:
        value = predicate()
        if value: return value
        time.sleep(.1)
    raise AssertionError('Timed out waiting for native application state')

with tempfile.TemporaryDirectory(prefix='smartstage-native-http-') as config:
    process = None
    try:
        for restart in range(2):
            with socket.socket() as sock:
                sock.bind(('127.0.0.1',0)); port = sock.getsockname()[1]
            origin = f'http://127.0.0.1:{port}'
            process = subprocess.Popen([exe,'--port',str(port),'--bind','127.0.0.1','--config-dir',config,'--media-root',str(root/'testdata'/'media')],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8',
                creationflags=subprocess.CREATE_NEW_PROCESS_GROUP if os.name=='nt' else 0)
            lines = queue.Queue()
            def read_stdout():
                for line in process.stdout: lines.put(line)
            threading.Thread(target=read_stdout,daemon=True).start()
            keys = {}
            end=time.monotonic()+20
            while len(keys)<2 and time.monotonic()<end:
                if process.poll() is not None: raise AssertionError('Application startup failed: '+process.stderr.read())
                try: line=lines.get(timeout=.2)
                except queue.Empty: continue
                for role in ('Admin','Command'):
                    if line.startswith(role+' pairing key:'): keys[role]=line.split(':',1)[1].strip()
            assert len(keys)==2, 'Missing startup pairing keys'
            assert keys['Admin']!=keys['Command']
            def client(key):
                opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                token=''
                def call(method,path,body=None,expected=200):
                    data=None if body is None else json.dumps(body).encode()
                    req=urllib.request.Request(origin+path,data=data,method=method,headers={'Origin':origin,'Content-Type':'application/json','X-CSRF-Token':token})
                    try: response=opener.open(req,timeout=10)
                    except urllib.error.HTTPError as error: response=error
                    value=json.loads(response.read())
                    statuses = expected if isinstance(expected, tuple) else (expected,)
                    assert response.status in statuses,(method,path,response.status,value)
                    return value
                token=call('POST','/api/pair',{'key':key})['csrfToken']
                return call
            admin=client(keys['Admin']);command=client(keys['Command'])
            initial=admin('GET','/api/state')['state']
            assert initial['state']=='stopped' and not initial['stageEnabled'] and not initial['activeCueId']
            command('GET','/api/files',expected=403)
            if restart:
                assert [c['label'] for c in initial['cues']]==['Finale','Opening music','Welcome video','Interlude']
                records.append({'restart':'saved labels/order restored; silent and stage disabled','instanceChanged':initial['instanceId']!=previous_instance})
                assert initial['instanceId']!=previous_instance
            else:
                listing=admin('GET','/api/files')
                paths={entry['name']:entry['path'] for entry in listing['entries']}
                names=[("Opening – café's tone.wav",'Opening music'),('video-aac-1080p.mp4','Welcome video'),('tone.mp3','Interlude'),('silent-1080p.mp4','Finale')]
                cues=[{'id':'','label':label,'path':paths[name]} for name,label in names]
                saved=admin('PUT','/api/playlist',{'expectedRevision':initial['playlistRevision'],'cues':cues})
                cues=[{k:c[k] for k in ('id','label','path')} for c in saved['cues']]
                cues=[cues[-1],*cues[:-1]]
                saved=admin('PUT','/api/playlist',{'expectedRevision':saved['playlistRevision'],'cues':cues})
                ready=wait_for(lambda: (lambda s:s if all(c['validation']=='ready' for c in s['cues']) else None)(admin('GET','/api/state')['state']),90)
                assert len(ready['cues'])==4
                assert 'path' not in json.dumps(command('GET','/api/state'))
                outputs=admin('GET','/api/devices')
                audio=next((d for d in outputs['audio'] if not d['default']),next(iter(outputs['audio']),None))
                display=next((d for d in outputs['displays'] if not d['primary']),next(iter(outputs['displays']),None))
                admin('PUT','/api/outputs',{'audioId':audio['id'] if audio else 'default','displayId':display['id'] if display else '', 'allowPrimary':True})
                wait_for(lambda:admin('GET','/api/state')['state']['state']=='stopped')
                played=0
                for cue in saved['cues']:
                    is_video=cue['path'].endswith('.mp4'); silent=cue['path'].endswith('silent-1080p.mp4')
                    if is_video and not display or not silent and not audio: continue
                    current=command('GET','/api/state')['state']
                    command('POST','/api/play',{'requestId':f'http-smoke-play-{played}','instanceId':current['instanceId'],'stopEpoch':current['stopEpoch'],'cueId':cue['id']},expected=202)
                    def playing():
                        s=admin('GET','/api/state')['state']
                        assert s['state']!='error',s['lastError']
                        return s if s['state']=='playing' else None
                    wait_for(playing)
                    command('POST','/api/stop',{'requestId':f'http-smoke-stop-{played}'},expected=202)
                    stopped=wait_for(lambda:(lambda s:s if s['state']=='stopped' else None)(command('GET','/api/state')['state']))
                    if is_video: assert stopped['stageEnabled']
                    assert not stopped['activeCueId']
                    played+=1
                previous_instance=ready['instanceId']
                records.append({'cuesConfigured':4,'cuesNativelyPlayedAndStopped':played,'audioEndpoints':len(outputs['audio']),'displays':len(outputs['displays']),'physicalRoutingVerified':False})
                if soak_seconds:
                    playable=[c for c in saved['cues'] if (not c['path'].endswith('.mp4') or display) and (c['path'].endswith('silent-1080p.mp4') or audio)]
                    assert playable, 'No real runner output for soak'
                    start=time.monotonic();next_sample=start;cycles=0;samples=[]
                    # Retain incomplete results if a later cycle fails. Checkpoint
                    # before/throughout the loop so a killed job still has evidence.
                    soak={'requestedSeconds':soak_seconds,'elapsedSeconds':0,'cycles':0,
                          'completed':False,'samples':samples,'physicalRoutingOrAVDriftVerified':False}
                    records.append({'soak':soak})
                    save_records()
                    def sample_resources():
                        sample={'seconds':round(time.monotonic()-start,2),'cycles':cycles,**resources(process.pid)}
                        samples.append(sample)
                        save_records()
                        print('Native soak resource sample: '+json.dumps(sample),flush=True)
                    while time.monotonic()-start<soak_seconds:
                        cue=playable[cycles%len(playable)]
                        current=command('GET','/api/state')['state']
                        command('POST','/api/play',{'requestId':f'soak-play-{cycles}','instanceId':current['instanceId'],'stopEpoch':current['stopEpoch'],'cueId':cue['id']},expected=202)
                        if cycles%10:
                            wait_for(playing)
                        else:
                            replacement=playable[(cycles+1)%len(playable)]
                            command('POST','/api/play',{'requestId':f'soak-replace-{cycles}','instanceId':current['instanceId'],'stopEpoch':current['stopEpoch'],'cueId':replacement['id']},expected=202)
                        command('POST','/api/stop',{'requestId':f'soak-stop-{cycles}'},expected=202)
                        wait_for(lambda:command('GET','/api/state')['state']['state']=='stopped')
                        time.sleep(.1)
                        after=command('GET','/api/state')['state']
                        assert after['state']=='stopped' and not after['activeCueId'], 'Late native callback revived media'
                        cycles+=1
                        soak.update(elapsedSeconds=time.monotonic()-start,cycles=cycles)
                        if time.monotonic()>=next_sample:
                            sample_resources()
                            next_sample=time.monotonic()+60
                    sample_resources()
                    soak.update(elapsedSeconds=time.monotonic()-start,completed=True)
                    save_records()
            # Exercise shutdown during newly requested or still-running startup
            # validation. Native objects must drain before framework teardown.
            validation = admin('POST','/api/validate',{},expected=(202,503))
            if 'error' in validation: assert validation['error']['code']=='busy', validation
            process.send_signal(signal.CTRL_BREAK_EVENT if os.name=='nt' else signal.SIGINT)
            process.wait(timeout=15)
            assert process.returncode==0, f'Unclean application shutdown: {process.returncode}'
            process=None
        print('Real application HTTP/native smoke test passed; physical routing remains unverified.')
    finally:
        if process is not None and process.poll() is None: process.kill();process.wait()
        save_records()
