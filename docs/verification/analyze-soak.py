import argparse
import json
import re
import statistics
from pathlib import Path

import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

parser = argparse.ArgumentParser()
parser.add_argument('input', type=Path)
parser.add_argument('output', type=Path)
parser.add_argument('--run', required=True)
parser.add_argument('--commit', required=True)
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)

def describe(samples, key, scale=1):
    pairs = [(item['seconds'], item[key] / scale) for item in samples if key in item]
    if not pairs:
        return None
    values = [y for _, y in pairs]
    # Fixed before inspecting the long-run data; the full trace is still shown.
    settled = [(x / 3600, y) for x, y in pairs if x >= 900]
    slope = statistics.linear_regression(*zip(*settled)).slope if len(settled) >= 2 else None
    first_window = [y for x, y in pairs if 900 <= x <= 1800]
    final_window = [y for x, y in pairs if x >= pairs[-1][0] - 900]
    return {
        'first': values[0], 'final': values[-1], 'sampledMin': min(values),
        'sampledMax': max(values), 'changeFirstToFinal': values[-1] - values[0],
        'slopePerHourAfter15Minutes': slope,
        'medianMinutes15to30': statistics.median(first_window) if first_window else None,
        'medianFinal15Minutes': statistics.median(final_window),
        'final15MinutesRange': [min(final_window), max(final_window)],
    }

reports = []
traces = []
for source in sorted(args.input.rglob('*.http-smoke.json')):
    records = json.loads(source.read_text())
    target = re.search(r'(darwin|windows)-(amd64|arm64)', str(source))
    assert target, source
    target = target.group()
    repeats = [record['soak'] for record in records if 'soak' in record]
    report = {'target': target, 'sourceFile': str(source), 'repeatReportPresent': bool(repeats)}
    if repeats:
        assert len(repeats) == 1
        repeat = repeats[0]
        samples = repeat['samples']
        times = [sample['seconds'] for sample in samples]
        assert times and times == sorted(times), source
        duration = repeat['elapsedSeconds']
        requested = repeat['requestedSeconds']
        report.update({
            'requestedSeconds': requested, 'elapsedSeconds': duration,
            'cycles': repeat['cycles'], 'samples': len(samples),
            'requestedDurationCovered': duration >= requested and times[-1] >= requested and repeat.get('completed', True),
            'atLeastTwoHoursCovered': requested >= 7200 and duration >= 7200 and times[-1] >= 7200 and repeat.get('completed', True),
            'explicitCompletionFlag': repeat.get('completed'),
            'physicalRoutingOrAVDriftVerified': repeat['physicalRoutingOrAVDriftVerified'],
            'residentMiB': describe(samples, 'residentBytes', 1024 ** 2),
            'handles': describe(samples, 'handles'),
        })
        traces.append((target, samples))
    reports.append(report)

assert reports, 'No native application reports found'
summary = {'runId': args.run, 'sourceCommit': args.commit, 'matplotlib': matplotlib.__version__,
           'method': 'All samples retained. Ordinary least-squares trend excludes the first 15 minutes. Descriptive statistics only; these do not prove absence of leaks or physical A/V correctness.',
           'targets': reports}
(args.output / 'resource-summary.json').write_text(json.dumps(summary, indent=2))

fig, axes = plt.subplots(2, 1, figsize=(10.5, 7.5), sharex=True, constrained_layout=True)
colors = {'darwin-amd64': '#7251a1', 'darwin-arm64': '#087f8c', 'windows-amd64': '#cf6a12', 'windows-arm64': '#2462a2'}
for target, samples in traces:
    x = [sample['seconds'] / 60 for sample in samples]
    axes[0].plot(x, [sample['residentBytes'] / 1024 ** 2 for sample in samples], label=target, color=colors[target], linewidth=1.6)
    if 'handles' in samples[0]:
        axes[1].plot(x, [sample['handles'] for sample in samples], label=target, color=colors[target], linewidth=1.6)
for axis in axes:
    axis.grid(alpha=.22)
    axis.axvspan(0, 15, alpha=.06, color='black')
    axis.legend(loc='best', frameon=False)
    axis.spines[['top', 'right']].set_visible(False)
    axis.set_xlim(left=0)
axes[0].set_ylabel('Sampled resident memory (MiB)')
axes[1].set_ylabel('Windows process handles')
axes[1].set_xlabel('Elapsed native repeat test (minutes)')
fig.suptitle(f'Native cue transitions / STOP — run {args.run}\nsource {args.commit[:12]}; virtual runner outputs; physical A/V unverified', fontsize=12)
fig.savefig(args.output / 'resource-trends.png', dpi=170)
fig.savefig(args.output / 'resource-trends.svg')
plt.close(fig)
print(json.dumps(summary, indent=2))
