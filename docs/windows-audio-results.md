# Windows native audio evaluation

The published preview 4 executable has **native audio event and signal evidence
on both Windows architectures**, using a signed virtual audio driver on
disposable hosted runners. Physical speaker routing remains unverified. The
endpoint-meter isolation check failed; the signal/STOP/replay observations and
the meter's limitations are recorded separately below.

## Actual renderer execution

[Run 35482038438](https://github.com/arizzi74/Smart-Stage/actions/runs/35482038438)
at test commit `e2cb3cd` installed the exact published
`v0.1.0-preview.4` executable, application source
`505a1e495075dcc01abe6d7367d3b57d3f8e60a9`. Both jobs passed the real native
harness and application HTTP checks, including all four fixture cues and
saved-show restart while stopped with the stage disabled.

| Target | Vendor installer exit / elapsed | Native cue coverage | Repeated transitions / elapsed |
| --- | --- | --- | --- |
| Windows AMD64, `windows-2025` | 0 / 9.09 s | WAV, MP3, H.264/AAC and silent H.264, 4/4 played/stopped | 348 / 120.016 s |
| Windows ARM64, `windows-11-arm` | 0 / 9.54 s | Same 4/4 | 337 / 120.084 s |

Both machines initially had zero audio endpoints. Installation exposed
`CABLE Input` (default) and `CABLE In 16ch` (non-default). Tests explicitly
selected the latter through Smart Stage. This executes the actual Media
Foundation audio renderer and its per-stream silence control; event assertions
alone do not establish audible sound or exclusive routing.

Raw installer, signature, harness and application records are retained in
[`verification/windows-audio-e2cb3cd/`](verification/windows-audio-e2cb3cd/).
The short transition loop is not a two-hour audio soak.

## Evaluation environment

The workflow downloads the unchanged official
[VB-CABLE Pack45](https://vb-audio.com/Cable/), SHA-256
`b950e39f01af1d04ea623c8f6d8eb9b6ea5c477c637295fabf20631c85116bfb`.
It validates the installer Authenticode signature and Microsoft's hardware
compatibility catalog signature before executing the vendor installer.
The package supports AMD64 and ARM64. No driver bytes are redistributed in
Smart Stage, and its normal curl/irm installers do not install this driver.

The vendor requires a reboot after installation. These discarded test VMs
were **not rebooted**, so the results are explicitly pre-reboot observations,
not finalized driver setup, clean-machine acceptance or physical audio proof.
No OS security policy, trusted certificate store or driver-signature setting
was changed. The vendor's [licensing terms](https://vb-audio.com/Services/licensing.htm)
apply to evaluation and subsequent use.

The first [UI evaluation 35481647567](https://github.com/arizzi74/Smart-Stage/actions/runs/35481647567)
found no native Install button in the vendor's custom-drawn window. It performed
no installation, reported unavailable endpoints and explicitly skipped audio
tests. Read-only inspection of the exact verified installer then identified
`-i` (install) and `-h` (hide the vendor UI). Its command parser is at RVA `0x2d20`;
the install-command handler at `0x6437` invokes the same installation routine
as the GUI button. The evaluation uses only those flags, with bounded waits,
and requires both exit code 0 and actual endpoint enumeration before testing.

## Signal observations and isolation limitation

[Run 35482234579](https://github.com/arizzi74/Smart-Stage/actions/runs/35482234579)
at test commit `d98cef0` added read-only `IAudioMeterInformation` observations.
On both architectures, the stopped baseline was approximately `2.33e-10` and
the first WAV playback peak was `0.06103515625` on **both** virtual endpoints.
The test failed its requirement that the unselected endpoint remain silent.
This is not counted as an isolation pass. Both endpoints belong to the same
virtual cable; Windows session and meter-capability diagnostics are needed to
distinguish shared driver metering from incorrect application routing.
Raw records: [`verification/windows-audio-d98cef0/`](verification/windows-audio-d98cef0/).

[Diagnostic run 35482393839](https://github.com/arizzi74/Smart-Stage/actions/runs/35482393839)
at test commit `776acb1` retained that isolation failure while observing six
complete playback/STOP cycles per architecture: WAV, H.264/AAC and MP3, each
played twice in the same application process. All six selected-output signal,
STOP and replay assertions succeeded on each architecture. **Both jobs still
failed the isolation assertion**, and the subsequent 120-second loop was not run.

| Target | Selected endpoint's six playing-window peak values, range | Highest endpoint value in any STOP observation | Session observations |
| --- | --- | --- | --- |
| Windows AMD64 | 0.06030–0.06497 | 2.33e-10 | Selected session active with signal during all six playing windows, inactive with peak 0 after each STOP |
| Windows ARM64 | 0.05983–0.06548 | 2.33e-10 | Same six-cycle result |

Each playing observation lasted one second; each STOP observation lasted at
least 0.7 seconds after stopped state was received. The assertion allowed a
fixed 0.25-second grace within the STOP window, although every recorded STOP
sample was already near zero. These are functional signal observations, not
measurements from server receipt of STOP or physical latency. The default
multimedia endpoint remained unchanged throughout each six-cycle check.

Both endpoints reported **16 metering channels and hardware-support mask 7**,
including the driver-provided meter bit. Per-endpoint session snapshots found
Smart Stage's active session, with signal, on the explicitly selected non-default
`CABLE In 16ch` endpoint. No Smart Stage session was observed on `CABLE Input`.
Snapshots are not a complete session-notification history. These results are
consistent with a shared driver meter and do not justify changing the native
routing implementation. They also do not establish exclusive physical output
isolation; the two endpoints are not independent speaker devices.

Raw sample windows, capabilities and process-session snapshots are retained in
[`verification/windows-audio-776acb1/`](verification/windows-audio-776acb1/).
The strict isolation assertion remains in the diagnostic script, with its
failure preserved. Further physical acceptance needs independent outputs.

Microsoft documents [endpoint peak meters](https://learn.microsoft.com/en-us/windows/win32/coreaudio/peak-meters)
as normalized render-stream observations. They can be implemented by the driver
or by the Windows audio engine and operate before endpoint volume attenuation.
They cannot prove physical sound or the specification's wired STOP latency.
