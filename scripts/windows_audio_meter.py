"""Development-only, read-only Windows endpoint signal observation.

IAudioMeterInformation observes the render stream before endpoint volume. This
does not establish physical sound, A/V synchronization or wired STOP latency.
See https://learn.microsoft.com/en-us/windows/win32/coreaudio/peak-meters and
Microsoft's endpointvolume.idl for the interface UUID and method order.
"""
import ctypes
from ctypes import wintypes
import json
import os
from pathlib import Path
import time
import uuid


class GUID(ctypes.Structure):
    _fields_ = [('data1', ctypes.c_uint32), ('data2', ctypes.c_uint16),
                ('data3', ctypes.c_uint16), ('data4', ctypes.c_ubyte * 8)]

    @classmethod
    def parse(cls, value):
        return cls.from_buffer_copy(uuid.UUID(value).bytes_le)


def checked(result, operation):
    if result < 0:
        raise OSError(f'{operation}: HRESULT 0x{result & 0xffffffff:08x}')


def method(pointer, index, result, *arguments):
    table = ctypes.cast(pointer, ctypes.POINTER(ctypes.POINTER(ctypes.c_void_p))).contents
    return ctypes.WINFUNCTYPE(result, ctypes.c_void_p, *arguments)(table[index])


class PeakMeters:
    def __init__(self, identifiers):
        assert os.name == 'nt', 'Windows endpoint meters require Windows'
        self.pointers, self.meters, self.devices, self.capabilities = [], {}, {}, {}
        self.ole = ctypes.WinDLL('ole32')
        self.ole.CoInitializeEx.argtypes = [ctypes.c_void_p, wintypes.DWORD]
        self.ole.CoInitializeEx.restype = ctypes.c_long
        self.ole.CoUninitialize.argtypes = []
        self.ole.CoUninitialize.restype = None
        self.ole.CoCreateInstance.argtypes = [ctypes.POINTER(GUID), ctypes.c_void_p,
            wintypes.DWORD, ctypes.POINTER(GUID), ctypes.POINTER(ctypes.c_void_p)]
        self.ole.CoCreateInstance.restype = ctypes.c_long
        checked(self.ole.CoInitializeEx(None, 0), 'Initialize observer COM')
        try:
            clsid = GUID.parse('bcde0395-e52f-467c-8e3d-c4579291692e')
            iid = GUID.parse('a95664d2-9614-4f35-a746-de8db63617e6')
            enumerator = ctypes.c_void_p()
            checked(self.ole.CoCreateInstance(ctypes.byref(clsid), None, 1,
                ctypes.byref(iid), ctypes.byref(enumerator)), 'Create endpoint enumerator')
            self.pointers.append(enumerator)
            meter_iid = GUID.parse('c02216f6-8c67-4b5b-9d00-d008e73e0064')
            for identifier in identifiers:
                device, meter = ctypes.c_void_p(), ctypes.c_void_p()
                checked(method(enumerator, 5, ctypes.c_long, wintypes.LPCWSTR,
                    ctypes.POINTER(ctypes.c_void_p))(enumerator, identifier,
                    ctypes.byref(device)), 'Get selected endpoint')
                self.pointers.append(device)
                self.devices[identifier] = device
                checked(method(device, 3, ctypes.c_long, ctypes.POINTER(GUID),
                    wintypes.DWORD, ctypes.c_void_p, ctypes.POINTER(ctypes.c_void_p))(
                    device, ctypes.byref(meter_iid), 23, None, ctypes.byref(meter)),
                    'Activate endpoint meter')
                self.pointers.append(meter)
                self.meters[identifier] = meter
                channels, hardware = wintypes.UINT(), wintypes.DWORD()
                checked(method(meter, 4, ctypes.c_long, ctypes.POINTER(wintypes.UINT))(
                    meter, ctypes.byref(channels)), 'Read metering channel count')
                checked(method(meter, 6, ctypes.c_long, ctypes.POINTER(wintypes.DWORD))(
                    meter, ctypes.byref(hardware)), 'Read meter hardware support')
                self.capabilities[identifier] = {'channels': channels.value,
                    'hardwareSupportMask': hardware.value}
        except Exception:
            self.close()
            raise

    def peaks(self):
        result = {}
        for identifier, meter in self.meters.items():
            peak = ctypes.c_float()
            checked(method(meter, 3, ctypes.c_long, ctypes.POINTER(ctypes.c_float))(
                meter, ctypes.byref(peak)), 'Read endpoint peak')
            assert 0 <= peak.value <= 1, 'Invalid native meter value'
            result[identifier] = peak.value
        return result

    def sessions(self, pid):
        """Diagnostic snapshots, not a complete session-notification history."""
        result = {}
        manager_iid = GUID.parse('77aa99a0-1bd6-484f-8bc7-2c654c9a9b6f')
        control_iid = GUID.parse('bfb7ff88-7239-4fc9-8fa2-07c950be9c6d')
        meter_iid = GUID.parse('c02216f6-8c67-4b5b-9d00-d008e73e0064')
        for identifier, device in self.devices.items():
            pointers, result[identifier] = [], []
            try:
                manager, enumerator = ctypes.c_void_p(), ctypes.c_void_p()
                checked(method(device, 3, ctypes.c_long, ctypes.POINTER(GUID),
                    wintypes.DWORD, ctypes.c_void_p, ctypes.POINTER(ctypes.c_void_p))(
                    device, ctypes.byref(manager_iid), 23, None, ctypes.byref(manager)),
                    'Activate diagnostic session manager')
                pointers.append(manager)
                checked(method(manager, 5, ctypes.c_long, ctypes.POINTER(ctypes.c_void_p))(
                    manager, ctypes.byref(enumerator)), 'Enumerate diagnostic sessions')
                pointers.append(enumerator)
                count = ctypes.c_int()
                checked(method(enumerator, 3, ctypes.c_long, ctypes.POINTER(ctypes.c_int))(
                    enumerator, ctypes.byref(count)), 'Count diagnostic sessions')
                assert 0 <= count.value <= 256, 'Unexpected diagnostic session count'
                for index in range(count.value):
                    control, control2, meter = ctypes.c_void_p(), ctypes.c_void_p(), ctypes.c_void_p()
                    checked(method(enumerator, 4, ctypes.c_long, ctypes.c_int,
                        ctypes.POINTER(ctypes.c_void_p))(enumerator, index,
                        ctypes.byref(control)), 'Read diagnostic session')
                    pointers.append(control)
                    checked(method(control, 0, ctypes.c_long, ctypes.POINTER(GUID),
                        ctypes.POINTER(ctypes.c_void_p))(control, ctypes.byref(control_iid),
                        ctypes.byref(control2)), 'Query diagnostic session process')
                    pointers.append(control2)
                    owner = wintypes.DWORD()
                    checked(method(control2, 14, ctypes.c_long, ctypes.POINTER(wintypes.DWORD))(
                        control2, ctypes.byref(owner)), 'Read diagnostic session process')
                    if owner.value != pid:
                        continue
                    state, peak = ctypes.c_int(), ctypes.c_float()
                    checked(method(control, 3, ctypes.c_long, ctypes.POINTER(ctypes.c_int))(
                        control, ctypes.byref(state)), 'Read diagnostic session state')
                    checked(method(control, 0, ctypes.c_long, ctypes.POINTER(GUID),
                        ctypes.POINTER(ctypes.c_void_p))(control, ctypes.byref(meter_iid),
                        ctypes.byref(meter)), 'Query diagnostic session meter')
                    pointers.append(meter)
                    checked(method(meter, 3, ctypes.c_long, ctypes.POINTER(ctypes.c_float))(
                        meter, ctypes.byref(peak)), 'Read diagnostic session peak')
                    result[identifier].append({'processId': owner.value,
                        'state': state.value, 'peak': peak.value})
            finally:
                for pointer in reversed(pointers):
                    method(pointer, 2, wintypes.ULONG)(pointer)
        return result

    def close(self):
        for pointer in reversed(self.pointers):
            method(pointer, 2, wintypes.ULONG)(pointer)
        self.pointers.clear()
        self.meters.clear()
        self.devices.clear()
        self.ole.CoUninitialize()


