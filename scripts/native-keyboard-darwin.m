// Test-only driver: dispatch an ordinary Cocoa event through the production
// bridge's NSApplication loop, without Accessibility or production test hooks.
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#include "../internal/platform/bridge.h"
#include <string.h>
#include <stdlib.h>
#include <unistd.h>

@protocol SSDesktopTestActions
- (void)openAdmin:(id)sender;
@end

// Test the same asynchronous native entry points used by authenticated Admin
// actions. KVC observes the test process's own panel, never another app's UI.
static int desktopChecks(void) {
    setenv("SMARTSTAGE_APP_LAUNCH", "1", 1);
    char *error = ss_init();
    if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
    if (ss_desktop_can_choose_files() || ss_desktop_choose_files()) return 5;
    id<SSDesktopTestActions> delegate = (id)NSApp.delegate;
    [delegate openAdmin:nil]; [delegate openAdmin:nil];
    if (!ss_desktop_poll_admin_request() || ss_desktop_poll_admin_request()) return 6;
    ss_desktop_admin("http://127.0.0.1:8787/admin");
    __block NSUInteger phase = 0, polls = 0;
    __block BOOL passed = NO;
    __block NSOpenPanel *firstPanel;
    __block NSOpenPanel *lastPanel;
    dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
    dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC),
                             50 * NSEC_PER_MSEC, NSEC_PER_MSEC);
    dispatch_source_set_event_handler(timer, ^{
        if (++polls > 200) { fprintf(stderr, "Desktop probe timeout at phase %lu\n", (unsigned long)phase); ss_quit(); return; }
        NSOpenPanel *panel = [(NSObject *)NSApp.delegate valueForKey:@"filePanel"];
        if (phase == 0 && ss_desktop_can_choose_files()) {
            if (!ss_desktop_choose_files() || !ss_desktop_choose_files()) { ss_quit(); return; }
            phase = 1;
        } else if (phase == 1 && panel) {
            firstPanel = panel;
            if (!ss_desktop_choose_files()) { ss_quit(); return; }
            phase = 2;
        } else if (phase == 2) {
            if (panel != firstPanel) { fprintf(stderr, "Chooser was duplicated\n"); ss_quit(); return; }
            [panel cancel:nil]; phase = 3;
        } else if (phase == 3 && !panel) {
            if (ss_desktop_files_pending() || !ss_desktop_choose_files()) { ss_quit(); return; }
            phase = 4;
        } else if (phase == 4 && panel) {
            lastPanel = panel;
            passed = YES;
            ss_quit();
        }
    });
    dispatch_resume(timer);
    ss_run();
    dispatch_source_cancel(timer);
    if (!passed || ss_desktop_can_choose_files() || ss_desktop_choose_files() || lastPanel.isVisible) return 7;
    puts("{\"nativeAdminRequestBufferedBeforeReadiness\":true,\"nativeAdminRequestsCoalesced\":true,\"chooserPendingAndOpenRequestsReusePanel\":true,\"chooserCancelAddsNoFiles\":true,\"chooserCanReopenAfterCancel\":true,\"quitClosesOpenChooser\":true,\"chooserUnavailableAfterShutdown\":true}");
    return 0;
}

