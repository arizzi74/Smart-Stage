# Preview 6 verification records

Release: [v0.1.0-preview.6](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.6)
at source `4aa3529fb9695d7b9da870dae548a5400acb7758`.

- [Native/browser run 35500115900](https://github.com/arizzi74/Smart-Stage/actions/runs/35500115900)
  passed all four native targets at this source.
- [Tagged run 35500115581](https://github.com/arizzi74/Smart-Stage/actions/runs/35500115581)
  passed native checks, publication, fresh ZIP startup and real browser controls
  on all four targets. Automatic launch records establish that the OS accepted
  the browser dispatch; the separate browser records exercise the actual UI.
- `archive-verification.json` records independent public ZIP downloads, SHA-256,
  single-file contents, permissions, native headers, Go version and clean source
  revision. Both optional Mac apps contain the exact primary executable bytes.
- `download-*.json` and `browser-*.json` are the post-publication job reports.
  The downloaded executable hashes match the independent archive records.
- `smartstage-*.icon.json` and `smartstage-*.http-smoke.json` are the published
  native icon/Finder and real-application playback/restart records. Icon reports
  refer to the same downloaded executable hashes; Mac app hashes match as well.
- `browser-synthetic.json` records the local fixture check for responsive layout,
  pairing/reconnect, link refresh, clipboard fallback and STOP behavior. Its
  fixture image is not evidence of QR decoding; actual PNG decoding is covered
  by the Go QR and HTTP tests run in native CI.

No real phone camera, physical speakers/projector, hotplug, latency or
clean-machine acceptance is established by these records. The ten-second native
workloads here do not replace the separately recorded preview 4 two-hour runs.
