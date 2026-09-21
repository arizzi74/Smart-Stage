#!/usr/bin/env python3
"""Run the real native harness on a matching OS, recording actual evidence."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time

exe = str(Path(sys.argv[1]).resolve())
root = Path(__file__).resolve().parent.parent
media = root/'testdata'/'media'
records = []

def run(*args, success=True):
    result = subprocess.run([exe, *map(str,args)], capture_output=True, text=True, encoding='utf-8', timeout=45)
    records.append({'args':list(map(str,args)), 'returncode':result.returncode, 'stdout':result.stdout, 'stderr':result.stderr})
    if (result.returncode == 0) != success:
        raise AssertionError(f'{args}: unexpected exit {result.returncode}: {result.stderr}')
    return [json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]

def run_until_ended(*args):
    """Wait for native completion; startup time must not shorten the media run."""
    started = time.monotonic()
    timings = {}
    with tempfile.TemporaryDirectory(prefix='smartstage-native-smoke-') as directory:
        output, errors = Path(directory)/'stdout', Path(directory)/'stderr'
        with output.open('w',encoding='utf-8') as stdout, errors.open('w',encoding='utf-8') as stderr:
            process = subprocess.Popen([exe,*map(str,args)],stdin=subprocess.PIPE,
                                       stdout=stdout,stderr=stderr,text=True,encoding='utf-8')
            try:
                while process.poll() is None and time.monotonic()-started < 30:
                    events = []
                    for line in output.read_text(encoding='utf-8').splitlines():
                        try: events.append(json.loads(line))
                        except json.JSONDecodeError: pass # An in-flight write is read on the next poll.
                    for event in events:
                        timings.setdefault(event['kind'],round(time.monotonic()-started,3))
                    if any(event['kind'] in ('ended','error','device-lost') for event in events):
                        process.stdin.write('quit\n'); process.stdin.flush()
                        break
                    time.sleep(.05)
                else:
                    if process.poll() is None:
                        raise AssertionError('No native end/error event within 30 seconds')
                process.wait(timeout=15)
            finally:
                if process.poll() is None:
                    process.kill(); process.wait(timeout=5)
                records.append({'args':list(map(str,args)),'returncode':process.returncode,
                    'stdout':output.read_text(encoding='utf-8'),'stderr':errors.read_text(encoding='utf-8'),
                    'observedEventSeconds':timings,'elapsedSeconds':round(time.monotonic()-started,3)})
                process.stdin.close()
    assert process.returncode == 0, records[-1]
    return [json.loads(line) for line in records[-1]['stdout'].splitlines() if line.startswith('{')]

try:
    devices = run('--list')[0]
    assert isinstance(devices['audio'], list) and isinstance(devices['displays'], list)
    for name,kind,audio in [("Opening – café's tone.wav",'audio',True), ('tone.mp3','audio',True),
                            ('silent-1080p.mp4','video',False), ('video-aac-1080p.mp4','video',True)]:
        result = run('--inspect',media/name)[0]
        assert result['kind'] == kind and result['hasAudio'] is audio and 2.5 < result['duration'] < 3.5, result
    run('--inspect',media/'damaged.mp4',success=False)
    if sys.platform == 'win32':
        source = subprocess.run([sys.executable, str(root/'scripts/windows-source-checks.py')],
                                capture_output=True, text=True, encoding='utf-8', timeout=150)
        records.append({'nativeSourceOpening': {
            'returncode': source.returncode, 'stdout': source.stdout, 'stderr': source.stderr}})
        assert source.returncode == 0, f'Native source opening regression failed: {source.stdout}\n{source.stderr}'
        records[-1]['nativeSourceOpening'] = json.loads(source.stdout)
    # A real native source with an unavailable output must fail rather than
    # silently routing to a default speaker or an unrelated display.
    for name,option in [("Opening – café's tone.wav",'--audio'), ('silent-1080p.mp4','--display')]:
        events = run('--file',media/name,option,'smartstage-output-that-does-not-exist','--exit-after','5s')
        assert any(e['kind'] in ('error','device-lost') for e in events), events
        assert not any(e['kind']=='playing' for e in events), events
    if devices['audio']:
        endpoint = next((a for a in devices['audio'] if not a['default']),devices['audio'][0])
        events = run('--file',media/"Opening – café's tone.wav",'--audio',endpoint['id'],'--stop-playing-after','500ms','--exit-after','10s')
        assert any(e['kind']=='playing' for e in events), events
        assert any(e['kind']=='stopped' for e in events), events
        stopped = next(i for i,e in enumerate(events) if e['kind']=='stopped')
        assert not any(e['kind']=='playing' for e in events[stopped+1:]), events
    if devices['displays']:
        screen = devices['displays'][0]
        events = run_until_ended('--file',media/'silent-1080p.mp4','--display',screen['id'],'--exit-after','40s')
        assert any(e['kind']=='playing' for e in events), events
        assert any(e['kind']=='ended' and e['stageEnabled'] for e in events), events
        from native_keyboard_checks import escape_checks
        records.append({'nativeKeyboardEvents':escape_checks(screen),
                        'method':'Test-only event driver linked against the production bridge; no physical keyboard assertion'})
        if sys.platform == 'win32':
            from windows_cursor_checks import cursor_checks
            try:
                records.append({'nativeStageCursor': cursor_checks(screen)})
            except Exception as error:
                records.append({'nativeStageCursor': {'status': 'failed', 'diagnostic': str(error)}})
                raise
    if devices['displays'] and (sys.platform == 'win32' or devices['audio']):
        if sys.platform == 'darwin':
            from native_scene_checks import scene_checks
            scene = scene_checks(devices)
        else:
            from windows_scene_checks import scene_checks
            endpoint = next((item for item in devices['audio'] if item['default']), next(iter(devices['audio']), None))
            scene = scene_checks(endpoint, devices['displays'][0])
        records.append({'nativeScene': scene, 'method': 'Test-only observer of production native players, gain, timeline and presentation; no physical speaker assertion'})
    else:
        records.append({'nativeScene': {'status': 'unavailable', 'reason': 'Native scene checks need an audio endpoint and display', 'audioEndpoints': len(devices['audio']), 'displays': len(devices['displays'])}})
    print('Native smoke checks passed. This checks native events, not physical routing or visible blackout.')
finally:
    Path(exe+'.smoke.json').write_text(json.dumps(records,indent=2,ensure_ascii=False),encoding='utf-8')
