// Test-only own-process observer linked against the production Cocoa bridge.
// It uses public WebKit/Cocoa APIs; no production debugging endpoint or hook.
#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include "../internal/platform/bridge.h"
#include <stdlib.h>

@interface SSProbeDraggingInfo : NSObject
@property(nonatomic, strong) NSPasteboard *draggingPasteboard;
@property(nonatomic, strong) NSWindow *draggingDestinationWindow;
@property(nonatomic) NSPoint draggingLocation;
@end
@implementation SSProbeDraggingInfo
- (NSDragOperation)draggingSourceOperationMask { return NSDragOperationCopy; }
- (id)draggingSource { return nil; }
- (NSInteger)draggingSequenceNumber { return 1; }
@end

static NSUInteger adminWindowCount(NSWindow *admin) {
    NSUInteger count = 0;
    for (NSWindow *window in NSApp.windows)
        if (window == admin || [window.contentView isKindOfClass:WKWebView.class] ||
            [window.identifier isEqual:admin.identifier]) count++;
    return count;
}
static BOOL menuHasTitle(NSMenu *menu, NSString *title) {
    for (NSMenuItem *item in menu.itemArray)
        if ([item.title isEqualToString:title] || (item.submenu && menuHasTitle(item.submenu, title))) return YES;
    return NO;
}

