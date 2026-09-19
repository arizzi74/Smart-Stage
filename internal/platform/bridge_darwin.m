//go:build darwin && cgo

#import "bridge.h"
#import <AppKit/AppKit.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreAudio/CoreAudio.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreVideo/CoreVideo.h>
#import <IOKit/pwr_mgt/IOPMLib.h>
#import <QuartzCore/QuartzCore.h>
#include <stdatomic.h>
#include <pthread.h>

static _Atomic(uint64_t) currentGeneration;
static _Atomic(bool) shuttingDown;
static pthread_mutex_t eventMutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t commandMutex = PTHREAD_MUTEX_INITIALIZER;
static void (^latestCommand)(void);
static BOOL commandScheduled;
static NSMutableArray<NSData *> *events;
static NSWindow *stageWindow;
static CALayer *blackOverlay;
static AVPlayerLayer *videoLayer;
static NSString *stageDisplayID;
static BOOL stageEnabled;
static IOPMAssertionID powerAssertion = kIOPMNullAssertionID;
static id screenObserver;
static AudioObjectPropertyListenerBlock audioListener;
static void stopCurrent(void);
static void checkDevices(void);

// Go calls and AVFoundation callbacks do not necessarily arrive as AppKit
// events. Bound their temporary Objective-C objects to each main-queue task
// instead of depending on the application event loop's autorelease pool.
static void onMain(void (^work)(void)) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool { work(); }
    });
}

// Coalesce pending controls so STOP cannot sit behind a burst of UI blocks.
// The newest accepted generation supersedes every earlier pending command.
static void enqueueControl(uint64_t gen, void (^command)(void)) {
    pthread_mutex_lock(&commandMutex);
    atomic_store(&currentGeneration, gen);
    latestCommand = [command copy];
    BOOL wake = !commandScheduled; commandScheduled = YES;
    pthread_mutex_unlock(&commandMutex);
    if (wake) onMain(^{
        pthread_mutex_lock(&commandMutex);
        void (^next)(void) = latestCommand;
        latestCommand = nil; commandScheduled = NO;
        pthread_mutex_unlock(&commandMutex);
        if (next) next();
    });
}

