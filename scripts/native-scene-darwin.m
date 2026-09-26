// Test-only own-process observation. The production bridge is compiled into this
// translation unit so assertions can inspect actual AVPlayer volumes/layers;
// the shipped application contains no test endpoint or diagnostic hooks.
#import "../internal/platform/bridge_darwin.m"

static void probeApply(uint64_t revision, uint64_t foregroundID, NSString *foreground,
    NSString *image, NSString *background, NSString *backgroundKind, BOOL backgroundAudio,
    NSString *audio, NSString *display, BOOL stage, double fade, BOOL hard) {
    ss_scene_request request = {0};
    request.revision = revision; request.generation = foregroundID ?: revision;
    request.foreground_id = foregroundID; request.foreground_path = foreground.UTF8String ?: "";
    request.foreground_kind = foreground.length ? ([foreground.pathExtension.lowercaseString isEqual:@"mp4"] ? "video" : "audio") : ""; request.foreground_has_audio = foreground.length != 0;
    request.image_path = image.UTF8String ?: ""; request.background_path = background.UTF8String ?: "";
    request.background_kind = backgroundKind.UTF8String ?: ""; request.background_audio = backgroundAudio;
    request.audio = audio.UTF8String; request.display = display.UTF8String;
    request.stage_enabled = stage; request.fade_seconds = fade; request.hard_stop = hard;
    ss_scene(&request);
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
        char *failure = ss_init();
        if (failure) { fprintf(stderr, "%s\n", failure); ss_free(failure); return 3; }
        __block NSUInteger phase = 0;
        __block BOOL passed = NO, overlap = NO, stopOverlap = NO, sawLoop = NO, silenceFade = NO;
        __block BOOL sawFirstPlaying = NO, sawSecondPlaying = NO, sawEnded = NO;
        __block double began = NSProcessInfo.processInfo.systemUptime, phaseBegan = began, before = 0, lastBackground = 0;
        __block AVPlayer *originalForeground;
        __block SSScenePlayer *originalBackground, *outgoingVideo;
        __block SSSceneImage *outgoingImage;
        __block BOOL visualOverlap = NO, movingOutgoing = NO;
        __block NSMutableArray *observations = [NSMutableArray array];
        dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
        dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 20 * NSEC_PER_MSEC), 20 * NSEC_PER_MSEC, NSEC_PER_MSEC);
        dispatch_source_set_event_handler(timer, ^{
            @autoreleasepool {
                // ss_quit stops AppKit asynchronously. A pending timer delivery
                // must not repeat the final report or advance after a failure.
                if (passed || atomic_load(&shuttingDown)) return;
                double now = NSProcessInfo.processInfo.systemUptime;
                if (now - began > 50) { fprintf(stderr, "Scene probe timed out at phase %lu\n", (unsigned long)phase); ss_quit(); return; }
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
                    passed = YES;
                    dispatch_source_cancel(timer);
                    NSData *jsonData = [NSJSONSerialization dataWithJSONObject:@{@"status":@"passed",@"observations":observations,
                        @"physicalSpeakerOutputVerified":@NO,@"physicalPointerVisibilityVerified":@NO,
                        @"method":@"Real AVPlayer timelines/volume overlap and live video/image CALayer crossfade opacity observed inside a test process compiling the production bridge"} options:0 error:NULL];
                    printf("%s\n", [[NSString alloc] initWithData:jsonData encoding:NSUTF8StringEncoding].UTF8String);
                    ss_quit();
                }
            }
        });
        dispatch_resume(timer);
        probeApply(1, 0, nil, nil, background, @"video", YES, audio, display, YES, .4, NO);
        ss_run(); dispatch_source_cancel(timer);
        return passed ? 0 : 4;
    }
}
