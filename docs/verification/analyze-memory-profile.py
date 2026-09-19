"""Summarize captured diagnostics; does not infer leaks or enforce thresholds."""
import argparse
import json
import re
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('input', type=Path)
parser.add_argument('--run', required=True)
parser.add_argument('--application-commit', required=True)
parser.add_argument('--workflow-commit', required=True)
args = parser.parse_args()

def bytes_from_vmmap(value):
    match = re.fullmatch(r'([\d.]+)([KMGT]?)', value)
    assert match, value
    return float(match[1]) * 1024 ** ('KMGT'.index(match[2]) + 1 if match[2] else 0)

def vmmap_summary(path):
    content = path.read_text()
    assert content.startswith('Exit code: 0\n'), path
    zone = re.split(r'^MALLOC ZONE', content, flags=re.MULTILINE)[-1]
    total = re.search(r'^TOTAL\s+(.+)$', zone, re.MULTILINE)
    assert total, path
    columns = total[1].split()
    footprint = re.search(r'^Physical footprint:\s+(\S+)', content, re.MULTILINE)
    assert footprint, path
    return {'mallocAllocationCount':int(columns[4]),
            'mallocAllocatedBytesRounded':bytes_from_vmmap(columns[5]),
            'physicalFootprintBytesRounded':bytes_from_vmmap(footprint[1])}

reports = []
for path in sorted(args.input.rglob('*.http-smoke.json')):
    target = re.search(r'(darwin|windows)-(amd64|arm64)', str(path))
    assert target, path
    records = json.loads(path.read_text())
    soak = next(record['soak'] for record in records if 'soak' in record)
    prefix = str(path)[:-len('.http-smoke.json')]
    gc = Path(prefix+'.gctrace-0.txt').read_text()
    live_mb = [int(match[1]) for match in re.finditer(r'\d+->\d+->(\d+) MB', gc)]
    report = {'target':target[0], 'cycles':soak['cycles'],
              'elapsedSeconds':soak['elapsedSeconds'], 'completed':soak.get('completed', False),
              'firstSample':soak['samples'][0], 'lastSample':soak['samples'][-1],
              'goGCCollections':len(live_mb),
              'goLiveHeapMiBRounded':{'first':live_mb[0], 'final':live_mb[-1], 'max':max(live_mb)} if live_mb else None}
    for phase in ('before', 'after', 'idle'):
        vmmap = Path(prefix+f'.memory-{phase}-vmmap.txt')
        process = Path(prefix+f'.memory-{phase}-process.txt')
        if vmmap.exists():
            report[phase] = vmmap_summary(vmmap)
        elif process.exists():
            data = process.read_text(encoding='utf-8-sig')
            assert data.startswith('Exit code: 0\n'), process
            report[phase] = json.loads(data.split('\n',1)[1])
    if 'darwin' in target[0]:
        before, after = report['before'], report['after']
        count = after['mallocAllocationCount']-before['mallocAllocationCount']
        allocated = after['mallocAllocatedBytesRounded']-before['mallocAllocatedBytesRounded']
        report['mallocChange'] = {'allocationCount':count, 'bytesRounded':allocated,
                                 'allocationsPerCycle':count/soak['cycles'],
                                 'bytesPerCycleRounded':allocated/soak['cycles']}
    reports.append(report)
assert reports, 'No profiles found'
summary = {'runId':args.run, 'applicationCommit':args.application_commit,
           'workflowCommit':args.workflow_commit,
           'method':'GC heap values and vmmap sizes are rounded by their tools. Before/after snapshots and continuous resource samples are separate observations. Counts/trends describe this workload; they do not establish resource stability or physical A/V behavior.',
           'targets':reports}
(args.input/'memory-summary.json').write_text(json.dumps(summary, indent=2)+'\n')
print(json.dumps(summary, indent=2))