static NSDictionary *takePlaylist(void) {
    char *raw = ss_desktop_take_playlist_result();
    if (!raw) return nil;
    NSData *data = [[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding];
    ss_free(raw);
    return [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
}

static int playlistChecks(void) {
    setenv("SMARTSTAGE_APP_LAUNCH", "1", 1);
    char *error = ss_init();
    if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
    if (ss_desktop_can_choose_playlists() || ss_desktop_choose_playlist(1, 1)) return 5;
    ss_desktop_language("it");
    ss_desktop_admin("http://127.0.0.1:8787/admin");
    NSString *filename = [NSString stringWithFormat:@"Playlist-%@.smartstage.json", NSUUID.UUID.UUIDString];
    __block NSUInteger phase = 0, polls = 0;
    __block BOOL passed = NO;
    __block NSSavePanel *shutdownPanel;

    dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
    dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), 100 * NSEC_PER_MSEC, NSEC_PER_MSEC);
    dispatch_source_set_event_handler(timer, ^{
        if (++polls > 300) { fprintf(stderr, "Playlist dialog probe timeout at phase %lu\n", (unsigned long)phase); ss_quit(); return; }
        NSSavePanel *panel = [(NSObject *)NSApp.delegate valueForKey:@"playlistPanel"];
        if (phase == 0 && ss_desktop_can_choose_playlists()) {
            if (ss_desktop_choose_playlist(0, 1) || !ss_desktop_choose_playlist(1, 1) || ss_desktop_choose_playlist(2, 0)) { ss_quit(); return; }
            phase = 1;
        } else if (phase == 1 && panel.isVisible) {
            if (![panel.title isEqualToString:@"Salva playlist"] || ![panel.nameFieldStringValue isEqualToString:@"Playlist.smartstage.json"] || ss_desktop_choose_files()) { ss_quit(); return; }
            panel.directoryURL = [NSURL fileURLWithPath:NSTemporaryDirectory() isDirectory:YES];
            panel.nameFieldStringValue = filename;
            phase = 2;
        } else if (phase == 2 && panel.isVisible) {
            // Modern Save controls are hosted by an AppKit service. Dispatch a
            // real WindowServer key pair, only while this process owns focus;
            // synthetic events sent inside NSApp never reach that service.
            [NSApp activateIgnoringOtherApps:YES];
            [panel makeKeyAndOrderFront:nil];
            NSRunningApplication *front = NSWorkspace.sharedWorkspace.frontmostApplication;
            BOOL allowed = CGPreflightPostEventAccess();
            fprintf(stderr, "Native Save event access=%d ownPID=%d frontPID=%d front=%s active=%d keyWindow=%ld panelWindow=%ld panelKey=%d\n",
                allowed, getpid(), front.processIdentifier, front.bundleIdentifier.UTF8String ?: "none", NSApp.isActive,
                (long)NSApp.keyWindow.windowNumber, (long)panel.windowNumber, panel.isKeyWindow);
            if (front.processIdentifier != getpid() || !panel.isKeyWindow) return;
            CGEventRef down = CGEventCreateKeyboardEvent(NULL, 36, YES);
            CGEventRef up = CGEventCreateKeyboardEvent(NULL, 36, NO);
            if (!down || !up) {
                if (down) CFRelease(down); if (up) CFRelease(up);
                fprintf(stderr, "Could not create the native Save Return events\n"); ss_quit(); return;
            }
            CGEventSetFlags(down, 0); CGEventSetFlags(up, 0);
            CGEventPost(kCGHIDEventTap, down); CFRelease(down);
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 50 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
                CGEventPost(kCGHIDEventTap, up); CFRelease(up);
            });
            phase = 3;
        } else if (phase == 3 && !panel) {
            NSDictionary *result = takePlaylist();
            if (!result) return;
            NSString *path = result[@"path"];
            if (![result[@"id"] isEqual:@1] || [result[@"cancelled"] boolValue] || [result[@"error"] length] || !path.isAbsolutePath || ![path.lastPathComponent isEqualToString:filename] || [NSFileManager.defaultManager fileExistsAtPath:path] || takePlaylist()) { ss_quit(); return; }
            if (!ss_desktop_choose_playlist(3, 0)) { ss_quit(); return; }
            phase = 4;
        } else if (phase == 4 && panel.isVisible) {
            if (![panel isKindOfClass:NSOpenPanel.class] || [(NSOpenPanel *)panel allowsMultipleSelection] || ![panel.title isEqualToString:@"Carica playlist"]) { ss_quit(); return; }
            [panel cancel:nil]; phase = 5;
        } else if (phase == 5 && !panel) {
            NSDictionary *result = takePlaylist();
            if (!result) return;
            if (![result[@"id"] isEqual:@3] || ![result[@"cancelled"] boolValue] || [result[@"path"] length] || [result[@"error"] length] || !ss_desktop_choose_playlist(4, 1)) { ss_quit(); return; }
            phase = 6;
        } else if (phase == 6 && panel.isVisible) {
            shutdownPanel = panel; passed = YES; ss_quit();
        }
    });
    dispatch_resume(timer); ss_run(); dispatch_source_cancel(timer);
    NSDictionary *result = takePlaylist();
    if (!passed || shutdownPanel.isVisible || ss_desktop_can_choose_playlists() || ss_desktop_choose_playlist(5, 0) || ![result[@"id"] isEqual:@4] || ![result[@"cancelled"] boolValue]) {
        fprintf(stderr, "Native playlist checks failed at phase %lu; final result: %s\n", (unsigned long)phase, result.description.UTF8String ?: "none");
        return 7;
    }
    puts("{\"nativePlaylistSaveReturnsPathWithoutWriting\":true,\"nativePlaylistLoadCancellationReturnsNoPath\":true,\"nativePlaylistConcurrentDialogsRejected\":true,\"nativePlaylistItalianLabels\":true,\"quitClosesOpenPlaylistChooser\":true}");
    return 0;
}

int main(int argc, const char **argv) {
    @autoreleasepool {
        if (argc != 3) return 2;
        if (strcmp(argv[2], "desktop") == 0) return desktopChecks();
        if (strcmp(argv[2], "playlist") == 0) return playlistChecks();
        char *error = ss_init();
        if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
        __block BOOL sent = NO, passed = NO;
        __block NSUInteger polls = 0;
        __block NSWindow *stage;
        dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
        dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC),
                                 20 * NSEC_PER_MSEC, NSEC_PER_MSEC);
        dispatch_source_set_event_handler(timer, ^{
            if (++polls > 500) { fprintf(stderr, "No Escape result before timeout\n"); ss_quit(); return; }
            char *raw;
            while ((raw = ss_poll())) {
                NSData *data = [[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding];
                ss_free(raw);
                NSDictionary *event = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
                if ([event[@"kind"] isEqual:@"stopped"] && [event[@"stageEnabled"] boolValue] && !sent) {
                    sent = YES;
                    for (NSWindow *window in NSApp.windows) if (window.isVisible) { stage = window; break; }
                    if (!stage) { fprintf(stderr, "No visible native stage\n"); ss_quit(); return; }
                    [stage makeKeyWindow];
                    NSEvent *key = [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint
                        modifierFlags:0 timestamp:NSProcessInfo.processInfo.systemUptime
                        windowNumber:strcmp(argv[2], "stage") == 0 ? stage.windowNumber : 0 context:nil
                        characters:@"\033" charactersIgnoringModifiers:@"\033" isARepeat:NO keyCode:53];
                    [NSApp postEvent:key atStart:NO];
                } else if ([event[@"kind"] isEqual:@"escape"]) {
                    passed = sent && ![event[@"stageEnabled"] boolValue] && !stage.isVisible;
                    ss_quit(); return;
                }
            }
        });
        dispatch_resume(timer);
        ss_stage(1, argv[1], 1);
        ss_run();
        dispatch_source_cancel(timer);
        if (!passed) return 4;
        printf("{\"nativeEscapeEvent\":true,\"stageDisabled\":true,\"stageWindowHidden\":true,\"target\":\"%s\"}\n", argv[2]);
        return 0;
    }
}
