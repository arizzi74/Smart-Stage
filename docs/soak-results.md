# Native soak results

**Resource stability is not yet established.** Successful state assertions do
not prove a stable native heap, physical blackout, routed sound or A/V drift.

## Earlier source: `20bcf35`

[Run 35468888430](https://github.com/arizzi74/Smart-Stage/actions/runs/35468888430)
completed successfully on all four targets. Each native repeat loop lasted at
least 7,200 seconds and saved 121 resource samples, followed by a successful
saved-show restart while stopped/stage-disabled. These results belong to
`20bcf3544ecfa7245a35098565ff564684e4e990`, not to the later published preview 3.

| Target | Completed cycles | Elapsed seconds | RSS first → final / sampled max (MiB) | Windows handles first → final / sampled max | RSS trend after minute 15 (MiB/hour) |
| --- | ---: | ---: | ---: | ---: | ---: |
| Mac AMD64 | 13,528 | 7,200.12 | 33.2 → 57.4 / 57.4 | — | +9.06 |
| Mac ARM64 | 13,611 | 7,200.37 | 49.2 → 75.8 / 76.2 | — | +9.95 |
| Windows AMD64 | 17,203 | 7,200.41 | 56.0 → 92.0 / 105.5 | 565 → 616 / 616 | −0.14 |
| Windows ARM64 | 18,322 | 7,200.16 | 69.4 → 67.1 / 80.0 | 566 → 604 / 613 | +0.30 |

![Full two-hour resident-memory and Windows-handle traces](verification/soak-20bcf35/resource-trends.png)

Both Mac resident-memory traces continued rising after startup. Their last
15-minute medians were 56.4 MiB (AMD64) and 75.5 MiB (ARM64), compared with
42.9/60.2 MiB in minutes 15–30. This needs attribution; RSS alone cannot establish
whether memory remains live, is awaiting reclamation, or belongs to a leak.

Windows RSS varied around a relatively steady level after startup. Handle
counts also varied but ended higher. Their fitted trends after minute 15 were
approximately +10.5/+11.4 handles/hour on AMD64/ARM64. Further diagnostics are
needed before asserting that resource growth is bounded.

Mac runners used virtual/null audio and one virtual display; Windows runners
had no audio endpoints and repeated silent video only. The script checked
PLAY/replacement/STOP state and late revival after STOP. It did not observe
physical pixels, sound, A/V drift, native heap ownership or every OS handle type.

Raw per-target reports and the numerical summary are retained in
[`verification/soak-20bcf35/`](verification/soak-20bcf35/). The graph shows all
samples. The trend column is ordinary least-squares after a fixed 15-minute
startup exclusion, selected before examining this long-run data; it is not a
pass/fail threshold or a leak detector.

To regenerate the analysis with development-only Python and Matplotlib 3.10.9:

```sh
python docs/verification/analyze-soak.py \
  docs/verification/soak-20bcf35 docs/verification/soak-20bcf35 \
  --run 35468888430 --commit 20bcf3544ecfa7245a35098565ff564684e4e990
```

## Preview 3: `d70b3e2`

The exact-source [two-hour run 35471045100](https://github.com/arizzi74/Smart-Stage/actions/runs/35471045100)
is still running. The Mac native bridge is unchanged from the earlier baseline,
but the later application must retain its own result and source attribution.

[Diagnostic run 35475182913](https://github.com/arizzi74/Smart-Stage/actions/runs/35475182913)
completed 20 minutes of native transitions against each of the four **published**
preview 3 executables, with Go GC/scavenger traces and before/after OS memory
summaries. Application commit: `d70b3e2`; diagnostic workflow commit: `34927b4`.
It changes no application code and is not another two-hour acceptance run.

| Target | Cycles | Native malloc allocations before → after | Native allocated KiB before → after | Go live heap at final GC, rounded MiB |
| --- | ---: | ---: | ---: | ---: |
| Mac AMD64 | 2,145 | 22,568 → 54,651 | 3,692 → 7,080 | 1 |
| Mac ARM64 | 2,268 | 22,923 → 56,561 | 3,408 → 6,923 | 1 |

The Mac malloc allocation count increased by roughly 15 allocations per cycle,
with about 1.6 KiB more allocated bytes per cycle. Both Go traces contained 48
collections, with reported post-GC live heap at 0–1 MiB. This implicates native
allocation retention rather than growth in the live Go heap, but it does not
identify which native objects retained those allocations.

Windows Go live heap also ended at 1 MiB. Separate process snapshots showed
552 → 571 handles and 24 → 21 threads on AMD64; 554 → 604 handles and 24 → 25
threads on ARM64. Snapshots are taken separately from the continuous RSS/handle
samples. The increased handle count therefore cannot simply be explained by
more live threads. [Run 35476607468](https://github.com/arizzi74/Smart-Stage/actions/runs/35476607468)
is collecting per-type handle counts and two minutes of stopped idle samples
against the same published executables, using Microsoft's signed
[Handle 5.0](https://learn.microsoft.com/en-us/sysinternals/downloads/handle)
diagnostic tool in summary mode. It does not close or modify application handles.

Raw GC traces, OS summaries, application reports and the parsed numerical
summary are retained in
[`verification/memory-profile-d70b3e2/`](verification/memory-profile-d70b3e2/).
The OS tools round displayed sizes; derived byte values are approximate.
Regenerate the summary with development-only Python:

```sh
python docs/verification/analyze-memory-profile.py \
  docs/verification/memory-profile-d70b3e2 --run 35475182913 \
  --application-commit d70b3e297e0e121efc6f0800b1ffe7afe5a871d1 \
  --workflow-commit 34927b483f3c4fce7f3291a5457274e82990858f
```

## Candidate Mac cleanup change: `67a10d2`

Review found that main-queue control/AVFoundation work and synchronous screen
enumeration had no explicit local autorelease pools. The Go caller's pool
belongs to another thread. [Apple documents](https://developer.apple.com/library/archive/documentation/General/Conceptual/ConcurrencyProgrammingGuide/OperationQueues/OperationQueues.html)
that dispatch queues do not guarantee when their autorelease pools drain.
The candidate adds pools around those operations and native event callbacks;
retained playback state, queued event data and the screen result survive through
their existing strong references.

[Run 35476510349](https://github.com/arizzi74/Smart-Stage/actions/runs/35476510349)
is running the same 20-minute diagnostic on both Mac architectures. The source
change is a hypothesis under test, not yet evidence that resource growth is
fixed. Published preview 3 remains unchanged.