static char *copyString(NSString *s) { return strdup(s.UTF8String ?: ""); }
static NSNumber *jbool(BOOL value) { return value ? @YES : @NO; }
static char *json(id value) {
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:NULL];
    if (!data) return strdup("{\"error\":\"Cannot encode native result\"}");
    char *result = malloc(data.length+1);
    if (result) { memcpy(result, data.bytes, data.length); result[data.length] = 0; }
    return result;
}
static char *errorJSON(NSString *message) { return json(@{@"error": message ?: @"Unknown native error"}); }
static double seconds(CMTime time) {
    double value = CMTimeGetSeconds(time);
    return isfinite(value) && value > 0 ? value : 0;
}
// Every event is emitted from the main queue; the Go poller only takes this lock
// while copying a bounded queue entry, never during media or filesystem work.
static void emit(uint64_t gen, NSString *kind, NSString *message, double position, double duration) {
    @autoreleasepool {
        NSDictionary *value = @{@"generation": @(gen), @"kind": kind, @"message": message ?: @"",
            @"position": @(position), @"duration": @(duration), @"stageEnabled": jbool(stageEnabled)};
        NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:NULL];
        if (!data) return;
        pthread_mutex_lock(&eventMutex);
        if (events.count >= 256) [events removeObjectAtIndex:0];
        [events addObject:data];
        pthread_mutex_unlock(&eventMutex);
    }
}
static NSString *audioString(AudioDeviceID device, AudioObjectPropertySelector selector) {
    AudioObjectPropertyAddress address = {selector, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
    CFStringRef value = NULL; UInt32 size = sizeof(value);
    if (AudioObjectGetPropertyData(device, &address, 0, NULL, &size, &value) != noErr || !value) return @"";
    return CFBridgingRelease(value);
}
static NSArray *audioDevices(void) {
    AudioObjectPropertyAddress address = {kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
    UInt32 size = 0;
    if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &address, 0, NULL, &size) != noErr) return @[];
    AudioDeviceID *ids = malloc(size);
    if (!ids) return @[];
    if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &address, 0, NULL, &size, ids) != noErr) { free(ids); return @[]; }
    AudioDeviceID defaultID = kAudioObjectUnknown; UInt32 defaultSize = sizeof(defaultID);
    AudioObjectPropertyAddress def = {kAudioHardwarePropertyDefaultOutputDevice, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
    AudioObjectGetPropertyData(kAudioObjectSystemObject, &def, 0, NULL, &defaultSize, &defaultID);
    NSMutableArray *result = [NSMutableArray array];
    for (UInt32 i=0; i<size/sizeof(AudioDeviceID); ++i) {
        AudioObjectPropertyAddress streams = {kAudioDevicePropertyStreamConfiguration, kAudioDevicePropertyScopeOutput, kAudioObjectPropertyElementMain};
        UInt32 bufferSize = 0;
        if (AudioObjectGetPropertyDataSize(ids[i], &streams, 0, NULL, &bufferSize) != noErr || !bufferSize) continue;
        AudioBufferList *buffers = malloc(bufferSize); UInt32 channels = 0;
        if (!buffers) continue;
        if (AudioObjectGetPropertyData(ids[i], &streams, 0, NULL, &bufferSize, buffers) == noErr)
            for (UInt32 j=0; j<buffers->mNumberBuffers; ++j) channels += buffers->mBuffers[j].mNumberChannels;
        free(buffers);
        if (!channels) continue;
        NSString *uid = audioString(ids[i], kAudioDevicePropertyDeviceUID);
        if (!uid.length) continue;
        [result addObject:@{@"id": uid, @"name": audioString(ids[i], kAudioObjectPropertyName), @"default": jbool(ids[i] == defaultID)}];
    }
    free(ids); return result;
}
static NSString *displayID(NSScreen *screen) {
    CGDirectDisplayID number = [screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
    CFUUIDRef uuid = CGDisplayCreateUUIDFromDisplayID(number);
    if (!uuid) return @"";
    NSString *value = CFBridgingRelease(CFUUIDCreateString(NULL, uuid)); CFRelease(uuid);
    return value ?: @"";
}
static NSArray *displays(void) {
    NSMutableArray *result = [NSMutableArray array];
    for (NSScreen *screen in NSScreen.screens) {
        CGDirectDisplayID number = [screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
        CGRect bounds = CGDisplayBounds(number);
        [result addObject:@{@"id": displayID(screen), @"name": screen.localizedName,
            @"x": @(bounds.origin.x), @"y": @(bounds.origin.y),
            @"width": @(CGDisplayPixelsWide(number)), @"height": @(CGDisplayPixelsHigh(number)),
            @"primary": jbool(number == CGMainDisplayID()), @"mirrored": jbool(CGDisplayIsInMirrorSet(number))}];
    }
    return result;
}

@interface SSStageWindow : NSWindow
@end
@implementation SSStageWindow
- (BOOL)canBecomeKeyWindow { return YES; }
- (void)keyDown:(NSEvent *)event {
    if (event.keyCode == 53) {
        uint64_t gen = atomic_fetch_add(&currentGeneration, 1);
        stopCurrent(); emit(gen, @"escape", nil, 0, 0);
    } else [super keyDown:event];
}
@end
@interface SSStageView : NSView
@end
@implementation SSStageView
- (void)resetCursorRects {
    NSImage *transparent = [[NSImage alloc] initWithSize:NSMakeSize(1,1)];
    [self addCursorRect:self.bounds cursor:[[NSCursor alloc] initWithImage:transparent hotSpot:NSZeroPoint]];
}
@end

@interface SSPlayback : NSObject
@property(nonatomic) uint64_t generation;
@property(nonatomic, strong) AVURLAsset *asset;
@property(nonatomic, strong) AVPlayerItem *item;
@property(nonatomic, strong) AVPlayer *player;
@property(nonatomic, copy) NSString *audioID;
@property(nonatomic) BOOL video;
@property(nonatomic) BOOL observing;
@property(nonatomic) BOOL playingReported;
@property(nonatomic) BOOL startRequested;
@property(nonatomic, strong) id timeObserver;
@property(nonatomic, strong) id endObserver;
@property(nonatomic, strong) id failureObserver;
- (void)teardown;
- (void)ready;
@end
static SSPlayback *active;
static void blackout(void) {
    [CATransaction begin]; [CATransaction setDisableActions:YES];
    blackOverlay.hidden = NO;
    videoLayer.hidden = YES;
    [CATransaction commit]; [CATransaction flush];
}
static void stopCurrent(void) {
    blackout();
    SSPlayback *old = active; active = nil;
    [old teardown];
}
static BOOL enableStage(NSString *identity) {
    NSScreen *target = nil;
    for (NSScreen *s in NSScreen.screens) if ([displayID(s) isEqual:identity]) { target = s; break; }
    if (!target) return NO;
    if (!stageWindow) {
        stageWindow = [[SSStageWindow alloc] initWithContentRect:target.frame styleMask:NSWindowStyleMaskBorderless
            backing:NSBackingStoreBuffered defer:NO screen:target];
        stageWindow.releasedWhenClosed = NO;
        stageWindow.backgroundColor = NSColor.blackColor;
        stageWindow.opaque = YES; stageWindow.hasShadow = NO;
        stageWindow.level = NSMainMenuWindowLevel+1;
        stageWindow.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
        SSStageView *view = [[SSStageView alloc] initWithFrame:NSMakeRect(0,0,target.frame.size.width,target.frame.size.height)];
        view.wantsLayer = YES; view.layer.backgroundColor = NSColor.blackColor.CGColor;
        stageWindow.contentView = view;
        videoLayer = [AVPlayerLayer playerLayerWithPlayer:nil];
        videoLayer.videoGravity = AVLayerVideoGravityResizeAspect;
        videoLayer.frame = view.bounds;
        videoLayer.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        [view.layer addSublayer:videoLayer];
        blackOverlay = [CALayer layer]; blackOverlay.backgroundColor = NSColor.blackColor.CGColor;
        blackOverlay.frame = view.bounds; blackOverlay.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        [view.layer addSublayer:blackOverlay];
    }
    [stageWindow setFrame:target.frame display:YES];
    stageDisplayID = [identity copy]; stageEnabled = YES; blackout(); [stageWindow orderFrontRegardless];
    if (powerAssertion == kIOPMNullAssertionID)
        IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleDisplaySleep, kIOPMAssertionLevelOn,
            CFSTR("Smart Stage stage output enabled"), &powerAssertion);
    return YES;
}
static void disableStage(void) {
    stopCurrent(); stageEnabled = NO; stageDisplayID = nil;
    [stageWindow orderOut:nil];
    if (powerAssertion != kIOPMNullAssertionID) { IOPMAssertionRelease(powerAssertion); powerAssertion = kIOPMNullAssertionID; }
}
static void failPlayback(SSPlayback *p, NSString *message) {
    if (!p || active != p || p.generation != atomic_load(&currentGeneration)) return;
    uint64_t g = p.generation;
    stopCurrent(); emit(g, @"error", message, 0, 0);
}
@implementation SSPlayback
- (void)teardown {
    [self.asset cancelLoading];
    self.player.muted = YES; [self.player pause];
    if (self.observing) {
        [self.item removeObserver:self forKeyPath:@"status"];
        [self.player removeObserver:self forKeyPath:@"timeControlStatus"];
        if (self.video) [videoLayer removeObserver:self forKeyPath:@"readyForDisplay"];
        self.observing = NO;
    }
    if (self.timeObserver) { [self.player removeTimeObserver:self.timeObserver]; self.timeObserver = nil; }
    if (self.endObserver) { [NSNotificationCenter.defaultCenter removeObserver:self.endObserver]; self.endObserver = nil; }
    if (self.failureObserver) { [NSNotificationCenter.defaultCenter removeObserver:self.failureObserver]; self.failureObserver = nil; }
    if (videoLayer.player == self.player) videoLayer.player = nil;
    [self.player replaceCurrentItemWithPlayerItem:nil];
    self.player = nil; self.item = nil; self.asset = nil;
}
- (void)ready {
    if (active != self || self.generation != atomic_load(&currentGeneration)) return;
    if (self.item.status == AVPlayerItemStatusFailed) {
        failPlayback(self, self.item.error.localizedDescription ?: @"Native decoder could not prepare this file"); return;
    }
    if (self.item.status != AVPlayerItemStatusReadyToPlay) return;
    if (!self.startRequested) {
        self.startRequested = YES;
        self.player.muted = NO;
        [self.player play];
    }
    if (self.player.timeControlStatus == AVPlayerTimeControlStatusPlaying) {
        if (!self.playingReported) {
            self.playingReported = YES;
            emit(self.generation, @"playing", nil, 0, seconds(self.item.duration));
        }
        if (self.video && videoLayer.readyForDisplay && stageEnabled) {
            [CATransaction begin]; [CATransaction setDisableActions:YES];
            videoLayer.hidden = NO; blackOverlay.hidden = YES;
            [CATransaction commit];
        }
    }
}
- (void)observeValueForKeyPath:(NSString *)keyPath ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
    (void)keyPath; (void)object; (void)change; (void)context;
    __weak SSPlayback *weakSelf = self;
    onMain(^{ [weakSelf ready]; });
}
@end

