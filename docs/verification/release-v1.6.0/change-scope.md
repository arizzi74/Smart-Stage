# v1.6.0 change scope

The desktop change adds optional normalized cue levels, a runtime master level, guarded Admin volume persistence, command-role master control, English/Italian sliders and native AVPlayer/Media Foundation gain scaling. Volume changes preserve source identity, playback position, pause intent and independent audio/visual fade envelopes. Playlist files retain levels without moving media. Focused service/store/API, browser and native scene checks cover these behaviors.

No production gateway service, nginx virtual host, credentials, firewall rules or system mixer settings were changed. Master control uses the existing authenticated PLAY route; deployed gateways do not need an upgrade for this feature. Installer defaults are advanced only after the new public binaries and publication gates pass.

CI now serializes repeated public installer and updater probes per native target. The initial GitHub quota failures and Windows firewall snapshot mismatch are retained; the same tagged fixtures and published executables are used by subsequent runs.

The Windows verifier initializes the fresh runner’s Start component before its baseline. Native records retain the observed setup; the full global firewall comparison remains mandatory. This changes test setup, with no product or public installer byte changes. Earlier mismatches remain recorded.
