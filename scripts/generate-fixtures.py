#!/usr/bin/env python3
"""Development-only fixture authoring; never imported by the application."""
import hashlib
import json
import math
from pathlib import Path
import struct
import subprocess
import wave

root = Path(__file__).resolve().parent.parent / 'testdata' / 'media'
root.mkdir(parents=True, exist_ok=True)
tone = root / "Opening – café's tone.wav"
with wave.open(str(tone), 'wb') as w:
    w.setnchannels(2)
    w.setsampwidth(2)
    w.setframerate(48000)
    for i in range(48000 * 3):
        # Quiet (-24 dBFS peak) different tones make stereo routing observable.
        w.writeframesraw(struct.pack('<hh', *[int(2000*math.sin(2*math.pi*f*i/48000)) for f in (440, 660)]))

def encode(args):
    subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y', *args], check=True)

encode(['-i', str(tone), '-c:a', 'libmp3lame', '-b:a', '128k', str(root/'tone.mp3')])
video = ['-f', 'lavfi', '-i', 'testsrc2=size=1920x1080:rate=30', '-t', '3',
         '-c:v', 'libx264', '-preset', 'fast', '-profile:v', 'high', '-level:v', '4.0',
         '-pix_fmt', 'yuv420p', '-crf', '30', '-movflags', '+faststart', '-an']
encode([*video, str(root/'silent-1080p.mp4')])
encode(['-i', str(root/'silent-1080p.mp4'), '-i', str(tone), '-c:v', 'copy', '-c:a', 'aac',
        '-b:a', '128k', '-shortest', '-movflags', '+faststart', str(root/'video-aac-1080p.mp4')])
(root/'damaged.mp4').write_bytes(b'Smart Stage self-authored invalid media fixture\x00\xff')
records = {}
for p in sorted(root.iterdir()):
    if p.suffix in {'.mp3', '.mp4', '.wav'}:
        records[p.name] = {'sha256': hashlib.sha256(p.read_bytes()).hexdigest(), 'bytes': p.stat().st_size}
(root/'checksums.json').write_text(json.dumps(records, indent=2, ensure_ascii=False)+'\n')