int main(int argc, const char **argv) {
    @autoreleasepool {
        (void)argv;
        // AppKit interprets positional process arguments as native file-open
        // requests during finishLaunching. Keep probe data out of argv so the
        // only file event in this test is the deliberate pasteboard drop.
        const char *adminAddress = getenv("SMARTSTAGE_PROBE_ADMIN_URL");
        const char *firstOriginal = getenv("SMARTSTAGE_PROBE_ORIGINAL_ONE");
        const char *secondOriginal = getenv("SMARTSTAGE_PROBE_ORIGINAL_TWO");
        if (argc != 1 || !adminAddress || !*adminAddress ||
            !firstOriginal || !*firstOriginal || !secondOriginal || !*secondOriginal) return 2;
        setenv("SMARTSTAGE_APP_LAUNCH", "1", 1);
        // Exercise selection before AppKit initialization, then restore English
        // for the existing host-page checks. No OS preference is changed.
        ss_desktop_language("it");
        char *error = ss_init();
        if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
        if (!menuHasTitle(NSApp.mainMenu, @"Apri Admin") || !menuHasTitle(NSApp.mainMenu, @"Esci da Smart Stage")) {
            fputs("Pre-initialization Italian selection did not reach the native menus\n", stderr); return 5;
        }
        ss_desktop_language("en");
        NSString *address = [NSString stringWithUTF8String:adminAddress];
        NSArray<NSURL *> *originals = @[
            [NSURL fileURLWithPath:[NSString stringWithUTF8String:firstOriginal]],
            [NSURL fileURLWithPath:[NSString stringWithUTF8String:secondOriginal]]
        ];
        ss_desktop_admin(address.UTF8String);
        if (!ss_desktop_has_admin_window() || !ss_desktop_show_admin()) return 4;
        __block NSUInteger phase = 0, polls = 0;
        __block BOOL evaluating = NO, passed = NO;
        __block NSWindow *originalWindow;
        __block WKWebView *originalWebView;
        __block NSNumber *originalEpoch;
        __block NSTimeInterval reopenStarted = 0;
        __block NSMutableDictionary *report = [NSMutableDictionary dictionary];
        report[@"italianNativePreferenceAppliedBeforeInitialization"] = @YES;
        __block NSString *failure;
        void (^fail)(NSString *) = ^(NSString *message) {
            failure = message;
            ss_quit();
        };
        dispatch_source_t timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
        dispatch_source_set_timer(timer, dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC),
                                 100 * NSEC_PER_MSEC, NSEC_PER_MSEC);
        dispatch_source_set_event_handler(timer, ^{
            if (++polls > 600) {
                fail([NSString stringWithFormat:@"Native Admin timeout at phase %lu", (unsigned long)phase]);
                return;
            }
            // Match Go's normal handling if a native reopen requests the UI.
            if (ss_desktop_poll_admin_request()) ss_desktop_show_admin();
            NSWindow *window = [(NSObject *)NSApp.delegate valueForKey:@"adminWindow"];
            WKWebView *webView = [(NSObject *)NSApp.delegate valueForKey:@"adminWebView"];
            if (!window || !webView || evaluating) return;
            if (!originalWindow) { originalWindow = window; originalWebView = webView; }
            if (window != originalWindow || webView != originalWebView || adminWindowCount(window) != 1) {
                fail(@"Native Admin duplicated or replaced its window/WebKit instance"); return;
            }
            if (phase == 0) {
                evaluating = YES;
                [webView evaluateJavaScript:
                    @"JSON.stringify({ready:document.getElementById('connection')?.textContent==='Connected to host' && typeof role!=='undefined' && role==='admin' && !localSessionBusy && typeof csrf!=='undefined' && csrf.length>0 && typeof source!=='undefined' && source?.readyState===EventSource.OPEN && typeof state!=='undefined' && state?.cues?.length===1,epoch:typeof state!=='undefined'?state?.stopEpoch:null,nativeAgent:navigator.userAgent.includes('SmartStageDesktop'),rows:document.getElementById('playlist')?.children.length})"
                    completionHandler:^(id result, NSError *jsError) {
                        evaluating = NO;
                        if (jsError || ![result isKindOfClass:NSString.class]) return;
                        NSDictionary *value = [NSJSONSerialization JSONObjectWithData:[result dataUsingEncoding:NSUTF8StringEncoding] options:0 error:NULL];
                        if (![value[@"ready"] boolValue]) return;
                        if (![value[@"nativeAgent"] boolValue] || [value[@"rows"] unsignedIntegerValue] < 1) {
                            fail(@"Real Admin assets did not render the desktop playlist"); return;
                        }
                        originalEpoch = value[@"epoch"];
                        report[@"realAdminAssetsLoadedInWKWebView"] = @YES;
                        report[@"localSessionAndCSRFInitialized"] = @YES;
                        report[@"authenticatedEventSourceConnected"] = @YES;
                        report[@"nativeUserAgentAndPlaylistRendered"] = @YES;
                        evaluating = YES;
                        [webView evaluateJavaScript:
                            @"window.__smartStageNativeProbe='preserved';document.getElementById('remote-connection-settings').open=true;document.getElementById('gateway-url').value='https://unsaved.example/smartstage';document.getElementById('gateway-url').dispatchEvent(new Event('input',{bubbles:true}));document.getElementById('stop').click();true"
                            completionHandler:^(id ignored, NSError *actionError) {
                                (void)ignored; evaluating = NO;
                                if (actionError) fail(actionError.description); else phase = 1;
                            }];
                    }];
            } else if (phase == 1) {
                evaluating = YES;
                [webView evaluateJavaScript:@"JSON.stringify({epoch:state?.stopEpoch,live:online,connected:document.getElementById('connection').textContent})"
                    completionHandler:^(id result, NSError *jsError) {
                        evaluating = NO;
                        if (jsError || ![result isKindOfClass:NSString.class]) return;
                        NSDictionary *value = [NSJSONSerialization JSONObjectWithData:[result dataUsingEncoding:NSUTF8StringEncoding] options:0 error:NULL];
                        if ([value[@"epoch"] unsignedLongLongValue] <= originalEpoch.unsignedLongLongValue || ![value[@"live"] boolValue]) return;
                        report[@"realCSRFProtectedStopAccepted"] = @YES;
                        report[@"liveStateAfterStopObserved"] = @YES;
                        [window performClose:nil]; phase = 2;
                    }];
            } else if (phase == 2) {
                if (window.isVisible) { fail(@"Closing Admin did not hide its native window"); return; }
                if (!ss_desktop_has_admin_window()) { fail(@"Closing Admin disabled its reusable UI"); return; }
                report[@"closeHidWindowWithoutTerminating"] = @YES;
                [NSApp.delegate applicationShouldHandleReopen:NSApp hasVisibleWindows:NO];
                reopenStarted = NSProcessInfo.processInfo.systemUptime;
                phase = 3;
            } else if (phase == 3 && window.isVisible) {
                evaluating = YES;
                [webView evaluateJavaScript:
                    @"JSON.stringify({draft:window.__smartStageNativeProbe,url:document.getElementById('gateway-url')?.value,settingsOpen:document.getElementById('remote-connection-settings')?.open,gatewayDirty:typeof gatewayDirty!=='undefined' && gatewayDirty,online:typeof online!=='undefined' && online,eventSourceState:typeof source!=='undefined'?source?.readyState:null,hidden:document.hidden})"
                    completionHandler:^(id result, NSError *jsError) {
                        evaluating = NO;
                        if (jsError || ![result isKindOfClass:NSString.class]) {
                            fail([NSString stringWithFormat:@"Could not inspect reopened Admin: error=%@ result=%@", jsError, result]); return;
                        }
                        NSDictionary *value = [NSJSONSerialization JSONObjectWithData:[result dataUsingEncoding:NSUTF8StringEncoding] options:0 error:NULL];
                        if (![value isKindOfClass:NSDictionary.class] ||
                            ![value[@"draft"] isEqual:@"preserved"] ||
                            ![value[@"url"] isEqual:@"https://unsaved.example/smartstage"] ||
                            ![value[@"settingsOpen"] boolValue] || ![value[@"gatewayDirty"] boolValue]) {
                            fail([NSString stringWithFormat:@"Reopening Admin reloaded or lost its unsaved UI state: %@", result]); return;
                        }
                        report[@"unsavedUIImmediatelyPreservedOnReopen"] = @YES;
                        // Visibility deliberately reconnects SSE. Preserve the
                        // draft on every poll, including the successful one,
                        // while allowing the live connection to recover.
                        BOOL connected = [value[@"online"] boolValue] &&
                            [value[@"eventSourceState"] isEqual:@1] && ![value[@"hidden"] boolValue];
                        if (!connected) {
                            if (NSProcessInfo.processInfo.systemUptime - reopenStarted >= 15) {
                                fail([NSString stringWithFormat:@"Reopened Admin did not restore its live connection: %@", result]);
                            }
                            return;
                        }
                        report[@"dockReopenKeptSameWindowAndWebView"] = @YES;
                        report[@"unsavedUIAndLiveConnectionPreserved"] = @YES;
                        report[@"liveEventSourceRecoveredAfterReopen"] = @YES;
                        if (!ss_desktop_show_admin() || !ss_desktop_show_admin()) { fail(@"Repeated Admin show request failed"); return; }
                        ss_desktop_language("it");
                        phase = 8;
                    }];
            } else if (phase == 8) {
                if (![window.title isEqualToString:@"Smart Stage — Amministrazione"] ||
                    !menuHasTitle(NSApp.mainMenu, @"Scegli file multimediali…") ||
                    !menuHasTitle(NSApp.mainMenu, @"Esci da Smart Stage")) return;
                report[@"italianNativeWindowAndMenusObservedAtRuntime"] = @YES;
                if (!ss_desktop_choose_files()) { fail(@"Localized native chooser request failed"); return; }
                phase = 9;
            } else if (phase == 9) {
                NSOpenPanel *panel = [(NSObject *)NSApp.delegate valueForKey:@"filePanel"];
                if (!panel || !panel.isVisible) return;
                if (![panel.title isEqualToString:@"Scegli i file per Smart Stage"] ||
                    ![panel.prompt isEqualToString:@"Aggiungi allo spettacolo"]) {
                    fail(@"Native chooser did not use the selected Italian labels"); return;
                }
                report[@"italianNativeChooserTitleAndPromptObserved"] = @YES;
                [panel cancel:nil];
                phase = 10;
            } else if (phase == 10) {
                if ([(NSObject *)NSApp.delegate valueForKey:@"filePanel"]) return;
                ss_desktop_language("en");
                phase = 11;
            } else if (phase == 11) {
                if (![window.title isEqualToString:@"Smart Stage — Admin"] ||
                    !menuHasTitle(NSApp.mainMenu, @"Open Admin") || !menuHasTitle(NSApp.mainMenu, @"Quit Smart Stage")) return;
                report[@"englishNativeLabelsRestoredAtRuntime"] = @YES;
                report[@"languageChangeRetainedSameNativeWindowAndWebView"] = @YES;
                phase = 4;
            } else if (phase == 4) {
                if (ss_desktop_files_pending()) {
                    fail(@"Native file queue was not empty before the deliberate drop"); return;
                }
                report[@"nativeFileQueueEmptyBeforeDrop"] = @YES;
                NSPoint dropPoint = NSMakePoint(NSMidX(webView.bounds), NSMidY(webView.bounds));
                // NSView hitTest: takes its superview's coordinates. Check
                // public Cocoa targeting before exercising the drop primitive;
                // this does not simulate a physical Finder dragging session.
                NSPoint hitPoint = [webView convertPoint:dropPoint toView:window.contentView.superview];
                if ([window.contentView hitTest:hitPoint] != webView) {
                    fail(@"Admin drop point did not hit the native WebKit destination"); return;
                }
                report[@"publicHitTestTargetsNativeAdminDropView"] = @YES;
                NSPasteboard *pasteboard = [NSPasteboard pasteboardWithUniqueName];
                [pasteboard clearContents];
                if (![pasteboard writeObjects:originals]) { fail(@"Could not create native file URL pasteboard"); return; }
                SSProbeDraggingInfo *drag = [SSProbeDraggingInfo new];
                drag.draggingPasteboard = pasteboard;
                drag.draggingDestinationWindow = window;
                drag.draggingLocation = [webView convertPoint:dropPoint toView:nil];
                id<NSDraggingInfo> info = (id<NSDraggingInfo>)drag;
                BOOL accepted = [webView draggingEntered:info] != NSDragOperationNone &&
                                [webView prepareForDragOperation:info] && [webView performDragOperation:info];
                [pasteboard releaseGlobally];
                if (!accepted) { fail(@"Production Admin native file drop was rejected"); return; }
                char *raw = ss_desktop_poll_files();
                if (!raw) { fail(@"Native Admin file drop did not queue original paths"); return; }
                NSData *data = [[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding];
                ss_free(raw);
                NSDictionary *request = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
                if ([request[@"paths"] count] != 2 || ![request[@"id"] unsignedLongLongValue]) {
                    fail(@"Native drop queue had unexpected paths"); return;
                }
                report[@"nativePasteboardFileURLDropAccepted"] = @YES;
                report[@"queuedOriginalPaths"] = request[@"paths"];
                ss_desktop_files_result([request[@"id"] unsignedLongLongValue], "");
                phase = 5;
            } else if (phase == 5) {
                // A genuine file mixed with a filename-looking string must
                // never let that text supply another original-file reference.
                NSPasteboard *pasteboard = [NSPasteboard pasteboardWithUniqueName];
                NSPasteboardItem *file = [NSPasteboardItem new];
                [file setString:originals[0].absoluteString forType:NSPasteboardTypeFileURL];
                NSPasteboardItem *text = [NSPasteboardItem new];
                [text setString:@"/tmp/forged-original.wav" forType:NSPasteboardTypeString];
                [pasteboard writeObjects:@[file, text]];
                SSProbeDraggingInfo *drag = [SSProbeDraggingInfo new];
                drag.draggingPasteboard = pasteboard;
                drag.draggingDestinationWindow = window;
                BOOL accepted = [webView performDragOperation:(id<NSDraggingInfo>)drag];
                [pasteboard releaseGlobally];
                char *raw = ss_desktop_poll_files();
                if (accepted || raw) { if (raw) ss_free(raw); fail(@"Mixed text was accepted as a native original path"); return; }
                report[@"mixedTextCannotSupplyNativeOriginalPaths"] = @YES;
                report[@"repeatedShowRetainedOneWindow"] = @YES;
                NSURLComponents *blocked = [NSURLComponents componentsWithString:address];
                blocked.path = @"/command";
                [webView loadRequest:[NSURLRequest requestWithURL:blocked.URL]];
                phase = 6;
            } else if (phase == 6) {
                if (webView.isLoading) return;
                if (![webView.URL.absoluteString isEqual:address]) { fail(@"WebKit navigated outside the Admin page"); return; }
                evaluating = YES;
                [webView evaluateJavaScript:@"window.__smartStageNativeProbe==='preserved'"
                    completionHandler:^(id result, NSError *jsError) {
                        evaluating = NO;
                        if (jsError || ![result boolValue]) { fail(@"Rejected navigation lost the real Admin page"); return; }
                        report[@"navigationOutsideAdminBlocked"] = @YES;
                        evaluating = YES;
                        [webView evaluateJavaScript:@"document.getElementById('quit-app').click();true"
                            completionHandler:^(id ignored, NSError *actionError) {
                                (void)ignored; evaluating = NO;
                                if (actionError) fail(actionError.description); else phase = 7;
                            }];
                    }];
            } else if (phase == 7) {
                evaluating = YES;
                [webView evaluateJavaScript:@"document.getElementById('app-closed').hidden===false"
                    completionHandler:^(id result, NSError *jsError) {
                        evaluating = NO;
                        if (jsError || ![result boolValue]) return;
                        report[@"realAdminQuitAcknowledged"] = @YES;
                        passed = YES;
                        ss_quit();
                    }];
            }
        });
        dispatch_resume(timer);
        ss_run();
        dispatch_source_cancel(timer);
        if (!passed || failure || originalWindow.isVisible) {
            fprintf(stderr, "%s\n", (failure ?: @"Native Admin did not finish or remained visible after shutdown").UTF8String);
            return 5;
        }
        report[@"nativeQuitClosedAdminWindow"] = @YES;
        report[@"status"] = @"passed";
        NSData *json = [NSJSONSerialization dataWithJSONObject:report options:0 error:NULL];
        fwrite(json.bytes, 1, json.length, stdout); fputc('\n', stdout);
        return 0;
    }
}
