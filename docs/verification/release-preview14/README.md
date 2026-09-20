# Preview 14 verification

Release source: `49c9bf7051c2a5a70196dabc7e585690e7dee515`.

[Exact-source candidate run 35525447112](https://github.com/arizzi74/Smart-Stage/actions/runs/35525447112)
passed the shared checks and all four native/browser jobs.
[Tagged release run 35525752618](https://github.com/arizzi74/Smart-Stage/actions/runs/35525752618)
passed all 20 jobs, including publication, four public downloads, four public
browser checks, both Mac installs and actual automatic updates on all four targets.
All six ZIPs match their checksums and clean source metadata. The public Mac
installer defaults to preview 14 and matches its recorded repository hash. Both
Macs passed [default-install run 35526039651](https://github.com/arizzi74/Smart-Stage/actions/runs/35526039651)
without a version override; installed core/app/bootstrap hashes match the archive
and public-installer records.

Mac native scene probes observe AVPlayer volume overlap, timelines, looping,
image layers, stage cursor configuration, retirement and hard-stop cleanup.
They exercise background-to-cue, cue-to-cue and STOP-to-background crossfades,
STOP to silence and immediate start from silence. Mac browser integration also
keeps music selected through image changes and stage toggles, then stops only
music with the selected-button option.

Both Windows native probes verify looping video, images and rendered pixels,
scene identity, stage-off timeline preservation, cursor handling and hard stop.
Hosted Windows runners have no audio endpoint; Windows audio crossfades remain
unverified at runtime. They compile for both architectures. Browser reports
explicitly mark the unavailable audio checks rather than counting them as passes.

Physical speaker/projector output, physical pointer appearance, hotplug behavior,
clean-machine permission behavior and long-session acceptance remain unverified.
