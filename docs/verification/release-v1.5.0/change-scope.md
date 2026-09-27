# Independent audio and visual fading

Admin Stage & sound has separate audio and video/image fade switches and durations.
Both default to disabled/one second, accept any finite 0.1–30 second duration,
and remain independent when saving, reloading, changing language or loading files.
Audio includes music, video soundtracks and background sound. Visual fading affects
only the picture. First sound/first picture is immediate; pause, seek and emergency
stop semantics remain unchanged.

Canonical fields are audioFadeEnabled/audioFadeSeconds and
visualFadeEnabled/visualFadeSeconds. Strict JSON migration accepts old
fadeEnabled/fadeSeconds and applies them to both groups unless an explicit new
field overrides that value, including false. Legacy backup bytes are preserved.
No schema bump, gateway deployment, nginx or firewall configuration change.

Native audio and visual durations route separately. Outgoing media remains alive
until its required audio and visual fades finish; no unbounded decoder tails.
Real native probes check disabled groups, different completion orders, background
soundtrack independence and Windows Stage-off during overlapping background fades.
Windows hosted runners may lack audio endpoints; unavailable audio checks are
reported as unverified, never counted as physical playback verification.

The first candidate exposed a Mac probe timing assumption: an absent visual timer
could mean the incoming first frame was not ready yet, not that its fade finished.
The probe now positively observes the incoming visual's readiness and transition
before asserting post-fade audio retention; gain, duration and lifetime checks
remain intact. Production playback code was unchanged by this probe correction.
Final candidate evidence refers to the corrected test source.
