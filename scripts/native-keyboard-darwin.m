// Test-only driver: dispatch an ordinary Cocoa event through the production
// bridge's NSApplication loop, without Accessibility or production test hooks.
#import <AppKit/AppKit.h>
#include "../internal/platform/bridge.h"
#include <string.h>

int main(int argc, const char **argv) {
    @autoreleasepool {
        if (argc != 3) return 2;
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
