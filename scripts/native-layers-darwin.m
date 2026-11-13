// Own-process observation of production AVPlayer sources. No test hooks are
// compiled into the shipped app, and no physical audio measurement is claimed.
#import "../internal/platform/bridge_darwin.m"
#include <assert.h>
#include <string.h>

static NSString *probeAudio, *probeDisplay, *probeFirst, *probeSecond, *probeVideo;
static uint64_t probeRevision;
static NSMutableDictionary *probeForeground;
static NSMutableArray<NSMutableDictionary *> *probeLayers;
static BOOL probeBackgroundSound, probeStage = YES;
static double probeMaster = 1, probeFade = .35;

static NSMutableDictionary *voice(uint64_t identifier, NSString *path, uint64_t transport) {
    return [@{@"id": @(identifier), @"path": path, @"transport": @(transport),
        @"paused": @NO, @"seek": @0, @"position": @0, @"volume": @1, @"muted": @NO} mutableCopy];
}
static void apply(BOOL hard) {
    ss_scene_request request = {0};
    request.revision = ++probeRevision; request.generation = 1000 + probeRevision;
    request.master_volume = probeMaster;
    request.foreground_volume = [probeForeground[@"volume"] doubleValue];
    request.background_volume = 1;
    request.foreground_id = [probeForeground[@"id"] unsignedLongLongValue];
    request.foreground_path = [probeForeground[@"path"] UTF8String] ?: "";
    request.foreground_kind = probeForeground ? ([probeForeground[@"path"] isEqual:probeVideo] ? "video" : "audio") : "";
    request.foreground_has_audio = probeForeground != nil;
    request.foreground_muted = [probeForeground[@"muted"] boolValue];
    request.foreground_paused = [probeForeground[@"paused"] boolValue];
    request.transport_revision = [probeForeground[@"transport"] unsignedLongLongValue];
    request.seek_revision = [probeForeground[@"seek"] unsignedLongLongValue];
    request.seek_seconds = [probeForeground[@"position"] doubleValue];
    request.background_path = probeVideo.UTF8String;
    request.background_kind = "video";
    request.background_audio = probeBackgroundSound;
    request.audio = probeAudio.UTF8String; request.display = probeDisplay.UTF8String;
    request.stage_enabled = probeStage; request.hard_stop = hard;
    request.audio_fade_seconds = probeFade; request.visual_fade_seconds = .1;
    ss_audio_layer layers[16] = {0};
    assert(probeLayers.count <= 16);
    for (NSUInteger i = 0; i < probeLayers.count; ++i) {
        NSDictionary *entry = probeLayers[i];
        layers[i].id = [entry[@"id"] unsignedLongLongValue];
        layers[i].path = [entry[@"path"] UTF8String]; layers[i].audio = probeAudio.UTF8String;
        layers[i].paused = [entry[@"paused"] boolValue];
        layers[i].transport_revision = [entry[@"transport"] unsignedLongLongValue];
        layers[i].seek_revision = [entry[@"seek"] unsignedLongLongValue];
        layers[i].seek_seconds = [entry[@"position"] doubleValue];
        layers[i].volume = [entry[@"volume"] doubleValue];
    }
    request.audio_layers = layers; request.audio_layer_count = (uint32_t)probeLayers.count;
    ss_scene(&request);
    // The bridge must own a copy before returning, including the stack array.
    memset(layers, 0, sizeof layers);
}
static SSScenePlayer *layer(uint64_t identifier) {
    for (SSScenePlayer *source in sceneAudioLayers) if (source.identifier == identifier) return source;
    return nil;
}
static BOOL playing(SSScenePlayer *source) {
    return source && source.started && source.player.rate > 0 && !source.player.muted && source.player.volume > .99;
}
static BOOL quietBackground(void) {
    return !sceneBackground || sceneBackground.player.volume < .001 || sceneBackground.player.muted;
}
static void describeResume(const char *reason, SSScenePlayer *first, AVPlayer *firstPlayer,
    SSScenePlayer *second, AVPlayer *secondPlayer, double before, double elapsed, BOOL acknowledged) {
    fprintf(stderr, "%s: firstSourceSame=%d firstPlayerSame=%d secondSourceSame=%d secondPlayerSame=%d "
        "firstPlaying=%d secondPlaying=%d acknowledged=%d elapsed=%.6f before=%.6f current=%.6f delta=%.6f "
        "firstRate=%.6f firstTimeControl=%ld firstMuted=%d firstVolume=%.6f secondRate=%.6f secondTimeControl=%ld secondMuted=%d secondVolume=%.6f\n",
        reason, layer(30) == first, first.player == firstPlayer, layer(31) == second, second.player == secondPlayer,
        playing(first), playing(second), acknowledged, elapsed, before, seconds(firstPlayer.currentTime), seconds(firstPlayer.currentTime) - before,
        firstPlayer.rate, (long)firstPlayer.timeControlStatus, firstPlayer.muted, firstPlayer.volume,
        secondPlayer.rate, (long)secondPlayer.timeControlStatus, secondPlayer.muted, secondPlayer.volume);
}

