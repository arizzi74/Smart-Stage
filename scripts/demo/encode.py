"""Encode actual browser screenshots as a compact animated tutorial and poster."""
import argparse
import json
from pathlib import Path
from PIL import Image

parser = argparse.ArgumentParser()
parser.add_argument('frames', type=Path)
parser.add_argument('output', type=Path)
args = parser.parse_args()
manifest = json.loads((args.frames / 'frames.json').read_text())
frames = [Image.open(args.frames / f['file']).convert('RGB') for f in manifest['frames']]
assert frames and all(frame.width == frames[0].width for frame in frames)
width = min(720, frames[0].width)
size = (width, round(max(frame.height for frame in frames) * width / frames[0].width))
padded = []
for frame in frames:
    scaled = frame.resize((width, round(frame.height * width / frame.width)), Image.Resampling.LANCZOS)
    canvas = Image.new('RGB', size, '#111519')
    canvas.paste(scaled, (0, 0))
    padded.append(canvas)
frames = padded
# One shared palette prevents flashing between UI screenshots. Real GIF frame
# durations retain pauses around each interaction; unchanged frames coalesce.
strip = Image.new('RGB', (size[0], size[1] * len(frames)))
for index, frame in enumerate(frames):
    strip.paste(frame, (0, index * size[1]))
palette = strip.quantize(colors=128, method=Image.Quantize.MEDIANCUT)
indexed = [frame.quantize(palette=palette, dither=Image.Dither.NONE) for frame in frames]
args.output.parent.mkdir(parents=True, exist_ok=True)
indexed[0].save(args.output.with_suffix('.gif'), save_all=True, append_images=indexed[1:],
                duration=[f['duration'] for f in manifest['frames']], loop=0, optimize=True, disposal=1)
frames[-1].save(args.output.with_suffix('.png'), optimize=True)
print(json.dumps({'width':size[0], 'height':size[1], 'durationMs':sum(f['duration'] for f in manifest['frames']),
                  'frames':len(frames), 'bytes':args.output.with_suffix('.gif').stat().st_size}))
