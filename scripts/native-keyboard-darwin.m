// Test-only driver: dispatch an ordinary Cocoa event through the production
// bridge's NSApplication loop, without Accessibility or production test hooks.
#import <AppKit/AppKit.h>
#include "../internal/platform/bridge.h"
#include <string.h>
#include <stdlib.h>

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

int main(int argc, const char **argv) {
    @autoreleasepool {
        if (argc != 3) return 2;
        if (strcmp(argv[2], "desktop") == 0) return desktopChecks();
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