int main(void) {
    @autoreleasepool {
        NSString *(^environment)(const char *) = ^NSString *(const char *name) {
            return [NSString stringWithUTF8String:getenv(name) ?: ""];
        };
        probeAudio = environment("SMARTSTAGE_SCENE_PROBE_AUDIO");
        probeDisplay = environment("SMARTSTAGE_SCENE_PROBE_DISPLAY");
        probeFirst = environment("SMARTSTAGE_SCENE_PROBE_FIRST");
        probeSecond = environment("SMARTSTAGE_SCENE_PROBE_SECOND");
        probeVideo = environment("SMARTSTAGE_SCENE_PROBE_BACKGROUND");
        if (!probeAudio.length || !probeDisplay.length || !probeFirst.length || !probeSecond.length || !probeVideo.length) return 2;
        char *failure = ss_init();
        if (failure) { fprintf(stderr, "%s\n", failure); ss_free(failure); return 3; }
        probeLayers = [NSMutableArray array];
        probeForeground = voice(10, probeVideo, 1);
        __block NSUInteger phase = 0;
        __block BOOL passed = NO, replacementOverlap = NO, stopFade = NO, stopSeekPending = NO, mixedDuringFade = NO, zeroFadePreparationHeld = NO;
        __block double preservedFadeStart = 0, preservedFadeDuration = 0;
        __block uint64_t hardStopRevision = 0;
        __block double began = NSProcessInfo.processInfo.systemUptime, phaseBegan = began;
        __block double videoBefore = 0, firstBefore = 0, secondBefore = 0;
        __block SSScenePlayer *video, *first, *second, *retiring;
        __block AVPlayer *videoPlayer, *firstPlayer, *secondPlayer;
        __block AVPlayerItem *videoItem;
        NSMutableArray *observations = [NSMutableArray array];
        NSMutableDictionary *resumeClockObservation = [NSMutableDictionary dictionary];
        NSMutableSet<NSString *> *transportEvents = [NSMutableSet set];
        NSMutableDictionary<NSString *, NSString *> *failureMessages = [NSMutableDictionary dictionary];
        dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
        dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 20 * NSEC_PER_MSEC), 20 * NSEC_PER_MSEC, NSEC_PER_MSEC);
        dispatch_source_set_event_handler(timer, ^{
            @autoreleasepool {
                if (passed || atomic_load(&shuttingDown)) return;
                double now = NSProcessInfo.processInfo.systemUptime;
                if (now - began > 55) {
                    fprintf(stderr, "Native layer probe timed out: phase=%lu active=%lu retiring=%lu foreground=%llu\n",
                        (unsigned long)phase, (unsigned long)sceneAudioLayers.count, (unsigned long)sceneRetiringAudioLayers.count,
                        (unsigned long long)sceneForeground.identifier);
                    if (phase == 5) describeResume("Layer resume timed out", first, firstPlayer, second, secondPlayer,
                        firstBefore, now - phaseBegan, [transportEvents containsObject:@"30:53:playing"]);
                    ss_quit(); return;
                }
                char *raw;
                while ((raw = ss_poll())) {
                    NSDictionary *event = [NSJSONSerialization JSONObjectWithData:[[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding] options:0 error:NULL];
                    ss_free(raw);
                    uint64_t identifier = [event[@"generation"] unsignedLongLongValue];
                    NSString *kind = event[@"kind"];
                    [transportEvents addObject:[NSString stringWithFormat:@"%@:%@:%@", event[@"generation"], event[@"transportRevision"] ?: @0, kind]];
                    if ([kind isEqual:@"error"]) failureMessages[[NSString stringWithFormat:@"%@:%@", event[@"generation"], event[@"transportRevision"]]] = event[@"message"] ?: @"";
                    if (([kind isEqual:@"error"] && identifier != 53 && identifier != 54) || [kind isEqual:@"background-error"] || [kind isEqual:@"device-lost"] ||
                        (hardStopRevision && [event[@"sceneRevision"] unsignedLongLongValue] >= hardStopRevision && ([kind isEqual:@"playing"] || [kind isEqual:@"paused"] || [kind isEqual:@"progress"] || [kind isEqual:@"ended"]))) {
                        fprintf(stderr, "Unexpected layer event: %s\n", event.description.UTF8String); ss_quit(); return;
                    }
                }
                if (phase == 0 && playing(sceneForeground) && sceneForeground.layer.readyForDisplay && !sceneVisualTimer && seconds(sceneForeground.player.currentTime) > .1) {
                    video = sceneForeground; videoPlayer = video.player; videoItem = video.item; videoBefore = seconds(videoPlayer.currentTime);
                    [probeLayers addObject:voice(20, probeFirst, 41)]; apply(NO);
                    phase = 1; phaseBegan = now;
                } else if (phase == 1 && playing(layer(20)) && !sceneFadeTimer) {
                    if (sceneForeground != video || video.player != videoPlayer || video.item != videoItem || !playing(video) ||
                        seconds(videoPlayer.currentTime) <= videoBefore || sceneVisualCurrent != video || sceneVisualTimer || !quietBackground()) {
                        fprintf(stderr, "MixAudio restarted video or replaced its soundtrack\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"mixAudioRetainsVideoPlayerFrameTimelineAndSoundtrack": @YES}];
                    retiring = layer(20); videoBefore = seconds(videoPlayer.currentTime);
                    probeForeground[@"muted"] = @YES;
                    [probeLayers removeAllObjects]; [probeLayers addObject:voice(30, probeSecond, 51)]; apply(NO);
                    phase = 2; phaseBegan = now;
                } else if (phase == 2) {
                    if (layer(30).player.volume > .05 && layer(30).player.volume < .95 && retiring.player.volume > .05) {
                        replacementOverlap = YES;
                        if (!mixedDuringFade) {
                            mixedDuringFade = YES;
                            preservedFadeStart = layer(30).audioFadeStarted;
                            preservedFadeDuration = layer(30).audioFadeDuration;
                            [probeLayers addObject:voice(32, probeFirst, 62)]; apply(NO);
                        }
                    }
                    if (playing(layer(30)) && !sceneFadeTimer && !sceneRetiringAudioLayers.count) {
                        if (!replacementOverlap || !mixedDuringFade || layer(30).audioFadeStarted != preservedFadeStart ||
                            layer(30).audioFadeDuration != preservedFadeDuration || sceneForeground != video || video.player != videoPlayer || video.item != videoItem ||
                            seconds(videoPlayer.currentTime) <= videoBefore || video.player.volume > .001 || sceneVisualCurrent != video ||
                            video.layer.hidden || !retiring.disposed || retiring.player || !quietBackground()) {
                            fprintf(stderr, "KeepVideo failed identity/playhead, replacement fade, or soundtrack suppression\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"keepVideoRetainsExactPlayerFrameAndPlayheadWhileReplacingSound": @YES,
                            @"audioLayerReplacementCrossfadesAndReleasesRetiredDecoder": @YES,
                            @"mixAdditionPreservesExistingAudioFadeEnvelopes": @YES}];
                        // Hold the short fixture video's existing frame while the
                        // remaining independent audio transport checks execute.
                        probeForeground[@"paused"] = @YES; probeForeground[@"transport"] = @2;
                        [probeLayers removeObjectAtIndex:1];
                        [probeLayers addObject:voice(31, probeFirst, 61)]; apply(NO);
                        phase = 3; phaseBegan = now;
                    }
                } else if (phase == 3 && playing(layer(30)) && playing(layer(31)) && !sceneFadeTimer && video.player.rate == 0) {
                    first = layer(30); second = layer(31); firstPlayer = first.player; secondPlayer = second.player;
                    firstBefore = seconds(firstPlayer.currentTime); secondBefore = seconds(secondPlayer.currentTime);
                    probeLayers[0][@"paused"] = @YES; probeLayers[0][@"transport"] = @52; apply(NO);
                    phase = 4; phaseBegan = now;
                } else if (phase == 4 && now - phaseBegan > .2 && [transportEvents containsObject:@"30:52:paused"]) {
                    if (first.player != firstPlayer || second.player != secondPlayer || firstPlayer.rate != 0 || !firstPlayer.muted ||
                        fabs(seconds(firstPlayer.currentTime) - firstBefore) > .09 || !playing(second) ||
                        seconds(secondPlayer.currentTime) < secondBefore + .08 || video.player != videoPlayer || video.player.volume > .001 || !quietBackground()) {
                        fprintf(stderr, "Pausing one layer affected another source or resumed background audio\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"audioLayerPauseFreezesOnlyItsOwnDecoderAndClock": @YES,
                        @"pausedReplacementCannotRestoreVideoOrBackgroundSound": @YES}];
                    probeLayers[0][@"paused"] = @NO; probeLayers[0][@"transport"] = @53; apply(NO);
                    phase = 5; phaseBegan = now;
                } else if (phase == 5) {
                    BOOL acknowledged = [transportEvents containsObject:@"30:53:playing"];
                    // Identities and the unrelated decoder must remain intact
                    // throughout the resume, including while AVPlayer buffers.
                    if (layer(30) != first || first.player != firstPlayer || layer(31) != second || second.player != secondPlayer || !playing(second)) {
                        describeResume("Layer resume replaced or interrupted a decoder", first, firstPlayer, second, secondPlayer,
                            firstBefore, now - phaseBegan, acknowledged);
                        ss_quit(); return;
                    }
                    if (!playing(first) || now - phaseBegan <= .15 || !acknowledged) return;
                    double advancement = seconds(firstPlayer.currentTime) - firstBefore;
                    if (!resumeClockObservation.count) {
                        resumeClockObservation[@"firstEligibleClockAdvanceSeconds"] = @(advancement);
                        resumeClockObservation[@"firstEligibleElapsedSeconds"] = @(now - phaseBegan);
                        resumeClockObservation[@"waitedForClock"] = @(advancement <= .07);
                        if (advancement <= .07) describeResume("Layer resume awaiting actual clock advance", first, firstPlayer, second, secondPlayer,
                            firstBefore, now - phaseBegan, acknowledged);
                    }
                    // A playing acknowledgment reports AVPlayer readiness, not
                    // .07 seconds of elapsed media. Keep the exact motion
                    // requirement and the probe's existing 55-second deadline.
                    if (advancement <= .07) return;
                    resumeClockObservation[@"completedClockAdvanceSeconds"] = @(advancement);
                    resumeClockObservation[@"completedElapsedSeconds"] = @(now - phaseBegan);
                    resumeClockObservation[@"requiredClockAdvanceSeconds"] = @0.07;
                    [observations addObject:@{@"audioLayerResumeRetainsPlayerAndUsesItsOwnTransportRevision": @YES}];
                    probeMaster = .4; probeLayers[0][@"volume"] = @0.5; probeLayers[1][@"volume"] = @0.75; apply(NO);
                    phase = 6; phaseBegan = now;
                } else if (phase == 6 && now - phaseBegan > .1) {
                    if (first.player != firstPlayer || second.player != secondPlayer || fabs(firstPlayer.volume - .2) > .001 ||
                        fabs(secondPlayer.volume - .3) > .001 || first.fadeGain != 1 || second.fadeGain != 1 || sceneFadeTimer || !quietBackground()) {
                        fprintf(stderr, "Layer master/track volumes restarted or mis-scaled playback\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"masterAndTrackVolumeScaleEveryAudioLayerWithoutRestart": @YES}];
                    firstBefore = seconds(firstPlayer.currentTime); secondBefore = seconds(secondPlayer.currentTime);
                    probeStage = NO; apply(NO); phase = 17; phaseBegan = now;
                } else if (phase == 17 && now - phaseBegan > .15) {
                    if (stageEnabled || stageWindow.isVisible || first.player != firstPlayer || second.player != secondPlayer ||
                        firstPlayer.rate <= 0 || secondPlayer.rate <= 0 || seconds(firstPlayer.currentTime) < firstBefore + .05 ||
                        seconds(secondPlayer.currentTime) < secondBefore + .05 || !quietBackground()) {
                        fprintf(stderr, "Stage-off interrupted an audio layer\n"); ss_quit(); return;
                    }
                    probeStage = YES; apply(NO); phase = 18; phaseBegan = now;
                } else if (phase == 18 && stageEnabled && stageWindow.isVisible && now - phaseBegan > .1) {
                    if (first.player != firstPlayer || second.player != secondPlayer || video.player != videoPlayer ||
                        video.player.rate != 0 || video.player.volume > .001 || fabs(firstPlayer.volume - .2) > .001 ||
                        fabs(secondPlayer.volume - .3) > .001 || !quietBackground()) {
                        fprintf(stderr, "Stage-on changed layer transport, gain or preserved video frame\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"stageChangesPreserveAllAudioLayersAndKeptVideo": @YES}];
                    probeMaster = 1; probeLayers[0][@"volume"] = @1; probeLayers[1][@"volume"] = @1;
                    probeLayers[0][@"transport"] = @54; probeLayers[0][@"seek"] = @1; probeLayers[0][@"position"] = @19.7;
                    apply(NO); phase = 7; phaseBegan = now;
                } else if (phase == 7 && [transportEvents containsObject:@"30:54:ended"]) {
                    if (layer(30) || !first.disposed || first.player || layer(31) != second || !playing(second) || video.player != videoPlayer || !quietBackground()) {
                        fprintf(stderr, "Layer natural end affected another decoder or restored suppressed sound\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"audioLayerEndReportsOwnIdentityWithoutStoppingOtherSources": @YES}];
                    apply(NO); phase = 8; phaseBegan = now;
                } else if (phase == 8 && now - phaseBegan > .15) {
                    if (layer(30) || sceneAudioLayers.count != 1 || layer(31) != second || !playing(second) || !quietBackground()) {
                        fprintf(stderr, "An ended audio layer was resurrected by a later scene\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"lateSceneCannotResurrectEndedAudioLayer": @YES}];
                    [probeLayers removeAllObjects]; probeForeground = nil; probeFade = .35; apply(NO);
                    phase = 9; phaseBegan = now;
                } else if (phase == 9) {
                    if (second.player.volume > .05 && second.player.volume < .95) stopFade = YES;
                    if (!sceneAudioLayers.count && !sceneRetiringAudioLayers.count && !sceneFadeTimer && now - phaseBegan > .1) {
                        if (!stopFade || !second.disposed || second.player || sceneForeground || !quietBackground()) {
                            fprintf(stderr, "STOP did not fade and release all audio layers\n"); ss_quit(); return;
                        }
                        [observations addObject:@{@"stopFadesAndReleasesAllAudioLayers": @YES}];
                        probeFade = .15; probeForeground = voice(50, probeFirst, 70);
                        [probeLayers addObject:voice(51, probeSecond, 71)]; [probeLayers addObject:voice(52, probeFirst, 72)]; apply(NO);
                        phase = 10; phaseBegan = now;
                    }
                } else if (phase == 10 && playing(sceneForeground) && playing(layer(51)) && playing(layer(52)) && !sceneFadeTimer) {
                    first = layer(51); second = layer(52); firstPlayer = first.player; secondPlayer = second.player;
                    firstBefore = seconds(firstPlayer.currentTime); secondBefore = seconds(secondPlayer.currentTime);
                    probeForeground[@"paused"] = @YES; probeForeground[@"transport"] = @73; apply(NO);
                    phase = 11; phaseBegan = now;
                } else if (phase == 11 && now - phaseBegan > .2 && [transportEvents containsObject:@"50:73:paused"]) {
                    if (!playing(first) || !playing(second) || seconds(firstPlayer.currentTime) < firstBefore + .08 ||
                        seconds(secondPlayer.currentTime) < secondBefore + .08 || sceneForeground.player.rate != 0 || !quietBackground()) {
                        fprintf(stderr, "Pausing original audio silenced its independent mixed layers\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"mixAudioRetainsOriginalAudioAndIndependentPauseControls": @YES}];
                    [probeLayers addObject:voice(53, [probeFirst stringByAppendingString:@".missing.wav"], 74)]; apply(NO);
                    phase = 12; phaseBegan = now;
                } else if (phase == 12 && [transportEvents containsObject:@"53:74:error"]) {
                    if (layer(53) || layer(51) != first || layer(52) != second || !playing(first) || !playing(second) || !quietBackground()) {
                        fprintf(stderr, "Layer error stopped or unmuted an unrelated source\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"audioLayerErrorReportsOwnIdentityAndPreservesOtherSources": @YES}];
                    // Repair the real file after the native error, before a newer
                    // transport arrives. Reopening the failed identity would now
                    // succeed, so this proves terminal caching rather than a
                    // second decoder failure that happens to produce the error.
                    if (![NSFileManager.defaultManager copyItemAtPath:probeFirst toPath:probeLayers[2][@"path"] error:NULL]) {
                        fprintf(stderr, "Could not repair failed layer fixture\n"); ss_quit(); return;
                    }
                    probeLayers[2][@"paused"] = @YES; probeLayers[2][@"transport"] = @75; apply(NO);
                    phase = 19; phaseBegan = now;
                } else if (phase == 19 && [transportEvents containsObject:@"53:75:error"]) {
                    if (layer(53) || layer(51) != first || layer(52) != second || !playing(first) || !playing(second) ||
                        ![failureMessages[@"53:74"] isEqual:failureMessages[@"53:75"]]) {
                        fprintf(stderr, "Failed layer restarted or lost its terminal error after a new transport\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"failedAudioLayerReacknowledgesLatestTransportWithoutRestart": @YES}];
                    [probeLayers removeObjectAtIndex:2];
                    probeForeground = voice(54, [probeSecond stringByAppendingString:@".missing.wav"], 76); apply(NO);
                    phase = 20; phaseBegan = now;
                } else if (phase == 20 && [transportEvents containsObject:@"54:76:error"]) {
                    if (sceneForeground || !playing(first) || !playing(second) ||
                        ![NSFileManager.defaultManager copyItemAtPath:probeSecond toPath:probeForeground[@"path"] error:NULL]) {
                        fprintf(stderr, "Foreground failure disrupted a mixed source or fixture repair failed\n"); ss_quit(); return;
                    }
                    probeForeground[@"paused"] = @YES; probeForeground[@"transport"] = @77;
                    probeForeground[@"seek"] = @1; probeForeground[@"position"] = @0.5; apply(NO);
                    phase = 21; phaseBegan = now;
                } else if (phase == 21 && [transportEvents containsObject:@"54:77:error"]) {
                    if (sceneForeground || sceneFailedForegroundID != 54 || layer(51) != first || layer(52) != second ||
                        !playing(first) || !playing(second) || ![failureMessages[@"54:76"] isEqual:failureMessages[@"54:77"]]) {
                        fprintf(stderr, "Failed foreground restarted or lost the latest transport error\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"failedForegroundReacknowledgesLatestTransportWithoutRestart": @YES}];
                    probeForeground = voice(55, probeForeground[@"path"], 78); apply(NO);
                    phase = 25; phaseBegan = now;
                } else if (phase == 25 && playing(sceneForeground) && sceneForeground.identifier == 55 && !sceneFadeTimer) {
                    if (sceneFailedForegroundID || !playing(first) || !playing(second) || ![transportEvents containsObject:@"55:78:playing"]) {
                        fprintf(stderr, "A new foreground identity could not retry the repaired file\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"newForegroundIdentityCanRetryPreviouslyFailedPath": @YES}];
                    probeForeground = nil; [probeLayers removeAllObjects]; probeBackgroundSound = YES;
                    [probeLayers addObject:voice(70, probeFirst, 81)]; [probeLayers addObject:voice(71, probeSecond, 82)]; apply(NO);
                    phase = 13; phaseBegan = now;
                } else if (phase == 13 && playing(sceneBackground) && playing(layer(70)) && playing(layer(71)) && !sceneFadeTimer) {
                    first = layer(70); second = layer(71); firstPlayer = first.player; secondPlayer = second.player;
                    firstBefore = seconds(firstPlayer.currentTime); secondBefore = seconds(secondPlayer.currentTime);
                    probeLayers[0][@"paused"] = @YES; probeLayers[0][@"transport"] = @83; apply(NO);
                    phase = 14; phaseBegan = now;
                } else if (phase == 14 && now - phaseBegan > .2 && [transportEvents containsObject:@"70:83:paused"]) {
                    if (!playing(sceneBackground) || !playing(second) || firstPlayer.rate != 0 || !firstPlayer.muted ||
                        seconds(secondPlayer.currentTime) < secondBefore + .08) {
                        fprintf(stderr, "MixAudio did not preserve eligible background soundtrack during independent pause\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"mixAudioPreservesEligibleBackgroundSoundtrack": @YES}];
                    probeLayers[1][@"transport"] = @84; probeLayers[1][@"seek"] = @1; probeLayers[1][@"position"] = @8;
                    apply(NO);
                    onMain(^{
                        stopSeekPending = layer(71).seeking;
                        probeStage = NO; probeForeground = nil; [probeLayers removeAllObjects]; apply(YES);
                        hardStopRevision = probeRevision;
                    });
                    phase = 16; phaseBegan = now;
                } else if (phase == 16 && now - phaseBegan > .25) {
                    if (!stopSeekPending || sceneForeground || sceneBackground || sceneRetiring || sceneAudioLayers.count || sceneRetiringAudioLayers.count ||
                        sceneVisualCurrent || sceneVisualOutgoing || sceneFadeTimer || sceneVisualTimer || stageEnabled || stageWindow.isVisible ||
                        !first.disposed || !second.disposed || first.player || second.player) {
                        fprintf(stderr, "Hard STOP left a native audio layer or late seek alive\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"hardStopDuringLayerSeekDisposesAllSourcesAndRejectsLateEvents": @YES}];
                    hardStopRevision = 0; probeBackgroundSound = NO; probeFade = 0;
                    probeForeground = voice(90, probeFirst, 90); apply(NO);
                    phase = 22; phaseBegan = now;
                } else if (phase == 22 && playing(sceneForeground)) {
                    retiring = sceneForeground;
                    probeForeground = nil; [probeLayers addObject:voice(91, probeSecond, 91)]; apply(NO);
                    onMain(^{
                        zeroFadePreparationHeld = layer(91) && !layer(91).started && sceneRetiring == retiring &&
                            !retiring.disposed && retiring.player.currentItem && retiring.player.rate > 0 &&
                            !retiring.player.muted && retiring.player.volume > .99;
                    });
                    phase = 23; phaseBegan = now;
                } else if (phase == 23 && playing(layer(91)) && !sceneRetiring) {
                    if (!zeroFadePreparationHeld || !retiring.disposed || retiring.player || sceneFadeTimer) {
                        fprintf(stderr, "Zero-fade KeepVideo replacement cut its outgoing primary before layer readiness\n"); ss_quit(); return;
                    }
                    [observations addObject:@{@"zeroFadeReplacementWaitsForNativeAudioPreparation": @YES}];
                    [probeLayers removeAllObjects]; apply(YES); hardStopRevision = probeRevision;
                    phase = 24; phaseBegan = now;
                } else if (phase == 24 && now - phaseBegan > .15) {
                    if (scenePlayers().count || sceneForeground || sceneBackground || sceneRetiring || sceneAudioLayers.count ||
                        sceneRetiringAudioLayers.count || sceneFadeTimer || sceneVisualTimer || stageEnabled || stageWindow.isVisible) {
                        fprintf(stderr, "Final layer probe STOP did not dispose all native sources\n"); ss_quit(); return;
                    }
                    passed = YES; dispatch_source_cancel(timer);
                    NSData *report = [NSJSONSerialization dataWithJSONObject:@{@"status": @"passed", @"observations": observations,
                        @"resumeClockObservation": resumeClockObservation,
                        @"method": @"Production Cocoa bridge; actual AVPlayer identity, native clocks, gain, state and event observations. No physical audio measurement."} options:0 error:NULL];
                    puts([[NSString alloc] initWithData:report encoding:NSUTF8StringEncoding].UTF8String); fflush(stdout); ss_quit();
                }
            }
        });
        dispatch_resume(timer); apply(NO); ss_run(); dispatch_source_cancel(timer);
        return passed ? 0 : 1;
    }
}
