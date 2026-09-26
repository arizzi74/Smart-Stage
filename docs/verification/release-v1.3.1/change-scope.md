# Compact Admin layout

This patch reduces the Admin transport/title/navigation spacing, collapses empty
notice rows, and keeps the original 256 × 256 remote QR image beside connection
instructions at the top of the panel. Below 651px, Admin keeps a single column
and puts the QR before the URL. The phone remote retains its prior geometry.
Known duplicate connected text is omitted; unknown diagnostics, connection
settings, LAN firewall guidance, copy/open actions and errors remain visible.
No native playback, credential, update, playlist format or gateway protocol change
is included. Production gateway, nginx and firewall configuration are untouched.

Local validation: all 11 JavaScript tests, the existing full browser regression
suite and Go web/HTTP tests passed. Separate layout observations cover English
and Italian, gateway and LAN, desktop/tablet and narrow Admin viewports. The full
QR and caption fit at 1095 × 758, 1024 × 700, 900 × 600 and 768 × 700. Narrow phone
Admin can scroll; there is no horizontal overflow. Expanded settings and the QR
remain separate; disconnection clears the QR layout; unknown diagnostics remain.
Remote header and cue geometry match the previous CSS at 320, 390 and 768px.
Screenshots contain synthetic example.com links, never an active control secret.

Main and tag workflows ran in parallel for this UI-only patch. Exact job outcomes
are recorded separately; the candidate was not claimed to have passed before
tagging. Publication is gated by all shared, native, gateway and binary-security
jobs. Public downloads and actual update/installer execution have separate records.
