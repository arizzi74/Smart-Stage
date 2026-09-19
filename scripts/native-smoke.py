#!/usr/bin/env python3
"""Run the real native harness on a matching OS, recording actual evidence."""
import json
from pathlib import Path
import subprocess
import sys

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

try:
    devices = run('--list')[0]
    assert isinstance(devices['audio'], list) and isinstance(devices['displays'], list)
    for name,kind,audio in [("Opening – café's tone.wav",'audio',True), ('tone.mp3','audio',True),
                            ('silent-1080p.mp4','video',False), ('video-aac-1080p.mp4','video',True)]:
        result = run('--inspect',media/name)[0]
        assert result['kind'] == kind and result['hasAudio'] is audio and 2.5 < result['duration'] < 3.5, result
    run('--inspect',media/'damaged.mp4',success=False)
    if devices['audio']:
        endpoint = next((a for a in devices['audio'] if not a['default']),devices['audio'][0])
        events = run('--file',media/"Opening – café's tone.wav",'--audio',endpoint['id'],'--stop-playing-after','500ms','--exit-after','10s')
        assert any(e['kind']=='playing' for e in events), events
        assert any(e['kind']=='stopped' for e in events), events
        stopped = next(i for i,e in enumerate(events) if e['kind']=='stopped')
        assert not any(e['kind']=='playing' for e in events[stopped+1:]), events
    if devices['displays']:
        screen = devices['displays'][0]
        events = run('--file',media/'silent-1080p.mp4','--display',screen['id'],'--exit-after','6s')
        assert any(e['kind']=='playing' for e in events), events
        assert any(e['kind']=='ended' and e['stageEnabled'] for e in events), events
    print('Native smoke checks passed. This checks native events, not physical routing or visible blackout.')
finally:
    Path(exe+'.smoke.json').write_text(json.dumps(records,indent=2,ensure_ascii=False),encoding='utf-8')
