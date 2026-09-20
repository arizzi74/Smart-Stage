# Preview 13 verification

Release: [v0.1.0-preview.13](https://github.com/arizzi74/Smart-Stage/releases/tag/v0.1.0-preview.13)

Source: `824698e90b360822f713840df60ae914767e21df`.
[Candidate run 35521516191](https://github.com/arizzi74/Smart-Stage/actions/runs/35521516191)
passed shared checks and all four native/browser targets.
[Tagged run 35521992281](https://github.com/arizzi74/Smart-Stage/actions/runs/35521992281)
passed all 20 jobs. The 31 JSON reports here cover:

- Six independent public ZIP downloads, checksums, clean source, architecture and matching Mac app/standalone cores.
- Four native playback/HTTP/icon targets, four public startup checks and four real browser checks.
- Both Mac app windows: real WKWebView assets, session/CSRF/EventSource, STOP and Quit, preserved close/reopen state, Cocoa hit testing and native file-URL pasteboard delivery. These checks are nested in the Mac `native-icon` reports.
- Actual public automatic discovery, download, replacement and restart on all four targets, with exact installed-byte checks.
- Both tagged Mac installers and both default Mac installers from [run 35522396312](https://github.com/arizzi74/Smart-Stage/actions/runs/35522396312).
- The public installer bootstrap hash and local synthetic browser regression checks.

The default installer source is `d2a0926be76ab0719c15cd441450e67425bc986d`;
its SHA-256 is `c170764799c0e02591983cac09f88bb78deb77d844a56c24cbe245a8b0c57db6`.
The collector verifies expected native outcomes and cross-checks hashes across
archive, startup, icon, installer and update reports. Native drop checks exercise
Cocoa hit testing and destination handlers; they do not simulate a physical
Finder mouse gesture. The browser fixture does not claim native window execution.
Physical speaker/projector routing and clean-machine acceptance remain pending.
Use the [Mac physical test procedure](../../macos-physical-test.md) for those checks.
