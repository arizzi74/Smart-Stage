# Mac app installer verification

[Run 35501797274](https://github.com/arizzi74/Smart-Stage/actions/runs/35501797274)
passed on native ARM64 and Intel Macs running macOS 15.7.9.

The installer source is `24777c368067d026379693b1ea16fdd8ed41924b`. Its published
`main/install.sh` bytes matched the checked-out script and SHA-256
`62df9f65e04153b606370274ceb023b784186b0d6540c6eda822ea5c77082fc8`.
It installs the existing preview 6 app archives, whose core source remains
`4aa3529fb9695d7b9da870dae548a5400acb7758`; no new playback binary was needed.

The reports [ARM64](arm64.json) and [AMD64](amd64.json) establish:

- Public installer download and piped shell execution, default release selection,
  archive checksums, exact installed release bytes and executable permissions.
- Native icon decoding, bundle metadata, ad-hoc signature integrity, and paths
  containing spaces, quotes and Unicode.
- Real quarantine attributes injected onto a private test copy's staged app and
  nested executable/command, then removed by the installer's normal cleanup.
  Other extended attributes, an unrelated quarantined sibling and a marker in
  the existing configuration directory stayed intact.
- Reinstallation, refusal to replace an unrelated app or symlink, corrupt archive
  rejection, restoration after replacement failure, and restoration after a HUP
  immediately following the prior app's backup rename. Original inode, bytes and
  attributes were checked; temporary work and locks were removed.
- The installer's default launch opened the installed bundle through Launch
  Services and Terminal. The serving process matched the installed core's
  kernel-reported path, Admin listened only on `127.0.0.1:8787`, and the remote
  listener rejected Admin. A running app was preserved when an update was refused.

These checks use an isolated installation parent on ephemeral hosted Macs. They
do not establish every macOS version, managed-device policy, a clean machine,
physical playback or notarization. The installer intentionally removes only
this app's quarantine attribute; Developer ID signing/notarization remain absent.