static void checkDevices(void) {
    @autoreleasepool {
        BOOL lostDisplay = stageEnabled;
        if (stageEnabled) for (NSScreen *s in NSScreen.screens)
            if ([displayID(s) isEqual:stageDisplayID]) { lostDisplay = NO; break; }
        BOOL lostAudio = active.audioID.length > 0;
        if (lostAudio) for (NSDictionary *d in audioDevices())
            if ([d[@"id"] isEqual:active.audioID]) { lostAudio = NO; break; }
        if (lostDisplay || lostAudio) {
            uint64_t g = atomic_fetch_add(&currentGeneration, 1);
            stopCurrent(); if (lostDisplay) disableStage();
            emit(g, @"device-lost", lostDisplay ? @"Stage display disconnected; select and enable it again" : @"Audio output disconnected; playback stopped", 0, 0);
        }
        emit(atomic_load(&currentGeneration), @"devices", nil, 0, 0);
    }
}
static void beginPlayback(uint64_t gen, NSString *path, NSString *audio, NSString *display, BOOL video) {
    if (gen != atomic_load(&currentGeneration)) return;
    stopCurrent();
    if (video && !enableStage(display)) { emit(gen, @"error", @"Select an available stage display before playing video", 0, 0); return; }
    SSPlayback *p = [[SSPlayback alloc] init]; p.generation = gen; p.audioID = audio; p.video = video; active = p;
    p.asset = [AVURLAsset URLAssetWithURL:[NSURL fileURLWithPath:path] options:@{AVURLAssetPreferPreciseDurationAndTimingKey:@YES}];
    __weak SSPlayback *weakP = p;
    [p.asset loadValuesAsynchronouslyForKeys:@[@"playable", @"tracks", @"duration"] completionHandler:^{
        onMain(^{
            SSPlayback *current = weakP;
            if (!current || active != current || gen != atomic_load(&currentGeneration)) return;
            NSError *error = nil;
            for (NSString *key in @[@"playable", @"tracks", @"duration"]) {
                if ([current.asset statusOfValueForKey:key error:&error] != AVKeyValueStatusLoaded) {
                    failPlayback(current, error.localizedDescription ?: @"Native asset loading failed"); return;
                }
            }
            if (!current.asset.playable || current.asset.hasProtectedContent) { failPlayback(current, @"File is unsupported or protected"); return; }
            BOOL hasAudio = [current.asset tracksWithMediaType:AVMediaTypeAudio].count > 0;
            BOOL hasVideo = [current.asset tracksWithMediaType:AVMediaTypeVideo].count > 0;
            if (!hasAudio && !hasVideo) { failPlayback(current, @"No audio or video tracks"); return; }
            if (hasVideo && !video) { failPlayback(current, @"File changed: video requires a stage display"); return; }
            if (hasAudio) {
                BOOL available = NO;
                for (NSDictionary *d in audioDevices()) if ([d[@"id"] isEqual:audio]) available = YES;
                if (!available) { failPlayback(current, @"Selected audio output is unavailable"); return; }
            }
            current.item = [AVPlayerItem playerItemWithAsset:current.asset];
            current.player = [AVPlayer playerWithPlayerItem:current.item];
            current.player.actionAtItemEnd = AVPlayerActionAtItemEndPause;
            current.player.automaticallyWaitsToMinimizeStalling = YES;
            current.player.muted = YES;
            if (audio.length) current.player.audioOutputDeviceUniqueID = audio;
            if (hasAudio && ![current.player.audioOutputDeviceUniqueID isEqual:audio]) { failPlayback(current, @"AVPlayer did not accept the requested audio output"); return; }
            if (video) videoLayer.player = current.player;
            [current.item addObserver:current forKeyPath:@"status" options:NSKeyValueObservingOptionNew context:NULL];
            [current.player addObserver:current forKeyPath:@"timeControlStatus" options:NSKeyValueObservingOptionNew context:NULL];
            if (video) [videoLayer addObserver:current forKeyPath:@"readyForDisplay" options:NSKeyValueObservingOptionNew context:NULL];
            current.observing = YES;
            current.timeObserver = [current.player addPeriodicTimeObserverForInterval:CMTimeMake(1,4) queue:dispatch_get_main_queue() usingBlock:^(CMTime time) {
                @autoreleasepool {
                    SSPlayback *playing = weakP;
                    if (playing && active == playing && gen == atomic_load(&currentGeneration))
                        emit(gen, @"progress", nil, seconds(time), seconds(playing.item.duration));
                }
            }];
            current.endObserver = [NSNotificationCenter.defaultCenter addObserverForName:AVPlayerItemDidPlayToEndTimeNotification object:current.item queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
                @autoreleasepool {
                    (void)note;
                    SSPlayback *finished = weakP;
                    if (finished && active == finished && gen == atomic_load(&currentGeneration)) { stopCurrent(); emit(gen, @"ended", nil, 0, 0); }
                }
            }];
            current.failureObserver = [NSNotificationCenter.defaultCenter addObserverForName:AVPlayerItemFailedToPlayToEndTimeNotification object:current.item queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
                @autoreleasepool {
                    NSError *reason = note.userInfo[AVPlayerItemFailedToPlayToEndTimeErrorKey];
                    failPlayback(weakP, reason.localizedDescription ?: @"Native playback failed");
                }
            }];
            [current ready];
        });
    }];
}

