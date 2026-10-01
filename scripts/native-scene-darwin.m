// Test-only own-process observation. The production bridge is compiled into this
// translation unit so assertions can inspect actual AVPlayer volumes/layers;
// the shipped application contains no test endpoint or diagnostic hooks.
#import "../internal/platform/bridge_darwin.m"

static BOOL probePaused;
static uint64_t probeTransportRevision, probeSeekRevision;
static double probeSeekSeconds;
static void probeTransport(BOOL paused, uint64_t transport, uint64_t seek, double position) {
    probePaused = paused; probeTransportRevision = transport;
    probeSeekRevision = seek; probeSeekSeconds = position;
}
static void probeApplyFades(uint64_t revision, uint64_t foregroundID, NSString *foreground,
    NSString *image, NSString *background, NSString *backgroundKind, BOOL backgroundAudio,
    NSString *audio, NSString *display, BOOL stage, double audioFade, double visualFade, BOOL hard) {
    ss_scene_request request = {0};
    request.master_volume = request.foreground_volume = request.background_volume = 1;
    request.revision = revision; request.generation = foregroundID ?: revision;
    request.foreground_id = foregroundID; request.foreground_path = foreground.UTF8String ?: "";
    request.foreground_kind = foreground.length ? ([foreground.pathExtension.lowercaseString isEqual:@"mp4"] ? "video" : "audio") : "";
    request.foreground_has_audio = foreground.length != 0 && ![foreground.lastPathComponent isEqual:@"silent-1080p.mp4"];
    request.image_path = image.UTF8String ?: ""; request.background_path = background.UTF8String ?: "";
    request.background_kind = backgroundKind.UTF8String ?: ""; request.background_audio = backgroundAudio;
    request.audio = audio.UTF8String; request.display = display.UTF8String;
    request.stage_enabled = stage; request.audio_fade_seconds = audioFade;
    request.visual_fade_seconds = visualFade; request.hard_stop = hard;
    request.foreground_paused = probePaused; request.transport_revision = probeTransportRevision;
    request.seek_revision = probeSeekRevision; request.seek_seconds = probeSeekSeconds;
    ss_scene(&request);
}
static void probeApply(uint64_t revision, uint64_t foregroundID, NSString *foreground,
    NSString *image, NSString *background, NSString *backgroundKind, BOOL backgroundAudio,
    NSString *audio, NSString *display, BOOL stage, double fade, BOOL hard) {
    // Existing scene and transport regressions exercise the linked settings.
    probeApplyFades(revision, foregroundID, foreground, image, background, backgroundKind,
        backgroundAudio, audio, display, stage, fade, fade, hard);
}
static BOOL probeVolumeLevels(void) {
    float master = sceneMasterVolume;
    NSArray<SSScenePlayer *> *players = scenePlayers();
    NSMutableArray<NSNumber *> *levels = [NSMutableArray array];
    double started = sceneFadeStarted, duration = sceneFadeDuration;
    dispatch_source_t timer = sceneFadeTimer;
    BOOL valid = YES;
    sceneMasterVolume = .4f;
    for (SSScenePlayer *player in players) {
        [levels addObject:@(player.trackVolume)]; player.trackVolume = .5f;
        float envelope = player.fadeGain;
        setSceneGain(player, envelope);
        valid = valid && fabsf(player.player.volume - envelope * .2f) < .001f && player.fadeGain == envelope;
    }
    sceneMasterVolume = 0;
    BOOL activeSound = NO;
    for (SSScenePlayer *player in players) {
        BOOL wasActive = audibleScenePlayer(player); activeSound |= wasActive;
        float envelope = player.fadeGain; setSceneGain(player, envelope);
        valid = valid && player.player.volume == 0 && player.fadeGain == envelope && audibleScenePlayer(player) == wasActive;
    }
    valid = valid && activeSound;
    sceneMasterVolume = master;
    for (NSUInteger i = 0; i < players.count; ++i) {
        players[i].trackVolume = levels[i].floatValue; setSceneGain(players[i], players[i].fadeGain);
    }
    return valid && sceneFadeTimer == timer && sceneFadeStarted == started && sceneFadeDuration == duration;
}
int main(void) {
    @autoreleasepool {
        // AppKit treats positional file paths as application open-file events.
        // Keep fixture paths in the test process environment instead.
        NSString *(^environment)(const char *) = ^NSString *(const char *name) {
            return [NSString stringWithUTF8String:getenv(name) ?: ""];
        };
        NSString *audio = environment("SMARTSTAGE_SCENE_PROBE_AUDIO"), *display = environment("SMARTSTAGE_SCENE_PROBE_DISPLAY");
        NSString *first = environment("SMARTSTAGE_SCENE_PROBE_FIRST"), *second = environment("SMARTSTAGE_SCENE_PROBE_SECOND");
        NSString *background = environment("SMARTSTAGE_SCENE_PROBE_BACKGROUND"), *imagePath = environment("SMARTSTAGE_SCENE_PROBE_IMAGE");
        NSString *secondImage = environment("SMARTSTAGE_SCENE_PROBE_SECOND_IMAGE");
        if (!audio.length || !display.length || !first.length || !second.length || !background.length || !imagePath.length || !secondImage.length) return 2;
        NSString *shortAudio = [background.stringByDeletingLastPathComponent stringByAppendingPathComponent:@"Opening – café's tone.wav"];
        NSString *silentVideo = [background.stringByDeletingLastPathComponent stringByAppendingPathComponent:@"silent-1080p.mp4"];
        char *failure = ss_init();
        if (failure) { fprintf(stderr, "%s\n", failure); ss_free(failure); return 3; }
        __block NSUInteger phase = 0;
        __block BOOL passed = NO, overlap = NO, stopOverlap = NO, sawLoop = NO, silenceFade = NO;
        __block BOOL volumeLevelsVerified = NO;
        __block BOOL sawFirstPlaying = NO, sawSecondPlaying = NO, sawEnded = NO;
        __block double began = NSProcessInfo.processInfo.systemUptime, phaseBegan = began, before = 0, lastBackground = 0;
        __block AVPlayer *originalForeground;
        __block AVPlayerItem *originalItem;
        __block SSSceneImage *transportImage;
        __block SSScenePlayer *transportTail;
        __block SSScenePlayer *originalBackground, *outgoingVideo;
        __block SSSceneImage *outgoingImage;
        __block BOOL visualOverlap = NO, movingOutgoing = NO, shortVisualFadeStarted = NO;
        __block BOOL seekRacePending = NO, stopSeekPending = NO, escapeSeekPending = NO, stageSeekPending = NO, pauseSeekPending = NO;
        __block double audioFadeBeforePause = 0, visualFadeBeforePause = 0;
        __block uint64_t observedSeekSerial = 0;
        __block NSMutableSet<NSString *> *transportEvents = [NSMutableSet set];
        __block NSMutableArray *observations = [NSMutableArray array];
        dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
        dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 20 * NSEC_PER_MSEC), 20 * NSEC_PER_MSEC, NSEC_PER_MSEC);
        dispatch_source_set_event_handler(timer, ^{
            @autoreleasepool {
                // ss_quit stops AppKit asynchronously. A pending timer delivery
                // must not repeat the final report or advance after a failure.
                if (passed || atomic_load(&shuttingDown)) return;
                double now = NSProcessInfo.processInfo.systemUptime;
                if (now - began > 85) { fprintf(stderr, "Scene probe timed out at phase %lu\n", (unsigned long)phase); ss_quit(); return; }
                char *raw;
                while ((raw = ss_poll())) {
                    NSDictionary *event = [NSJSONSerialization JSONObjectWithData:[[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding] options:0 error:NULL];
                    ss_free(raw);
                    if ([event[@"kind"] isEqual:@"error"] || [event[@"kind"] isEqual:@"background-error"] || [event[@"kind"] isEqual:@"device-lost"]) {
                        fprintf(stderr, "Unexpected native event: %s\n", event.description.UTF8String); ss_quit(); return;
                    }
                    if ([event[@"kind"] isEqual:@"playing"] && [event[@"generation"] unsignedLongLongValue] == 10) sawFirstPlaying = YES;
                    if ([event[@"kind"] isEqual:@"playing"] && [event[@"generation"] unsignedLongLongValue] == 20) sawSecondPlaying = YES;
                    if ([event[@"kind"] isEqual:@"ended"] && [event[@"generation"] unsignedLongLongValue] == 30) sawEnded = YES;
                    if ([event[@"generation"] unsignedLongLongValue] >= 80) {
                        [transportEvents addObject:[NSString stringWithFormat:@"%@:%@:%@", event[@"generation"], event[@"transportRevision"], event[@"kind"]]];
                        if (([event[@"transportRevision"] unsignedLongLongValue] == 13 || [event[@"transportRevision"] unsignedLongLongValue] == 17) &&
                            ([event[@"kind"] isEqual:@"playing"] || [event[@"kind"] isEqual:@"ended"])) {
                            fprintf(stderr, "Cancelled seek delivered a late foreground event\n"); ss_quit(); return;
                        }
                    }
                }
                if (phase == 0 && sceneBackground.started && sceneBackground.player.volume > .999 && sceneBackground.layer.readyForDisplay && blackOverlay.hidden) {
                    originalBackground = sceneBackground;
                    [observations addObject:@{@"backgroundStartsImmediatelyFromSilence":@YES}];
                    phase = 1; phaseBegan = now; lastBackground = seconds(sceneBackground.player.currentTime);
                } else if (phase == 1) {
                    double position = seconds(sceneBackground.player.currentTime);
                    if (lastBackground > 2 && position < 1) sawLoop = YES;
                    lastBackground = position;
                    if (sawLoop && position > .15) {
                        [observations addObject:@{@"backgroundVideoLoops":@YES}];
                        probeApply(2, 10, first, nil, background, @"video", YES, audio, display, YES, .4, NO);
                        phase = 2; phaseBegan = now;
                    }
                } else if (phase == 2) {
                    if (!volumeLevelsVerified && sceneFadeTimer && sceneForeground.fadeGain > .05f && sceneForeground.fadeGain < .95f) {
                        if (!probeVolumeLevels()) { fprintf(stderr, "Track/master gain changed a fade envelope or failed native mute\n"); ss_quit(); return; }
                        volumeLevelsVerified = YES;
                        [observations addObject:@{@"trackAndMasterVolumeScaleNativeCrossfadeWithoutRestart":@YES}];
                    }
                    if (sceneForeground.player.volume > .05 && sceneForeground.player.volume < .95 && sceneBackground.player.volume > .05) overlap = YES;
                    if (sawFirstPlaying && sceneForeground.player.volume > .999 && sceneBackground.player.volume < .001) {
                        if (!overlap || sceneBackground != originalBackground || sceneBackground.layer.hidden) { fprintf(stderr, "No background-to-foreground crossfade or background visual\n"); ss_quit(); return; }
                        [observations addObject:@{@"backgroundToCueCrossfade":@YES,@"backgroundKeepsLoopingMuted":@YES}];
                        originalForeground = sceneForeground.player; before = seconds(originalForeground.currentTime);
                        probeApply(3, 10, first, nil, background, @"video", YES, audio, display, NO, .4, NO);
                        phase = 3; phaseBegan = now;
                    }
                } else if (phase == 3 && now - phaseBegan > .3) {
                    if (stageEnabled || stageWindow.isVisible || sceneForeground.player != originalForeground || seconds(originalForeground.currentTime) <= before + .1 || originalForeground.volume < .999) {
                        fprintf(stderr, "Stage-off interrupted foreground audio\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"stageOffPreservesForegroundTimelineAndVolume":@YES}];
                    probeApply(4, 10, first, imagePath, background, @"video", YES, audio, display, YES, .4, NO);
                    phase = 4; phaseBegan = now;
                } else if (phase == 4 && sceneImage.layer.contents && !sceneImage.layer.hidden && stageWindow.isVisible && !sceneVisualTimer) {
                    if (sceneForeground.player != originalForeground || originalForeground.volume < .999 || !sceneBackground.layer.hidden) {
                        fprintf(stderr, "Image interrupted foreground or failed overlay priority\n"); ss_quit(); return;
                    }
                    SSStageView *view = (SSStageView *)stageWindow.contentView;
                    [view updateTrackingAreas];
                    if (!(view.stageTracking.options & NSTrackingActiveAlways) || [view transparentCursor].image.size.width != 1) {
                        fprintf(stderr, "Stage cursor is not configured\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"imageOverlayPreservesMusic":@YES,@"transparentStageCursorTracking":@YES}];
                    overlap = NO;
                    probeApply(5, 20, second, nil, background, @"video", YES, audio, display, YES, .4, NO);
                    phase = 5; phaseBegan = now;
                } else if (phase == 5) {
                    if (sceneForeground.player.volume > .05 && sceneForeground.player.volume < .95 && sceneRetiring.player.volume > .05) overlap = YES;
                    if (sawSecondPlaying && sceneForeground.player.volume > .999 && !sceneRetiring) {
                        if (!overlap || sceneForeground.player == originalForeground || originalForeground.currentItem != nil) {
                            fprintf(stderr, "No cue-to-cue crossfade or outgoing decoder was retained\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"cueToCueCrossfade":@YES,@"retiredDecoderReleased":@YES}];
                        probeApply(6, 0, nil, nil, background, @"video", YES, audio, display, YES, .4, NO);
                        phase = 6; phaseBegan = now;
                    }
                } else if (phase == 6) {
                    if (sceneBackground.player.volume > .05 && sceneBackground.player.volume < .95 && sceneRetiring.player.volume > .05) stopOverlap = YES;
                    if (sceneBackground.player.volume > .999 && !sceneRetiring) {
                        if (!stopOverlap || sceneForeground || sceneBackground != originalBackground) {
                            fprintf(stderr, "STOP did not crossfade back to existing background\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"stopCrossfadesToExistingBackground":@YES}];
                        probeApply(7, 0, nil, nil, imagePath, @"image", NO, audio, display, YES, .4, NO);
                        phase = 7; phaseBegan = now;
                    }
                } else if (phase == 7 && sceneBackgroundImage.layer.contents && !sceneBackgroundImage.layer.hidden && !sceneRetiring && !sceneVisualTimer) {
                    [observations addObject:@{@"backgroundImageDecodedAndVisible":@YES}];
                    probeApply(8, 10, first, nil, imagePath, @"image", NO, audio, display, YES, .4, NO);
                    phase = 8; phaseBegan = now;
                } else if (phase == 8 && sceneForeground.started) {
                    if (sceneForeground.player.volume < .999) { fprintf(stderr, "Fresh audio faded in from silence\n"); ss_quit(); return; }
                    [observations addObject:@{@"foregroundStartsImmediatelyFromSilence":@YES}];
                    probeApply(9, 0, nil, nil, imagePath, @"image", NO, audio, display, YES, .4, NO);
                    phase = 9; phaseBegan = now;
                } else if (phase == 9) {
                    if (sceneRetiring.player.volume > .05 && sceneRetiring.player.volume < .95) silenceFade = YES;
                    if (!sceneRetiring) {
                        if (!silenceFade) { fprintf(stderr, "STOP did not fade to silence\n"); ss_quit(); return; }
                        [observations addObject:@{@"stopFadesToSilenceWithoutBackgroundSoundtrack":@YES}];
                        // A pending PLAY immediately followed by hard STOP must
                        // never allow the superseded decoder to reveal/play.
                        probeApply(10, 20, second, nil, background, @"video", YES, audio, display, YES, .4, NO);
                        probeApply(11, 0, nil, nil, nil, nil, NO, audio, display, NO, .4, YES);
                        phase = 10; phaseBegan = now;
                    }
                } else if (phase == 10 && now - phaseBegan > .2) {
                    if (sceneForeground || sceneBackground || sceneRetiring || stageEnabled || stageWindow.isVisible || sceneFadeTimer || sceneVisualTimer || sceneVisualCurrent || sceneVisualOutgoing) {
                        fprintf(stderr, "Hard STOP left a native renderer alive\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"latestSceneWinsRapidPlayThenHardStop":@YES,@"hardStopCancelsAllFadesAndHidesStage":@YES}];
                    probeApply(12, 30, shortAudio, nil, background, @"video", YES, audio, display, YES, .4, NO);
                    phase = 11; phaseBegan = now;
                } else if (phase == 11 && sawEnded && !sceneForeground && sceneBackground.player.volume > .999) {
                    [observations addObject:@{@"naturalEndReturnsToBackgroundAudio":@YES}];
                    // Model a stage command already in flight when the native
                    // foreground reaches its end: its ID must never restart.
                    probeApply(13, 30, shortAudio, nil, background, @"video", YES, audio, display, YES, .4, NO);
                    phase = 12; phaseBegan = now;
                } else if (phase == 12 && now - phaseBegan > .2) {
                    if (sceneForeground) { fprintf(stderr, "Ended foreground was resurrected by a stage scene\n"); ss_quit(); return; }
                    [observations addObject:@{@"lateStageSceneCannotRestartEndedForeground":@YES}];
                    probeApply(14, 0, nil, nil, nil, nil, NO, audio, display, NO, .4, YES);
                    phase = 13; phaseBegan = now;
                } else if (phase == 13 && now - phaseBegan > .1) {
                    probeApply(15, 40, background, nil, nil, nil, NO, audio, display, YES, .5, NO);
                    phase = 14; phaseBegan = now;
                } else if (phase == 14 && sceneForeground.layer.readyForDisplay && sceneForeground.started && sceneVisualCurrent == sceneForeground && !sceneVisualTimer) {
                    outgoingVideo = sceneForeground; before = seconds(outgoingVideo.player.currentTime);
                    visualOverlap = NO; movingOutgoing = NO;
                    probeApply(16, 0, nil, imagePath, nil, nil, NO, audio, display, YES, .5, NO);
                    phase = 15; phaseBegan = now;
                } else if (phase == 15) {
                    if (sceneVisualOutgoing == outgoingVideo && sceneImage.layer.opacity > .05 && sceneImage.layer.opacity < .95 && !outgoingVideo.layer.hidden) {
                        visualOverlap = YES;
                        if (seconds(outgoingVideo.player.currentTime) > before + .03) movingOutgoing = YES;
                    }
                    if (sceneVisualCurrent == sceneImage && sceneImage.layer.contents && !sceneVisualTimer && !sceneRetiring) {
                        if (!visualOverlap || !movingOutgoing || !outgoingVideo.disposed || outgoingVideo.player) {
                            fprintf(stderr, "Video-to-image failed to retain moving video or release its decoder\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"videoToImageVisualCrossfade":@YES,@"outgoingVideoKeepsMovingDuringVisualFade":@YES,@"visualTailDecoderReleased":@YES}];
                        probeApply(17, 50, first, imagePath, nil, nil, NO, audio, display, YES, .5, NO);
                        phase = 16; phaseBegan = now;
                    }
                } else if (phase == 16 && sceneForeground.started && sceneForeground.player.volume > .999) {
                    originalForeground = sceneForeground.player; before = seconds(originalForeground.currentTime);
                    outgoingImage = sceneImage; visualOverlap = NO;
                    probeApply(18, 50, first, secondImage, nil, nil, NO, audio, display, YES, .5, NO);
                    phase = 17; phaseBegan = now;
                } else if (phase == 17) {
                    if (sceneVisualOutgoing == outgoingImage && sceneImage.layer.opacity > .05 && sceneImage.layer.opacity < .95 && !outgoingImage.layer.hidden) visualOverlap = YES;
                    if ([sceneImage.path isEqual:secondImage] && sceneImage.layer.contents && !sceneVisualTimer) {
                        if (!visualOverlap || !outgoingImage.disposed || sceneForeground.player != originalForeground || originalForeground.volume < .999 || seconds(originalForeground.currentTime) <= before + .2) {
                            fprintf(stderr, "Image-to-image fade interrupted music or retained old image\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"imageToImageCrossfadePreservesMusicTimelineAndGain":@YES}];
                        outgoingImage = sceneImage; visualOverlap = NO;
                        probeApply(19, 60, background, nil, nil, nil, NO, audio, display, YES, .5, NO);
                        phase = 18; phaseBegan = now;
                    }
                } else if (phase == 18) {
                    if (sceneVisualOutgoing == outgoingImage && sceneForeground.layer.readyForDisplay && sceneForeground.layer.opacity > .05 && sceneForeground.layer.opacity < .95) visualOverlap = YES;
                    if (sceneVisualCurrent == sceneForeground && sceneForeground.layer.readyForDisplay && !sceneVisualTimer && !sceneRetiring) {
                        if (!visualOverlap || !outgoingImage.disposed) { fprintf(stderr, "Image-to-video crossfade missing\n"); ss_quit(); return; }
                        [observations addObject:@{@"imageToReadyVideoCrossfade":@YES}];
                        outgoingVideo = sceneForeground; before = seconds(outgoingVideo.player.currentTime);
                        visualOverlap = NO; movingOutgoing = NO;
                        probeApply(20, 70, background, nil, nil, nil, NO, audio, display, YES, .5, NO);
                        phase = 19; phaseBegan = now;
                    }
                } else if (phase == 19) {
                    if (sceneVisualOutgoing == outgoingVideo && sceneForeground != outgoingVideo && sceneForeground.layer.opacity > .05 && sceneForeground.layer.opacity < .95) {
                        visualOverlap = YES;
                        if (seconds(outgoingVideo.player.currentTime) > before + .03) movingOutgoing = YES;
                    }
                    if (sceneForeground.identifier == 70 && sceneVisualCurrent == sceneForeground && sceneForeground.layer.readyForDisplay && !sceneVisualTimer && !sceneRetiring) {
                        if (!visualOverlap || !movingOutgoing || !outgoingVideo.disposed) { fprintf(stderr, "Video-to-video crossfade missing or leaked outgoing decoder\n"); ss_quit(); return; }
                        [observations addObject:@{@"videoToVideoCrossfadeKeepsOutgoingTimeline":@YES}];
                        outgoingVideo = sceneForeground; visualOverlap = NO;
                        probeApply(21, 0, nil, nil, nil, nil, NO, audio, display, YES, .5, NO);
                        phase = 20; phaseBegan = now;
                    }
                } else if (phase == 20) {
                    if (sceneVisualOutgoing == outgoingVideo && !sceneVisualCurrent && outgoingVideo.layer.opacity > .05 && outgoingVideo.layer.opacity < .95) visualOverlap = YES;
                    if (!sceneVisualTimer && !sceneVisualCurrent && !sceneVisualOutgoing && !sceneRetiring) {
                        if (!visualOverlap || blackOverlay.hidden || !outgoingVideo.disposed || !stageEnabled) { fprintf(stderr, "Video did not fade to black and release its decoder\n"); ss_quit(); return; }
                        [observations addObject:@{@"videoFadesToBlackWithStageStillEnabled":@YES}];
                        probeApply(22, 50, first, imagePath, secondImage, @"image", NO, audio, display, YES, .5, NO);
                        phase = 21; phaseBegan = now;
                    }
                } else if (phase == 21 && sceneImage.layer.contents && sceneBackgroundImage.layer.contents && sceneForeground.started && !sceneVisualTimer) {
                    originalForeground = sceneForeground.player; outgoingImage = sceneImage; visualOverlap = NO;
                    probeApply(23, 50, first, nil, secondImage, @"image", NO, audio, display, YES, .5, NO);
                    phase = 22; phaseBegan = now;
                } else if (phase == 22) {
                    if (sceneVisualOutgoing == outgoingImage && sceneBackgroundImage.layer.opacity > .05 && sceneBackgroundImage.layer.opacity < .95) visualOverlap = YES;
                    if (sceneVisualCurrent == sceneBackgroundImage && !sceneVisualTimer) {
                        if (!visualOverlap || sceneForeground.player != originalForeground || originalForeground.volume < .999) { fprintf(stderr, "Image stop did not crossfade to background while preserving music\n"); ss_quit(); return; }
                        [observations addObject:@{@"imageReturnsToBackgroundWithoutStoppingMusic":@YES}];
                        probeApply(24, 50, first, imagePath, secondImage, @"image", NO, audio, display, YES, 1, NO);
                        phase = 23; phaseBegan = now;
                    }
                } else if (phase == 23 && sceneVisualTimer && sceneImage.layer.opacity > .1 && sceneImage.layer.opacity < .7) {
                    probeApply(25, 50, first, nil, secondImage, @"image", NO, audio, display, YES, 1, NO);
                    phase = 24; phaseBegan = now;
                } else if (phase == 24 && appliedSceneRevision == 25 && !sceneVisualTimer) {
                    if (sceneVisualCurrent != sceneBackgroundImage || sceneVisualOutgoing || sceneForeground.player != originalForeground) { fprintf(stderr, "Rapid visual retarget left a stale layer or changed music\n"); ss_quit(); return; }
                    [observations addObject:@{@"rapidVisualRetargetKeepsOnlyLatestScene":@YES}];
                    probeApply(26, 50, first, imagePath, secondImage, @"image", NO, audio, display, YES, 1, NO);
                    phase = 25; phaseBegan = now;
                } else if (phase == 25 && sceneVisualTimer && sceneImage.layer.opacity > .1 && sceneImage.layer.opacity < .7) {
                    probeApply(27, 50, first, imagePath, secondImage, @"image", NO, audio, display, NO, 1, NO);
                    phase = 26; phaseBegan = now;
                } else if (phase == 26 && appliedSceneRevision == 27) {
                    if (stageEnabled || stageWindow.isVisible || sceneVisualTimer || sceneVisualCurrent || sceneVisualOutgoing || blackOverlay.hidden || sceneForeground.player != originalForeground) { fprintf(stderr, "Stage-off did not cancel visual fade immediately while retaining music\n"); ss_quit(); return; }
                    [observations addObject:@{@"stageOffImmediatelyCancelsVisualFadeAndPreservesMusic":@YES}];
                    probeApply(28, 50, first, imagePath, secondImage, @"image", NO, audio, display, YES, .5, NO);
                    phase = 27; phaseBegan = now;
                } else if (phase == 27 && stageEnabled && sceneVisualCurrent == sceneImage && !sceneVisualTimer) {
                    probeApply(29, 50, first, nil, secondImage, @"image", NO, audio, display, YES, 1, NO);
                    phase = 28; phaseBegan = now;
                } else if (phase == 28 && sceneVisualTimer) {
                    sceneEmergency(); phase = 29; phaseBegan = now;
                } else if (phase == 29 && now - phaseBegan > .2) {
                    if (sceneVisualTimer || sceneVisualCurrent || sceneVisualOutgoing || sceneForeground || sceneBackgroundImage || stageEnabled || stageWindow.isVisible) { fprintf(stderr, "Escape left visual sources alive\n"); ss_quit(); return; }
                    [observations addObject:@{@"escapeCancelsVisualFadeAndPreventsLateReveal":@YES}];
                    probeApply(30, 0, nil, imagePath, nil, nil, NO, audio, display, YES, .5, NO);
                    phase = 30; phaseBegan = now;
                } else if (phase == 30 && sceneImage.layer.contents && sceneVisualCurrent == sceneImage) {
                    outgoingImage = sceneImage; visualOverlap = NO;
                    probeApply(31, 0, nil, nil, nil, nil, NO, audio, display, YES, .5, NO);
                    phase = 31; phaseBegan = now;
                } else if (phase == 31) {
                    if (sceneVisualOutgoing == outgoingImage && !sceneVisualCurrent && outgoingImage.layer.opacity > .05 && outgoingImage.layer.opacity < .95) visualOverlap = YES;
                    if (!sceneVisualTimer && !sceneVisualCurrent && !sceneVisualOutgoing) {
                        if (!visualOverlap || blackOverlay.hidden || !outgoingImage.disposed || !stageEnabled) { fprintf(stderr, "Image did not fade to black\n"); ss_quit(); return; }
                        [observations addObject:@{@"imageFadesToBlackAndReleasesRaster":@YES}];
                        probeApply(32, 0, nil, imagePath, nil, nil, NO, audio, display, YES, .5, NO);
                        phase = 32; phaseBegan = now;
                    }
                } else if (phase == 32 && sceneImage.layer.contents && sceneVisualCurrent == sceneImage) {
                    probeApply(33, 0, nil, secondImage, nil, nil, NO, audio, display, YES, 1, NO);
                    phase = 33; phaseBegan = now;
                } else if (phase == 33 && sceneVisualTimer) {
                    probeApply(34, 0, nil, nil, nil, nil, NO, audio, display, NO, 1, YES);
                    phase = 34; phaseBegan = now;
                } else if (phase == 34 && appliedSceneRevision == 34 && now - phaseBegan > .1) {
                    if (sceneVisualTimer || sceneVisualCurrent || sceneVisualOutgoing || sceneImage || sceneForeground || stageEnabled || stageWindow.isVisible) { fprintf(stderr, "Hard STOP did not immediately dispose visual transition\n"); ss_quit(); return; }
                    [observations addObject:@{@"hardStopImmediatelyCancelsActiveVisualTransition":@YES}];
                    probeApply(35, 0, nil, imagePath, nil, nil, NO, audio, display, YES, 0, NO);
                    phase = 35; phaseBegan = now;
                } else if (phase == 35 && sceneImage.layer.contents && sceneVisualCurrent == sceneImage) {
                    outgoingImage = sceneImage;
                    probeApply(36, 0, nil, secondImage, nil, nil, NO, audio, display, YES, 0, NO);
                    phase = 36; phaseBegan = now;
                } else if (phase == 36 && [sceneImage.path isEqual:secondImage] && sceneImage.layer.contents) {
                    if (sceneVisualTimer || sceneVisualOutgoing || sceneVisualCurrent != sceneImage || sceneImage.layer.opacity != 1 || !outgoingImage.disposed) { fprintf(stderr, "Disabled visual fade did not switch immediately\n"); ss_quit(); return; }
                    [observations addObject:@{@"disabledVisualFadeSwitchesImmediately":@YES}];
                    probeTransport(NO, 1, 0, 0);
                    probeApply(37, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 37; phaseBegan = now;
                } else if (phase == 37 && sceneForeground.started && sceneForeground.player.volume > .999 &&
                           seconds(sceneForeground.player.currentTime) > .35 && !sceneVisualTimer) {
                    originalForeground = sceneForeground.player; originalItem = sceneForeground.item;
                    originalBackground = sceneBackground; transportImage = sceneImage;
                    before = seconds(originalForeground.currentTime);
                    probeTransport(YES, 2, 0, 0);
                    probeApply(38, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 38; phaseBegan = now;
                } else if (phase == 38 && appliedSceneRevision == 38 && now - phaseBegan > .25) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - before) > .08 || !originalForeground.muted ||
                        sceneForeground.player.volume < .999 || sceneBackground != originalBackground ||
                        (sceneBackground.player.volume > .001 && !sceneBackground.player.muted) || sceneImage != transportImage ||
                        sceneImage.layer.hidden || !stageEnabled || ![transportEvents containsObject:@"80:2:paused"]) {
                        fprintf(stderr, "Audio pause moved the clock, replaced the decoder, or returned to background\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"audioPauseFreezesNativeClockAndRetainsDecoder":@YES,
                        @"audioPausePreservesImageAndBackgroundOwnership":@YES}];
                    probeApply(39, 80, first, imagePath, background, @"video", YES, audio, display, NO, .8, NO);
                    phase = 39; phaseBegan = now;
                } else if (phase == 39 && appliedSceneRevision == 39 && now - phaseBegan > .15) {
                    if (stageEnabled || sceneForeground.player != originalForeground || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - before) > .08) {
                        fprintf(stderr, "Stage-off resumed paused audio\n"); ss_quit(); return;
                    }
                    probeApply(40, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 40; phaseBegan = now;
                } else if (phase == 40 && appliedSceneRevision == 40 && now - phaseBegan > .2) {
                    if (!stageEnabled || sceneForeground.player != originalForeground || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - before) > .08 || sceneImage != transportImage) {
                        fprintf(stderr, "Stage-on resumed paused audio or replaced its image\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"stageChangesRetainPausedAudioAndImage":@YES}];
                    probeTransport(YES, 3, 1, 5.25);
                    probeApply(41, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 41; phaseBegan = now;
                } else if (phase == 41 && sceneForeground.completedSeekRevision == 1 && !sceneForeground.seeking && now - phaseBegan > .2) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - 5.25) > .06 || ![transportEvents containsObject:@"80:3:paused"]) {
                        fprintf(stderr, "Paused audio seek lost its position, pause, or decoder\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"pausedAudioSeekUsesRequestedNativePositionWithoutPlayback":@YES}];
                    before = seconds(originalForeground.currentTime); observedSeekSerial = sceneForeground.seekSerial;
                    probeApply(42, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 42; phaseBegan = now;
                } else if (phase == 42 && appliedSceneRevision == 42 && now - phaseBegan > .2) {
                    if (sceneForeground.seekSerial != observedSeekSerial || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - before) > .06) {
                        fprintf(stderr, "Unchanged seek intent repeated on unrelated scene update\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"unchangedSeekRevisionDoesNotRepeatNativeSeek":@YES}];
                    probeTransport(NO, 4, 1, 5.25);
                    probeApply(43, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 43; phaseBegan = now;
                } else if (phase == 43 && originalForeground.rate > 0 && seconds(originalForeground.currentTime) > before + .2) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem ||
                        seconds(originalForeground.currentTime) > before + 1.2 || ![transportEvents containsObject:@"80:4:playing"]) {
                        fprintf(stderr, "Audio resume restarted instead of continuing the paused clock\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"audioResumeContinuesSameNativeDecoderAndClock":@YES}];
                    probeTransport(NO, 5, 2, 9);
                    probeApply(44, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 44; phaseBegan = now;
                } else if (phase == 44 && sceneForeground.completedSeekRevision == 2 && !sceneForeground.seeking && seconds(originalForeground.currentTime) > 9.15) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem || originalForeground.rate <= 0 ||
                        seconds(originalForeground.currentTime) > 10.2 || ![transportEvents containsObject:@"80:5:playing"]) {
                        fprintf(stderr, "Playing audio seek did not continue from its native target\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"playingAudioSeekPreservesPlaybackAndDecoder":@YES}];
                    probeTransport(NO, 6, 3, 15);
                    probeApply(45, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    // This block follows the actual first seek application but
                    // precedes its queued main-thread completion. The next API
                    // request invalidates that completion before it can play.
                    onMain(^{
                        seekRacePending = sceneForeground.seeking;
                        probeTransport(NO, 7, 4, 2);
                        probeApply(46, 80, first, imagePath, background, @"video", YES, audio, display, YES, .8, NO);
                    });
                    phase = 45; phaseBegan = now;
                } else if (phase == 45 && appliedSceneRevision == 46 && sceneForeground.completedSeekRevision == 4 &&
                           !sceneForeground.seeking && seconds(originalForeground.currentTime) > 2.15) {
                    if (!seekRacePending || sceneForeground.player != originalForeground || originalForeground.rate <= 0 ||
                        seconds(originalForeground.currentTime) > 3.2 || [transportEvents containsObject:@"80:6:playing"] ||
                        ![transportEvents containsObject:@"80:7:playing"]) {
                        fprintf(stderr, "A superseded in-flight seek won over the latest native intent\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"latestAudioSeekWinsOverInFlightCompletion":@YES}];
                    [NSNotificationCenter.defaultCenter postNotificationName:AVPlayerItemDidPlayToEndTimeNotification object:originalItem];
                    phase = 46; phaseBegan = now;
                } else if (phase == 46 && now - phaseBegan > .15) {
                    if (sceneForeground.player != originalForeground || originalForeground.rate <= 0 ||
                        [transportEvents containsObject:@"80:7:ended"]) {
                        fprintf(stderr, "Stale native end notification retired the newly sought cue\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"staleEndAfterSeekCannotRetireForeground":@YES}];
                    probeTransport(NO, 8, 0, 0);
                    probeApply(47, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 47; phaseBegan = now;
                } else if (phase == 47 && sceneForeground.started && sceneForeground.layer.readyForDisplay &&
                           sceneVisualCurrent == sceneForeground && sceneFadeTimer && sceneVisualTimer &&
                           sceneForeground.player.volume > .1 && sceneForeground.player.volume < .7) {
                    originalForeground = sceneForeground.player; originalItem = sceneForeground.item;
                    transportTail = sceneRetiring; before = seconds(originalForeground.currentTime);
                    audioFadeBeforePause = sceneFadeStarted; visualFadeBeforePause = sceneVisualStarted;
                    probeTransport(YES, 9, 0, 0);
                    probeApply(48, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 48; phaseBegan = now;
                } else if (phase == 48 && appliedSceneRevision == 48 && now - phaseBegan > .2) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - before) > .08 || !sceneForeground.layer.readyForDisplay ||
                        sceneForeground.layer.hidden || sceneBackground != originalBackground ||
                        sceneFadeStarted != audioFadeBeforePause || sceneVisualStarted != visualFadeBeforePause ||
                        (transportTail.player && (!transportTail.player.muted || transportTail.player.volume > .001)) ||
                        ![transportEvents containsObject:@"90:9:paused"]) {
                        fprintf(stderr, "Video pause moved the frame, restarted a fade, or left its retiring audio audible\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"videoPauseFreezesNativeClockAndRetainsVisibleFrame":@YES,
                        @"pausePreservesFadeClocksAndSilencesRetiringForegroundAudio":@YES}];
                    probeTransport(YES, 10, 1, 1.5);
                    probeApply(49, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 49; phaseBegan = now;
                } else if (phase == 49 && sceneForeground.completedSeekRevision == 1 && !sceneForeground.seeking && now - phaseBegan > .2) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - 1.5) > .06 || !sceneForeground.layer.readyForDisplay ||
                        ![transportEvents containsObject:@"90:10:paused"]) {
                        fprintf(stderr, "Paused video seek failed to hold its requested native frame\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"pausedVideoSeekHoldsRequestedFrameWithSameDecoder":@YES}];
                    probeApply(50, 90, background, nil, background, @"video", YES, audio, display, NO, .8, NO);
                    phase = 50; phaseBegan = now;
                } else if (phase == 50 && appliedSceneRevision == 50 && now - phaseBegan > .1) {
                    if (stageEnabled || originalForeground.rate != 0 || fabs(seconds(originalForeground.currentTime) - 1.5) > .06) {
                        fprintf(stderr, "Stage-off changed paused video transport\n"); ss_quit(); return;
                    }
                    probeApply(51, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 51; phaseBegan = now;
                } else if (phase == 51 && appliedSceneRevision == 51 && now - phaseBegan > .15) {
                    if (sceneForeground.player != originalForeground || originalForeground.rate != 0 || !stageEnabled ||
                        sceneForeground.layer.hidden || fabs(seconds(originalForeground.currentTime) - 1.5) > .06) {
                        fprintf(stderr, "Stage-on restarted or hid paused video\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"stageChangesRetainPausedVideoFrameAndClock":@YES}];
                    probeTransport(NO, 11, 1, 1.5);
                    probeApply(52, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 52; phaseBegan = now;
                } else if (phase == 52 && originalForeground.rate > 0 && seconds(originalForeground.currentTime) > 1.65) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem ||
                        seconds(originalForeground.currentTime) > 2.5 || ![transportEvents containsObject:@"90:11:playing"]) {
                        fprintf(stderr, "Video resume failed native timeline continuity\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"videoResumeContinuesSameNativeDecoderAndClock":@YES}];
                    probeTransport(NO, 12, 2, .4);
                    probeApply(53, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 53; phaseBegan = now;
                } else if (phase == 53 && sceneForeground.completedSeekRevision == 2 && !sceneForeground.seeking &&
                           originalForeground.rate > 0 && seconds(originalForeground.currentTime) > .55) {
                    if (sceneForeground.player != originalForeground || sceneForeground.item != originalItem ||
                        seconds(originalForeground.currentTime) > 1.4 || ![transportEvents containsObject:@"90:12:playing"]) {
                        fprintf(stderr, "Playing video seek failed to resume at its native target\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"playingVideoSeekPreservesPlaybackAndDecoder":@YES}];
                    probeTransport(NO, 13, 3, 2);
                    probeApply(54, 90, background, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    onMain(^{
                        stopSeekPending = sceneForeground.seeking;
                        outgoingVideo = sceneForeground;
                        probeTransport(NO, 14, 0, 0);
                        probeApply(55, 0, nil, nil, nil, nil, NO, audio, display, NO, .8, YES);
                    });
                    phase = 54; phaseBegan = now;
                } else if (phase == 54 && appliedSceneRevision == 55 && now - phaseBegan > .3) {
                    if (!stopSeekPending || sceneForeground || sceneBackground || sceneRetiring || sceneFadeTimer || sceneVisualTimer ||
                        stageEnabled || stageWindow.isVisible || originalForeground.currentItem || originalForeground.rate != 0 || !outgoingVideo.disposed) {
                        fprintf(stderr, "Hard STOP lost to an in-flight seek completion\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"hardStopDuringNativeSeekPreventsLatePlaybackAndReveal":@YES}];
                    probeTransport(YES, 15, 0, 0);
                    probeApply(56, 100, background, nil, nil, nil, NO, audio, display, YES, .8, NO);
                    phase = 55; phaseBegan = now;
                } else if (phase == 55 && sceneForeground.layer.readyForDisplay && now - phaseBegan > .25) {
                    if (sceneForeground.started || sceneForeground.player.rate != 0 || !sceneForeground.player.muted ||
                        seconds(sceneForeground.player.currentTime) > .03 || sceneForeground.layer.hidden ||
                        ![transportEvents containsObject:@"100:15:paused"] || [transportEvents containsObject:@"100:15:playing"]) {
                        fprintf(stderr, "Initially paused video briefly played or failed to prepare its first frame\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"pauseBeforeVideoReadyDecodesFirstFrameWithoutStartingClock":@YES}];
                    originalForeground = sceneForeground.player;
                    probeTransport(NO, 16, 0, 0);
                    probeApply(57, 100, background, nil, nil, nil, NO, audio, display, YES, .8, NO);
                    phase = 56; phaseBegan = now;
                } else if (phase == 56 && sceneForeground.started && seconds(originalForeground.currentTime) > .15) {
                    probeTransport(NO, 17, 1, 2);
                    probeApply(58, 100, background, nil, nil, nil, NO, audio, display, YES, .8, NO);
                    onMain(^{
                        escapeSeekPending = sceneForeground.seeking;
                        outgoingVideo = sceneForeground;
                        sceneEmergency();
                    });
                    phase = 57; phaseBegan = now;
                } else if (phase == 57 && now - phaseBegan > .3) {
                    if (!escapeSeekPending || sceneForeground || sceneBackground || sceneRetiring || sceneVisualCurrent || sceneVisualOutgoing ||
                        stageEnabled || stageWindow.isVisible || originalForeground.currentItem || originalForeground.rate != 0 || !outgoingVideo.disposed) {
                        fprintf(stderr, "Escape lost to an in-flight seek completion\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"escapeDuringNativeSeekPreventsLatePlaybackAndReveal":@YES}];
                    probeTransport(YES, 18, 0, 0);
                    probeApply(59, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 58; phaseBegan = now;
                } else if (phase == 58 && sceneForeground.layer.readyForDisplay && sceneBackground.started &&
                           sceneBackground.player.volume > .999 && seconds(sceneBackground.player.currentTime) > .2) {
                    originalForeground = sceneForeground.player; originalBackground = sceneBackground;
                    before = seconds(sceneBackground.player.currentTime);
                    phase = 59; phaseBegan = now;
                } else if (phase == 59 && now - phaseBegan > .25) {
                    if (sceneForeground.player != originalForeground || originalForeground.rate != 0 ||
                        seconds(originalForeground.currentTime) > .03 || sceneForeground.layer.hidden ||
                        sceneBackground != originalBackground || sceneBackground.player.volume < .999 || sceneBackground.player.muted ||
                        seconds(sceneBackground.player.currentTime) < before + .15) {
                        fprintf(stderr, "Paused silent video interrupted its independent background soundtrack\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"silentVideoPauseKeepsIndependentBackgroundAudioAndClock":@YES}];
                    probeTransport(YES, 19, 1, 99);
                    probeApply(60, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 60; phaseBegan = now;
                } else if (phase == 60 && sceneForeground.completedSeekRevision == 1 && !sceneForeground.seeking && now - phaseBegan > .15) {
                    if (sceneForeground.player != originalForeground || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - seconds(sceneForeground.item.duration)) > .06) {
                        fprintf(stderr, "Native seek did not clamp to finite duration while retaining pause\n"); ss_quit(); return;
                    }
                    probeTransport(YES, 20, 2, -5);
                    probeApply(61, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 61; phaseBegan = now;
                } else if (phase == 61 && sceneForeground.completedSeekRevision == 2 && !sceneForeground.seeking && now - phaseBegan > .15) {
                    if (sceneForeground.player != originalForeground || originalForeground.rate != 0 || seconds(originalForeground.currentTime) > .03) {
                        fprintf(stderr, "Native seek did not clamp a negative target to zero\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"nativeSeekClampsBothFiniteTimelineBounds":@YES}];
                    probeTransport(YES, 21, 3, 1.2);
                    probeApply(62, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    onMain(^{
                        stageSeekPending = sceneForeground.seeking;
                        probeApply(63, 110, silentVideo, nil, background, @"video", YES, audio, display, NO, .8, NO);
                    });
                    phase = 62; phaseBegan = now;
                } else if (phase == 62 && appliedSceneRevision == 63 && sceneForeground.completedSeekRevision == 3 &&
                           !sceneForeground.seeking && now - phaseBegan > .2) {
                    if (!stageSeekPending || stageEnabled || sceneForeground.player != originalForeground || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - 1.2) > .06 || ![transportEvents containsObject:@"110:21:paused"]) {
                        fprintf(stderr, "Stage update lost the completion of the current paused seek\n"); ss_quit(); return;
                    }
                    observedSeekSerial = sceneForeground.seekSerial;
                    probeApply(64, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 63; phaseBegan = now;
                } else if (phase == 63 && appliedSceneRevision == 64 && now - phaseBegan > .15) {
                    if (sceneForeground.seekSerial != observedSeekSerial || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - 1.2) > .06 || sceneForeground.layer.hidden) {
                        fprintf(stderr, "Stage restoration repeated the completed seek or resumed video\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"stageDuringNativeSeekRetainsCompletionAndPauseWithoutRepeatingSeek":@YES}];
                    probeTransport(NO, 22, 3, 1.2);
                    probeApply(65, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    phase = 64; phaseBegan = now;
                } else if (phase == 64 && originalForeground.rate > 0 && seconds(originalForeground.currentTime) > 1.35) {
                    probeTransport(NO, 23, 4, .4);
                    probeApply(66, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    onMain(^{
                        pauseSeekPending = sceneForeground.seeking;
                        probeTransport(YES, 24, 4, .4);
                        probeApply(67, 110, silentVideo, nil, background, @"video", YES, audio, display, YES, .8, NO);
                    });
                    phase = 65; phaseBegan = now;
                } else if (phase == 65 && appliedSceneRevision == 67 && sceneForeground.completedSeekRevision == 4 &&
                           !sceneForeground.seeking && now - phaseBegan > .25) {
                    if (!pauseSeekPending || sceneForeground.player != originalForeground || originalForeground.rate != 0 ||
                        fabs(seconds(originalForeground.currentTime) - .4) > .06 || [transportEvents containsObject:@"110:23:playing"] ||
                        ![transportEvents containsObject:@"110:24:paused"]) {
                        fprintf(stderr, "Pause during an in-flight seek lost to the earlier playing intent\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"pauseDuringNativeSeekPreventsEarlierPlayingIntentFromResuming":@YES}];
                    probeTransport(NO, 25, 0, 0);
                    probeApplyFades(68, 0, nil, nil, nil, nil, NO, audio, display, NO, 0, 0, YES);
                    phase = 66; phaseBegan = now;
                } else if (phase == 66 && appliedSceneRevision == 68 && !sceneForeground && !sceneBackground) {
                    probeTransport(NO, 26, 0, 0);
                    probeApplyFades(69, 120, background, nil, nil, nil, NO, audio, display, YES, .8, 0, NO);
                    phase = 67; phaseBegan = now;
                } else if (phase == 67 && sceneForeground.started && sceneForeground.layer.readyForDisplay &&
                           seconds(sceneForeground.player.currentTime) > .15) {
                    if (sceneFadeTimer || sceneVisualTimer || sceneForeground.player.volume < .999 || sceneForeground.layer.opacity != 1) {
                        fprintf(stderr, "Independent fade settings delayed first playback from silence or black\n"); ss_quit(); return;
                    }
                    outgoingVideo = sceneForeground; overlap = NO;
                    probeTransport(NO, 27, 0, 0);
                    probeApplyFades(70, 130, background, nil, nil, nil, NO, audio, display, YES, .8, 0, NO);
                    phase = 68; phaseBegan = now;
                } else if (phase == 68 && sceneForeground.identifier == 130) {
                    if (sceneForeground.layer.readyForDisplay && sceneForeground.player.volume > .1 && sceneForeground.player.volume < .9) {
                        if (!sceneFadeTimer || sceneVisualTimer || sceneVisualOutgoing || sceneVisualCurrent != sceneForeground ||
                            sceneForeground.layer.opacity != 1 || sceneRetiring != outgoingVideo || outgoingVideo.player.volume <= .05 || outgoingVideo.disposed) {
                            fprintf(stderr, "Audio-only fade failed to cut video immediately while retaining its audio tail\n"); ss_quit(); return;
                        }
                        overlap = YES;
                    }
                    if (sceneForeground.layer.readyForDisplay && sceneForeground.player.volume > .999 && !sceneFadeTimer && !sceneRetiring) {
                        if (!overlap || !outgoingVideo.disposed) {
                            fprintf(stderr, "Audio-only crossfade was absent or retained its completed decoder\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"audioOnlyFadeCrossfadesGainAndCutsVideo":@YES}];
                        outgoingVideo = sceneForeground; before = seconds(outgoingVideo.player.currentTime);
                        visualOverlap = NO; movingOutgoing = NO;
                        probeTransport(NO, 28, 0, 0);
                        probeApplyFades(71, 140, first, imagePath, nil, nil, NO, audio, display, YES, 0, .8, NO);
                        phase = 69; phaseBegan = now;
                    }
                } else if (phase == 69) {
                    if (sceneForeground.started && sceneVisualOutgoing == outgoingVideo && sceneImage.layer.opacity > .05 && sceneImage.layer.opacity < .95) {
                        if (sceneFadeTimer || sceneRetiring || sceneForeground.player.volume < .999 || outgoingVideo.player.volume > .001 || outgoingVideo.disposed) {
                            fprintf(stderr, "Visual-only fade changed gain gradually or disposed its moving outgoing video\n"); ss_quit(); return;
                        }
                        visualOverlap = YES;
                        if (seconds(outgoingVideo.player.currentTime) > before + .03) movingOutgoing = YES;
                    }
                    if (sceneVisualCurrent == sceneImage && sceneImage.layer.contents && !sceneVisualTimer) {
                        if (!visualOverlap || !movingOutgoing || !outgoingVideo.disposed || sceneForeground.player.volume < .999) {
                            fprintf(stderr, "Visual-only crossfade missed its overlap, gain cut, or decoder cleanup\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"visualOnlyFadeCutsGainAndRetainsMovingVideoUntilOpacityCompletes":@YES}];
                        probeTransport(NO, 29, 0, 0);
                        probeApplyFades(72, 150, background, nil, nil, nil, NO, audio, display, YES, 0, 0, NO);
                        phase = 70; phaseBegan = now;
                    }
                } else if (phase == 70 && sceneForeground.identifier == 150 && sceneForeground.started &&
                           sceneForeground.layer.readyForDisplay && seconds(sceneForeground.player.currentTime) > .15) {
                    outgoingVideo = sceneForeground; before = seconds(outgoingVideo.player.currentTime);
                    overlap = NO; movingOutgoing = NO;
                    probeTransport(NO, 30, 0, 0);
                    probeApplyFades(73, 160, background, nil, nil, nil, NO, audio, display, YES, .25, .9, NO);
                    phase = 71; phaseBegan = now;
                } else if (phase == 71 && sceneForeground.identifier == 160) {
                    if (sceneForeground.started && sceneForeground.fadeTo == 1 && !sceneFadeTimer &&
                        sceneVisualTimer && sceneVisualOutgoing == outgoingVideo) {
                        if (sceneRetiring || outgoingVideo.disposed || !outgoingVideo.player || outgoingVideo.player.volume > .001 ||
                            outgoingVideo.layer.hidden || fabs(sceneFadeDuration - .25) > .001 || fabs(sceneVisualDuration - .9) > .001) {
                            fprintf(stderr, "Short audio fade released or audibly retained a longer visual tail\n"); ss_quit(); return;
                        }
                        overlap = YES;
                        if (seconds(outgoingVideo.player.currentTime) > before + .15) movingOutgoing = YES;
                    }
                    if (sceneForeground.layer.readyForDisplay && !sceneFadeTimer && !sceneVisualTimer && !sceneRetiring) {
                        if (!overlap || !movingOutgoing || !outgoingVideo.disposed) {
                            fprintf(stderr, "Audio-before-visual completion failed independent decoder ownership\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"shortAudioFadeRetainsSilentMovingVideoUntilLongVisualFadeCompletes":@YES}];
                        outgoingVideo = sceneForeground; before = seconds(outgoingVideo.player.currentTime);
                        visualOverlap = NO; movingOutgoing = NO; shortVisualFadeStarted = NO;
                        probeTransport(NO, 31, 0, 0);
                        probeApplyFades(74, 170, background, nil, nil, nil, NO, audio, display, YES, .9, .25, NO);
                        phase = 72; phaseBegan = now;
                    }
                } else if (phase == 72 && sceneForeground.identifier == 170) {
                    // Audio can start while the next video frame is still
                    // decoding. Observe the actual visual transition before
                    // treating its absent timer as a completed picture fade.
                    if (sceneForeground.layer.readyForDisplay && sceneVisualCurrent == sceneForeground &&
                        sceneVisualTimer && sceneVisualOutgoing == outgoingVideo) shortVisualFadeStarted = YES;
                    if (shortVisualFadeStarted && sceneForeground.layer.readyForDisplay && sceneVisualCurrent == sceneForeground &&
                        !sceneVisualTimer && sceneFadeTimer && sceneRetiring == outgoingVideo && outgoingVideo.player.volume > .05) {
                        if (sceneVisualOutgoing || !outgoingVideo.layer.hidden || outgoingVideo.disposed ||
                            sceneForeground.player.volume >= .999 || fabs(sceneFadeDuration - .9) > .001 || fabs(sceneVisualDuration - .25) > .001) {
                            fprintf(stderr, "Short visual fade disposed or revealed a longer audio tail: ready=%d selected=%d observed=%d outgoing=%d hidden=%d disposed=%d incomingGain=%.4f outgoingGain=%.4f audioDuration=%.4f visualDuration=%.4f\n",
                                (int)sceneForeground.layer.readyForDisplay, (int)(sceneVisualCurrent == sceneForeground), (int)shortVisualFadeStarted,
                                (int)(sceneVisualOutgoing != nil), (int)outgoingVideo.layer.hidden, (int)outgoingVideo.disposed,
                                sceneForeground.player.volume, outgoingVideo.player.volume, sceneFadeDuration, sceneVisualDuration);
                            ss_quit(); return;
                        }
                        visualOverlap = YES;
                        if (seconds(outgoingVideo.player.currentTime) > before + .15) movingOutgoing = YES;
                    }
                    if (sceneForeground.layer.readyForDisplay && !sceneFadeTimer && !sceneVisualTimer && !sceneRetiring) {
                        if (!shortVisualFadeStarted || !visualOverlap || !movingOutgoing || !outgoingVideo.disposed) {
                            fprintf(stderr, "Visual-before-audio completion failed independent decoder ownership: ready=%d selected=%d observed=%d overlap=%d moving=%d hidden=%d disposed=%d incomingGain=%.4f outgoingGain=%.4f audioDuration=%.4f visualDuration=%.4f\n",
                                (int)sceneForeground.layer.readyForDisplay, (int)(sceneVisualCurrent == sceneForeground), (int)shortVisualFadeStarted,
                                (int)visualOverlap, (int)movingOutgoing, (int)outgoingVideo.layer.hidden, (int)outgoingVideo.disposed,
                                sceneForeground.player.volume, outgoingVideo.player.volume, sceneFadeDuration, sceneVisualDuration);
                            ss_quit(); return;
                        }
                        [observations addObject:@{@"shortVisualFadeRetainsHiddenAudioDecoderUntilLongAudioFadeCompletes":@YES}];
                        outgoingVideo = sceneForeground; overlap = NO;
                        probeTransport(NO, 32, 0, 0);
                        probeApplyFades(75, 0, nil, nil, background, @"video", YES, audio, display, YES, .65, 0, NO);
                        phase = 73; phaseBegan = now;
                    }
                } else if (phase == 73) {
                    if (sceneBackground.started && sceneBackground.layer.readyForDisplay && sceneBackground.player.volume > .05 && sceneBackground.player.volume < .95) {
                        if (!sceneFadeTimer || sceneVisualTimer || sceneVisualOutgoing || sceneVisualCurrent != sceneBackground ||
                            sceneBackground.layer.opacity != 1 || sceneRetiring != outgoingVideo || outgoingVideo.player.volume <= .05) {
                            fprintf(stderr, "Background video soundtrack did not use the independent audio fade\n"); ss_quit(); return;
                        }
                        overlap = YES;
                    }
                    if (sceneBackground.started && sceneBackground.player.volume > .999 && !sceneFadeTimer && !sceneRetiring) {
                        if (!overlap || !outgoingVideo.disposed) {
                            fprintf(stderr, "Background soundtrack crossfade did not complete or release the old decoder\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"backgroundSoundtrackUsesAudioFadeWithVisualFadeDisabled":@YES}];
                        originalBackground = sceneBackground; visualOverlap = NO;
                        probeTransport(NO, 33, 0, 0);
                        probeApplyFades(76, 180, first, imagePath, background, @"video", YES, audio, display, YES, 0, .65, NO);
                        phase = 74; phaseBegan = now;
                    }
                } else if (phase == 74) {
                    if (sceneForeground.started && sceneVisualOutgoing == originalBackground && sceneImage.layer.opacity > .05 && sceneImage.layer.opacity < .95) {
                        if (sceneFadeTimer || sceneForeground.player.volume < .999 || sceneBackground.player.volume > .001) {
                            fprintf(stderr, "Independent visual fade delayed the background soundtrack gain cut\n"); ss_quit(); return;
                        }
                        visualOverlap = YES;
                    }
                    if (sceneForeground.started && sceneVisualCurrent == sceneImage && !sceneVisualTimer) {
                        if (!visualOverlap || sceneBackground != originalBackground || originalBackground.disposed || originalBackground.player.rate <= 0) {
                            fprintf(stderr, "Visual-only image transition replaced or stopped its independent background\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"backgroundSoundtrackCutsImmediatelyWhileVisualFadeContinues":@YES}];
                        passed = YES;
                        dispatch_source_cancel(timer);
                        NSData *jsonData = [NSJSONSerialization dataWithJSONObject:@{@"status":@"passed",@"observations":observations,
                            @"physicalSpeakerOutputVerified":@NO,@"physicalPointerVisibilityVerified":@NO,
                            @"method":@"Real AVPlayer timelines/volume overlap and live video/image CALayer crossfade opacity observed inside a test process compiling the production bridge"} options:0 error:NULL];
                        printf("%s\n", [[NSString alloc] initWithData:jsonData encoding:NSUTF8StringEncoding].UTF8String);
                        ss_quit();
                    }
                }
            }
        });
        dispatch_resume(timer);
        probeApply(1, 0, nil, nil, background, @"video", YES, audio, display, YES, .4, NO);
        ss_run(); dispatch_source_cancel(timer);
        return passed ? 0 : 4;
    }
}
