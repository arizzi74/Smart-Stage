# Smart Stage v1.3.0 verification evidence

Records in this directory distinguish exact candidate and tagged-source checks,
public release/download verification, and later installer/updater follow-ups.
Workflow IDs, source commits and collection times are embedded in their JSON.
Incomplete or unavailable checks remain explicitly recorded as such.

- `candidate-workflow.json` and `tag-workflow.json`: exact build gates.
- `native-scene-checks.json` and `native-scene-release-checks.json`: sanitized
  native scene observations from each architecture's uploaded harness report.
- `browser-checks.json`: synthetic-browser UI regression results; these do not
  establish native playback. Real browser/native integration is in the workflow.
- `assets.json`: independent public asset, checksum, architecture and provenance audit.
- `security-scans.json` and `govulncheck-*.txt`: exact public executable Go package
  scans, using the recorded scanner/database. They do not assess unknown logic
  defects, native OS libraries or codecs.
- `installer-defaults.json`: committed installer defaults matched to public bytes.
- `auto-update-evidence.json`, `postpublication-status.json` and
  `default-installer-workflows.json`: actual follow-up outcomes at collection time.
- `local-checks.json` and `change-scope.md`: local validation, implemented behavior
  and test scope.

Mac probes observe native layer opacity/ownership and source/audio timelines;
they do not sample final composited display pixels. Windows probes observe real
blended pixels and advancing EVR frame timestamps as well as renderer ownership.
Windows runners without an audio endpoint explicitly report audio fade coverage
unavailable; Mac native gain checks remain separate. No hosted result establishes
physical projector/speaker behavior or perceived display smoothness.

The first candidate failed an Admin checkbox browser action after native scene
checks had passed. A reproducible validation-refresh race was fixed, and the
corrected candidate is identified by its own source/run rather than treating
that failed run as passed. A second candidate exposed redundant background
inspection during Stage settings saves; unchanged, freshly resolved files now
reuse matching ready validation, and inspection errors retain their diagnostic.
The final candidate passed all seven gates before tagging. No production gateway,
proxy or firewall change was needed for this desktop feature.