char *ss_init(void) {
    @autoreleasepool {
        if (!NSThread.isMainThread) return copyString(@"AppKit initialization must run on the process main thread");
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        [NSApp finishLaunching];
        events = [NSMutableArray array];
        if (![AVPlayer instancesRespondToSelector:@selector(setAudioOutputDeviceUniqueID:)])
            return copyString(@"This macOS AVPlayer cannot select a per-player audio output");
        screenObserver = [NSNotificationCenter.defaultCenter addObserverForName:NSApplicationDidChangeScreenParametersNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) { (void)note; checkDevices(); }];
        audioListener = ^(UInt32 n, const AudioObjectPropertyAddress *addresses) { (void)n; (void)addresses; checkDevices(); };
        for (NSNumber *selector in @[@(kAudioHardwarePropertyDevices), @(kAudioHardwarePropertyDefaultOutputDevice)]) {
            AudioObjectPropertyAddress address = {selector.unsignedIntValue, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
            AudioObjectAddPropertyListenerBlock(kAudioObjectSystemObject, &address, dispatch_get_main_queue(), audioListener);
        }
        return NULL;
    }
}
void ss_run(void) {
    @autoreleasepool {
        [NSApp run];
        disableStage();
        [NSNotificationCenter.defaultCenter removeObserver:screenObserver]; screenObserver = nil;
        for (NSNumber *selector in @[@(kAudioHardwarePropertyDevices), @(kAudioHardwarePropertyDefaultOutputDevice)]) {
            AudioObjectPropertyAddress address = {selector.unsignedIntValue, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
            AudioObjectRemovePropertyListenerBlock(kAudioObjectSystemObject, &address, dispatch_get_main_queue(), audioListener);
        }
        audioListener = nil;
        [stageWindow close]; stageWindow = nil; videoLayer = nil; blackOverlay = nil;
    }
}
void ss_quit(void) {
    atomic_store(&shuttingDown, true); atomic_fetch_add(&currentGeneration, 1);
    onMain(^{
        disableStage(); [NSApp stop:nil];
        NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];
        [NSApp postEvent:wake atStart:NO];
    });
}
void ss_free(char *p) { free(p); }
char *ss_poll(void) {
    @autoreleasepool {
        pthread_mutex_lock(&eventMutex);
        NSData *data = events.firstObject;
        if (data) [events removeObjectAtIndex:0];
        pthread_mutex_unlock(&eventMutex);
        if (!data) return NULL;
        char *p = malloc(data.length+1);
        if (p) { memcpy(p, data.bytes, data.length); p[data.length] = 0; }
        return p;
    }
}
char *ss_devices(void) {
    @autoreleasepool {
        if (atomic_load(&shuttingDown)) return errorJSON(@"Native backend is closed");
        NSArray *audio = audioDevices();
        __block NSArray *screens;
        dispatch_sync(dispatch_get_main_queue(), ^{
            // The caller's pool belongs to its Go thread. The strong __block
            // result survives this main-thread pool until JSON is copied out.
            @autoreleasepool { screens = displays(); }
        });
        return json(@{@"audio": audio, @"displays": screens});
    }
}
char *ss_inspect(const char *path) {
    @autoreleasepool {
        NSString *file = [NSString stringWithUTF8String:path];
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:[NSURL fileURLWithPath:file] options:@{AVURLAssetPreferPreciseDurationAndTimingKey:@YES}];
        dispatch_semaphore_t loaded = dispatch_semaphore_create(0);
        [asset loadValuesAsynchronouslyForKeys:@[@"playable", @"tracks", @"duration"] completionHandler:^{ dispatch_semaphore_signal(loaded); }];
        if (dispatch_semaphore_wait(loaded, dispatch_time(DISPATCH_TIME_NOW, 30*NSEC_PER_SEC)) != 0) {
            [asset cancelLoading]; return errorJSON(@"Native media inspection timed out after 30 seconds");
        }
        NSError *error = nil;
        for (NSString *key in @[@"playable", @"tracks", @"duration"])
            if ([asset statusOfValueForKey:key error:&error] != AVKeyValueStatusLoaded) return errorJSON(error.localizedDescription);
        if (!asset.playable || asset.hasProtectedContent) return errorJSON(@"File is unsupported or protected");
        BOOL audio = NO, video = NO;
        // AVAssetReader decodes an actual sample per selected media type. It has
        // no audio renderer, window or AVPlayer, so validation cannot leak output.
        for (AVMediaType type in @[AVMediaTypeAudio, AVMediaTypeVideo]) {
            AVAssetTrack *track = [asset tracksWithMediaType:type].firstObject;
            if (!track) continue;
            NSDictionary *settings = [type isEqual:AVMediaTypeAudio]
                ? @{AVFormatIDKey:@(kAudioFormatLinearPCM)}
                : @{(NSString *)kCVPixelBufferPixelFormatTypeKey:@(kCVPixelFormatType_32BGRA)};
            AVAssetReader *reader = [[AVAssetReader alloc] initWithAsset:asset error:&error];
            if (!reader) return errorJSON(error.localizedDescription);
            AVAssetReaderTrackOutput *output = [[AVAssetReaderTrackOutput alloc] initWithTrack:track outputSettings:settings];
            output.alwaysCopiesSampleData = NO;
            if (![reader canAddOutput:output]) return errorJSON(@"Native decoder cannot read this track");
            [reader addOutput:output];
            if (![reader startReading]) return errorJSON(reader.error.localizedDescription);
            CMSampleBufferRef sample = [output copyNextSampleBuffer];
            if (!sample) { [reader cancelReading]; return errorJSON(reader.error.localizedDescription ?: @"No decodable sample in media track"); }
            CFRelease(sample); [reader cancelReading];
            if ([type isEqual:AVMediaTypeAudio]) audio = YES; else video = YES;
        }
        if (!audio && !video) return errorJSON(@"No audio or video track");
        return json(@{@"kind": video ? @"video" : @"audio", @"hasAudio": jbool(audio), @"hasVideo": jbool(video), @"duration": @(seconds(asset.duration))});
    }
}
void ss_start(uint64_t gen, const char *path, const char *audio, const char *display, int video) {
    @autoreleasepool {
        NSString *file = [NSString stringWithUTF8String:path], *output = [NSString stringWithUTF8String:audio], *screen = [NSString stringWithUTF8String:display];
        enqueueControl(gen, ^{ beginPlayback(gen, file, output, screen, video != 0); });
    }
}
void ss_stop(uint64_t gen) {
    enqueueControl(gen, ^{
        if (gen != atomic_load(&currentGeneration)) return;
        stopCurrent(); emit(gen, @"stopped", nil, 0, 0);
    });
}
void ss_stage(uint64_t gen, const char *display, int enabled) {
    @autoreleasepool {
        NSString *identity = [NSString stringWithUTF8String:display];
        enqueueControl(gen, ^{
            if (gen != atomic_load(&currentGeneration)) return;
            stopCurrent();
            if (enabled && !enableStage(identity)) { emit(gen, @"error", @"Selected stage display is unavailable", 0, 0); return; }
            if (!enabled) disableStage();
            emit(gen, @"stopped", nil, 0, 0);
        });
    }
}
