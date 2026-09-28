# Background toggles, stage draft recovery and automatic-update notice

Background cue presses toggle a runtime override independently of the saved
default. A second intentional press restores that default or black and clears
button selection, including when the override is itself the saved default.
Independent music, image overlays, stage visibility and foreground transport
remain unchanged. Pending loads are cancelled and identical request retries are
idempotent. Cue/default edits and playlist loads reconcile the override; it is
never written into configuration or exported playlist files.

Stage drafts keep a baseline of the saved settings and selected source identity.
Successful unrelated playlist edits safely advance the draft revision. Older
in-flight responses cannot roll back newer state; newer revisions received while
saving are refreshed afterward. Actual stage/source conflicts retain the draft
without overwriting host settings. A failed reload also retains it; explicit
in-section recovery succeeds without restarting or reloading the Admin document.
The server continues enforcing optimistic revisions.

A bilingual notice in the sticky Admin header distinguishes update checking,
preparation, download/verification and restart and shows the target version when
known. STOP remains accessible. Active update polling is one second, idle checks
five seconds; existing startup-only automatic installation behavior is unchanged.
Available updates waiting for next launch are not presented as already installing.
A prior failed update does not mask a current attempt; actual failure clears the
installing notice. The screenshot is synthetic browser fixture data, including its preview version,
not a real update.

Go race/vet/vulnerability checks and browser fixtures passed locally. Native
browser tests additionally exercise these background/draft flows through the
actual published-target host; audio continuity is conditional on endpoint
availability and physical playback is not claimed. No native playback-engine,
production gateway, nginx or firewall changes are part of this release.