def exercise(exe, pid, admin, command, cues, audio, outputs, wait_for):
    """Check actual endpoint signal for WAV, MP3 and an AAC video soundtrack."""
    assert audio and len(outputs['audio']) >= 2 and not audio['default'], \
        'This routing evaluation requires a real non-default endpoint and a control endpoint'
    report = {'status': 'incomplete', 'endpoints': outputs['audio'],
        'selectedEndpoint': audio['id'], 'selectedNonDefault': not audio['default'],
        'physicalRoutingVerified': False, 'serverReceivedStopLatencyVerified': False,
        'signalThreshold': .01, 'silenceThreshold': .0001,
        'samplingPeriodSeconds': .01, 'stopObservationGraceSeconds': .25,
        'processId': pid, 'isolationFailures': [], 'phases': []}
    destination = Path(exe + '.audio-meter.json')
    meters = None

    def save():
        destination.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')

    def observe(phase, seconds, cue=None, repetition=None):
        row = {'phase': phase, 'cue': cue, 'repetition': repetition, 'samples': []}
        report['phases'].append(row)
        start = time.monotonic()
        while True:
            elapsed = time.monotonic() - start
            row['samples'].append({'seconds': round(elapsed, 6), 'peaks': meters.peaks()})
            if elapsed >= seconds:
                break
            time.sleep(.01)
        row['maximumPeaks'] = {identifier: max(s['peaks'][identifier] for s in row['samples'])
                              for identifier in meters.meters}
        row['applicationSessions'] = meters.sessions(pid)
        save()
        return row

    try:
        meters = PeakMeters([device['id'] for device in outputs['audio']])
        report['meterCapabilities'] = meters.capabilities
        baseline = observe('stopped-baseline', .4)
        assert max(baseline['maximumPeaks'].values()) <= report['silenceThreshold'], \
            'Another stream is active on an observed endpoint before the test'
        for index, cue in enumerate(c for c in cues if not c['path'].endswith('silent-1080p.mp4')):
            for repetition in range(2):
                current = command('GET', '/api/state')['state']
                command('POST', '/api/play', {'requestId': f'meter-play-{index}-{repetition}',
                    'instanceId': current['instanceId'], 'stopEpoch': current['stopEpoch'],
                    'cueId': cue['id']}, expected=202)

                def playing():
                    state = admin('GET', '/api/state')['state']
                    assert state['state'] != 'error', state['lastError']
                    return state['state'] == 'playing'

                wait_for(playing)
                observed = observe('playing', 1, cue['label'], repetition)
                assert observed['maximumPeaks'][audio['id']] > report['signalThreshold'], \
                    'No signal on the selected native endpoint'
                if any(peak > report['silenceThreshold'] for identifier, peak in
                    observed['maximumPeaks'].items() if identifier != audio['id']):
                    # Preserve the failure while observing subsequent STOP and
                    # replay phases, to diagnose shared-driver meter behavior.
                    report['isolationFailures'].append({'cue': cue['label'], 'repetition': repetition})
                command('POST', '/api/stop', {'requestId': f'meter-stop-{index}-{repetition}'}, expected=202)
                wait_for(lambda: command('GET', '/api/state')['state']['state'] == 'stopped')
                stopped = observe('after-stop', .7, cue['label'], repetition)
                settled = [sample for sample in stopped['samples']
                           if sample['seconds'] >= report['stopObservationGraceSeconds']]
                assert settled and all(peak <= report['silenceThreshold']
                    for sample in settled for peak in sample['peaks'].values()), \
                    'Signal persisted or returned after STOP'
                state = command('GET', '/api/state')['state']
                assert state['state'] == 'stopped' and not state['activeCueId']
        defaults_before = [device['id'] for device in outputs['audio'] if device['default']]
        report['endpointsAfter'] = admin('GET', '/api/devices')['audio']
        defaults_after = [device['id'] for device in report['endpointsAfter'] if device['default']]
        assert defaults_after == defaults_before, 'Default endpoint changed during routing test'
        report.update(audioCuePlayStopCycles=6, defaultEndpointUnchanged=True, signalStopReplayVerified=True)
        assert not report['isolationFailures'], 'Signal appeared on an unselected endpoint'
        report['status'] = 'passed'
    except Exception as error:
        report.update(status='failed', error=str(error))
        raise
    finally:
        save()
        if meters is not None:
            meters.close()
    return report
