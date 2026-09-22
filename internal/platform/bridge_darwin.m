//go:build darwin && cgo

#import "bridge.h"
#import <AppKit/AppKit.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreAudio/CoreAudio.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreVideo/CoreVideo.h>
#import <IOKit/pwr_mgt/IOPMLib.h>
#import <ImageIO/ImageIO.h>
#import <QuartzCore/QuartzCore.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import <WebKit/WebKit.h>
#include <stdatomic.h>
#include <pthread.h>
#include <signal.h>
#include <unistd.h>

static _Atomic(uint64_t) currentGeneration;
static _Atomic(bool) shuttingDown;
static _Atomic(bool) desktopReady;
static _Atomic(bool) desktopAdminRequested;
static _Atomic(bool) desktopChooserScheduled;
static _Atomic(bool) desktopAdminShowScheduled;
static pthread_mutex_t eventMutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t commandMutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t desktopFileMutex = PTHREAD_MUTEX_INITIALIZER;
static NSMutableArray<NSData *> *desktopFileQueue;
static NSMutableDictionary<NSNumber *, NSNumber *> *desktopFileCounts;
static NSUInteger desktopPendingFileCount;
static uint64_t desktopNextFileID;
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
static id keyObserver;
static AudioObjectPropertyListenerBlock audioListener;
static void stopCurrent(void);
static void emergencyStop(void);
static void checkDevices(void);
static BOOL sceneMode;
static void stopScene(void);
static void sceneEmergency(void);
static void sceneCheckDevices(void);

// Locale selection is atomic because Go may update it from an HTTP worker.
// AppKit objects are refreshed only on the main queue.
static _Atomic(int) desktopLanguage = -1;
static _Atomic(bool) desktopLanguageRefreshScheduled;
static NSDictionary<NSString *, NSString *> *desktopTranslations(void) {
    static NSDictionary<NSString *, NSString *> *translations;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        translations = @{
            @"Files could not be added": @"Impossibile aggiungere i file",
            @"Choose between 1 and 500 audio, video, or image files.": @"Scegli da 1 a 500 file audio, video o immagine.",
            @"Only files on this Mac or a mounted drive can be added.": @"Puoi aggiungere solo file presenti su questo Mac o su un’unità montata.",
            @"The selected file does not have a usable absolute filesystem path.": @"Il file selezionato non ha un percorso assoluto utilizzabile.",
            @"Smart Stage is still adding earlier files. Wait for that operation to finish, then try again.": @"Smart Stage sta ancora aggiungendo i file precedenti. Attendi il completamento, poi riprova.",
            @"The selected file paths could not be read.": @"Impossibile leggere i percorsi dei file selezionati.",
            @"Drop existing files from Finder. Mixed items and promised downloads cannot be added.": @"Trascina file esistenti dal Finder. Non puoi aggiungere elementi misti o download non ancora completati.",
            @"Smart Stage — Admin": @"Smart Stage — Amministrazione",
            @"Reload Admin": @"Ricarica Admin",
            @"Admin could not connect. Check that Smart Stage has finished starting, then reload.": @"Impossibile connettere Admin. Verifica che Smart Stage abbia completato l’avvio, poi ricarica.",
            @"The Admin page stopped responding. Reload to reconnect; the host is still running.": @"La pagina Admin non risponde. Ricarica per riconnetterti; Smart Stage è ancora in esecuzione.",
            @"The selected file does not have an absolute filesystem path.": @"Il file selezionato non ha un percorso assoluto.",
            @"Choose media for Smart Stage": @"Scegli i file per Smart Stage",
            @"Add to Show": @"Aggiungi allo spettacolo",
            @"Files stay in their original folders. Adding files does not start playback.": @"I file restano nelle cartelle originali. L’aggiunta non avvia la riproduzione.",
            @"Open Admin": @"Apri Admin",
            @"Choose Media…": @"Scegli file multimediali…",
            @"View Log": @"Visualizza registro",
            @"Quit Smart Stage": @"Esci da Smart Stage",
            @"File": @"File",
            @"Close Window": @"Chiudi finestra",
            @"Edit": @"Modifica",
            @"Undo": @"Annulla",
            @"Redo": @"Ripristina",
            @"Cut": @"Taglia",
            @"Copy": @"Copia",
            @"Paste": @"Incolla",
            @"Select All": @"Seleziona tutto",
            @"Smart Stage — Open Admin, view logs, or quit": @"Smart Stage — Apri Admin, visualizza il registro o esci",
            @"Smart Stage could not start": @"Impossibile avviare Smart Stage",
            @"%s\n\nDetails are saved in ~/Library/Logs/Smart Stage/smartstage.log.": @"%s\n\nI dettagli sono salvati in ~/Library/Logs/Smart Stage/smartstage.log.",
        };
    });
    return translations;
}
static NSString *desktopText(NSString *english) {
    if (atomic_load(&desktopLanguage) < 0) {
        char *language = ss_system_language();
        int unset = -1;
        atomic_compare_exchange_strong(&desktopLanguage, &unset, language && strcmp(language, "it") == 0 ? 1 : 0);
        free(language);
    }
    return atomic_load(&desktopLanguage) == 1 ? (desktopTranslations()[english] ?: english) : english;
}
static NSString *desktopRelocalize(NSString *text) {
    if (!text) return @"";
    for (NSString *english in desktopTranslations()) {
        if ([text isEqualToString:english] || [text isEqualToString:desktopTranslations()[english]])
            return desktopText(english);
    }
    return text;
}
char *ss_system_language(void) {
    @autoreleasepool {
        for (NSString *identifier in NSLocale.preferredLanguages) {
            NSString *language = [[NSLocale componentsFromLocaleIdentifier:identifier][NSLocaleLanguageCode] lowercaseString];
            if ([language isEqualToString:@"it"] || [language isEqualToString:@"en"])
                return strdup(language.UTF8String);
        }
        return strdup("en");
    }
}

// Only the Finder launcher opts into the Dock and menu bar lifecycle. CLI invocations
// keep their ordinary stdout/stderr and Ctrl+C behavior.
static BOOL desktopLaunch(void) {
    const char *value = getenv("SMARTSTAGE_APP_LAUNCH");
    return value && strcmp(value, "1") == 0;
}

// File errors must not enter a nested modal loop: Go can be finishing a Quit or
// an update while an operator leaves an error window unattended.
@interface SSFileAlert : NSObject <NSWindowDelegate>
@property(nonatomic, strong) NSAlert *alert;
- (void)dismiss:(id)sender;
@end
static NSMutableSet<SSFileAlert *> *desktopAlerts;
@implementation SSFileAlert
- (void)dismiss:(id)sender { (void)sender; [self.alert.window close]; }
- (void)windowWillClose:(NSNotification *)notification {
    (void)notification;
    [desktopAlerts removeObject:self];
}
@end
static void desktopFileAlert(NSString *message) {
    if (atomic_load(&shuttingDown)) return;
    // Bound outstanding windows when repeated file-open requests fail.
    if (desktopAlerts.count >= 3) {
        SSFileAlert *existing = desktopAlerts.anyObject;
        existing.alert.informativeText = message;
        [existing.alert.window makeKeyAndOrderFront:nil];
        return;
    }
    SSFileAlert *controller = [[SSFileAlert alloc] init];
    NSAlert *alert = [[NSAlert alloc] init];
    controller.alert = alert;
    alert.messageText = desktopText(@"Files could not be added");
    alert.informativeText = message;
    NSButton *button = [alert addButtonWithTitle:@"OK"];
    button.target = controller;
    button.action = @selector(dismiss:);
    button.keyEquivalent = @"\r";
    alert.window.delegate = controller;
    alert.window.releasedWhenClosed = NO;
    if (!desktopAlerts) desktopAlerts = [NSMutableSet set];
    [desktopAlerts addObject:controller];
    [alert.window center];
    [alert.window makeKeyAndOrderFront:nil];
}

// Native file-open events carry real filesystem references. The browser never
// supplies or guesses these paths, and the Go service validates them again
// against the configured media roots before changing the saved show.
static BOOL queueDesktopFiles(NSArray<NSURL *> *urls, NSString **failure) {
    if (!urls.count || urls.count > 500) {
        *failure = desktopText(@"Choose between 1 and 500 audio, video, or image files.");
        return NO;
    }
    NSMutableArray<NSString *> *paths = [NSMutableArray arrayWithCapacity:urls.count];
    for (NSURL *input in urls) {
        if (!input.isFileURL || (input.host.length && ![input.host.lowercaseString isEqualToString:@"localhost"])) {
            *failure = desktopText(@"Only files on this Mac or a mounted drive can be added.");
            return NO;
        }
        // Never touch a slow/network filesystem on AppKit's event thread.
        // Go resolves symlinks, checks regular files and media roots before save.
        NSString *path = input.path;
        if (!path.isAbsolutePath || !path.length || [path lengthOfBytesUsingEncoding:NSUTF8StringEncoding] > 32768) {
            *failure = desktopText(@"The selected file does not have a usable absolute filesystem path.");
            return NO;
        }
        [paths addObject:path];
    }
    pthread_mutex_lock(&desktopFileMutex);
    if (desktopFileCounts.count >= 8 || desktopPendingFileCount + paths.count > 500) {
        pthread_mutex_unlock(&desktopFileMutex);
        *failure = desktopText(@"Smart Stage is still adding earlier files. Wait for that operation to finish, then try again.");
        return NO;
    }
    uint64_t identifier = ++desktopNextFileID;
    if (!identifier) identifier = ++desktopNextFileID;
    NSData *data = [NSJSONSerialization dataWithJSONObject:@{@"id": @(identifier), @"paths": paths} options:0 error:NULL];
    if (!data) {
        pthread_mutex_unlock(&desktopFileMutex);
        *failure = desktopText(@"The selected file paths could not be read.");
        return NO;
    }
    desktopFileCounts[@(identifier)] = @(paths.count);
    desktopPendingFileCount += paths.count;
    [desktopFileQueue addObject:data];
    pthread_mutex_unlock(&desktopFileMutex);
    fprintf(stderr, "Queued %lu original media files from a native file-open action\n", (unsigned long)paths.count);
    return YES;
}

// The native destination receives this exact drag session's pasteboard. It
// never reads the global drag/clipboard pasteboard or interprets DOM strings as
// paths. Other drags keep WebKit's normal text and selection behavior.
@interface SSAdminWebView : WKWebView
@property(nonatomic) NSTimeInterval lastUserInteraction;
@end
@implementation SSAdminWebView
- (instancetype)initWithFrame:(NSRect)frame configuration:(WKWebViewConfiguration *)configuration {
    self = [super initWithFrame:frame configuration:configuration];
    if (self) {
        NSMutableArray<NSPasteboardType> *types = [self.registeredDraggedTypes mutableCopy];
        if (!types) types = [NSMutableArray array];
        if (![types containsObject:NSPasteboardTypeFileURL]) [types addObject:NSPasteboardTypeFileURL];
        [self registerForDraggedTypes:types];
    }
    return self;
}
- (BOOL)isNativeFileDrag:(id<NSDraggingInfo>)sender {
    return [sender.draggingPasteboard.types containsObject:NSPasteboardTypeFileURL];
}
- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)sender {
    if (![self isNativeFileDrag:sender]) return [super draggingEntered:sender];
    return atomic_load(&desktopReady) && !atomic_load(&shuttingDown) ? NSDragOperationCopy : NSDragOperationNone;
}
- (NSDragOperation)draggingUpdated:(id<NSDraggingInfo>)sender {
    if (![self isNativeFileDrag:sender]) return [super draggingUpdated:sender];
    return [self draggingEntered:sender];
}
- (BOOL)prepareForDragOperation:(id<NSDraggingInfo>)sender {
    if (![self isNativeFileDrag:sender]) return [super prepareForDragOperation:sender];
    return atomic_load(&desktopReady) && !atomic_load(&shuttingDown);
}
- (BOOL)performDragOperation:(id<NSDraggingInfo>)sender {
    if (![self isNativeFileDrag:sender]) return [super performDragOperation:sender];
    if (!atomic_load(&desktopReady) || atomic_load(&shuttingDown)) return NO;
    NSPasteboard *pasteboard = sender.draggingPasteboard;
    if (!pasteboard.pasteboardItems.count || pasteboard.pasteboardItems.count > 500) {
        desktopFileAlert(desktopText(@"Choose between 1 and 500 audio, video, or image files.")); return NO;
    }
    NSArray<NSURL *> *urls = [pasteboard readObjectsForClasses:@[NSURL.class]
        options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
    if (urls.count != pasteboard.pasteboardItems.count) {
        desktopFileAlert(desktopText(@"Drop existing files from Finder. Mixed items and promised downloads cannot be added.")); return NO;
    }
    NSString *failure = nil;
    if (!queueDesktopFiles(urls, &failure)) { desktopFileAlert(failure); return NO; }
    fputs("Queued original files from native Admin drop\n", stderr);
    return YES;
}
@end

@interface SSAdminWindow : NSWindow
@property(nonatomic, weak) SSAdminWebView *adminView;
@end
@implementation SSAdminWindow
- (void)sendEvent:(NSEvent *)event {
    if (event.type == NSEventTypeLeftMouseDown || event.type == NSEventTypeLeftMouseUp ||
        (event.type == NSEventTypeKeyDown && (event.keyCode == 36 || event.keyCode == 49))) {
        self.adminView.lastUserInteraction = NSProcessInfo.processInfo.systemUptime;
    }
    [super sendEvent:event];
}
@end

static BOOL validAdminURL(NSURL *url) {
    return [url.scheme.lowercaseString isEqualToString:@"http"] && [url.host isEqualToString:@"127.0.0.1"] &&
        url.port.integerValue > 0 && url.port.integerValue <= 65535 && [url.path isEqualToString:@"/admin"] &&
        !url.user.length && !url.password.length && !url.query.length;
}
static BOOL sameAdminPage(NSURL *url, NSURL *expected) {
    return validAdminURL(url) && validAdminURL(expected) && [url.port isEqual:expected.port];
}

@interface SSApplicationDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate>
@property(nonatomic, strong) NSStatusItem *status;
@property(nonatomic, strong) NSMutableArray<NSMenuItem *> *openItems;
@property(nonatomic, strong) NSMutableArray<NSMenuItem *> *quitItems;
@property(nonatomic, strong) NSURL *adminURL;
@property(nonatomic) BOOL quitPending;
@property(nonatomic) BOOL quitStarted;
@property(nonatomic) BOOL terminationPending;
@property(nonatomic, strong) NSOpenPanel *filePanel;
@property(nonatomic, strong) SSAdminWindow *adminWindow;
@property(nonatomic, strong) SSAdminWebView *adminWebView;
@property(nonatomic, strong) NSView *adminErrorView;
@property(nonatomic, strong) NSTextField *adminErrorLabel;
@property(nonatomic, strong) NSButton *adminRetryButton;
@property(nonatomic) BOOL adminShowPending;
@property(nonatomic) NSUInteger adminCrashRetries;
@property(nonatomic) NSTimeInterval adminCrashRetryStart;
- (void)openAdmin:(id)sender;
- (void)showAdminWindow;
- (void)reloadAdmin:(id)sender;
- (void)viewLog:(id)sender;
- (void)quit:(id)sender;
- (void)chooseMedia:(id)sender;
@end
static SSApplicationDelegate *applicationDelegate;

@implementation SSApplicationDelegate
- (void)showAdminError:(NSString *)message {
    if (self.quitStarted || atomic_load(&shuttingDown)) return;
    self.adminErrorLabel.stringValue = message;
    self.adminErrorView.hidden = NO;
}
- (void)reloadAdmin:(id)sender {
    (void)sender;
    if (self.quitStarted || atomic_load(&shuttingDown) || !validAdminURL(self.adminURL)) return;
    self.adminErrorView.hidden = YES;
    [self.adminWebView loadRequest:[NSURLRequest requestWithURL:self.adminURL
        cachePolicy:NSURLRequestReloadIgnoringLocalCacheData timeoutInterval:15]];
}
- (void)showAdminWindow {
    if (self.quitStarted || atomic_load(&shuttingDown)) return;
    if (!validAdminURL(self.adminURL)) { self.adminShowPending = YES; return; }
    self.adminShowPending = NO;
    if (!self.adminWindow) {
        // NSScreen.mainScreen can be a focused stage/projector. The menu-bar
        // screen is first; only initial placement uses it, never a reopen.
        NSRect screen = NSScreen.screens.firstObject.visibleFrame;
        CGFloat width = MIN(1100, MAX(640, screen.size.width - 80));
        CGFloat height = MIN(760, MAX(420, screen.size.height - 80));
        NSRect bounds = NSMakeRect(0, 0, width, height);
        SSAdminWindow *window = [[SSAdminWindow alloc] initWithContentRect:bounds
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
            backing:NSBackingStoreBuffered defer:NO];
        window.title = desktopText(@"Smart Stage — Admin");
        window.identifier = @"SmartStageAdminWindow";
        window.releasedWhenClosed = NO;
        window.minSize = NSMakeSize(640, 420);
        window.delegate = self;
        NSRect frame = window.frame;
        frame.origin = NSMakePoint(NSMidX(screen) - frame.size.width / 2, NSMidY(screen) - frame.size.height / 2);
        [window setFrame:frame display:NO];
        NSView *content = [[NSView alloc] initWithFrame:bounds];
        window.contentView = content;

        WKWebViewConfiguration *configuration = [[WKWebViewConfiguration alloc] init];
        configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        configuration.applicationNameForUserAgent = @"SmartStageDesktop";
        configuration.preferences.javaScriptCanOpenWindowsAutomatically = NO;
        configuration.mediaTypesRequiringUserActionForPlayback = WKAudiovisualMediaTypeAll;
        SSAdminWebView *webView = [[SSAdminWebView alloc] initWithFrame:content.bounds configuration:configuration];
        webView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        webView.navigationDelegate = self;
        webView.UIDelegate = self;
        webView.allowsBackForwardNavigationGestures = NO;
        [content addSubview:webView];
        window.adminView = webView;
        self.adminWindow = window;
        self.adminWebView = webView;

        NSView *errorView = [[NSView alloc] initWithFrame:NSMakeRect(0, height - 76, width, 76)];
        errorView.autoresizingMask = NSViewWidthSizable | NSViewMinYMargin;
        errorView.wantsLayer = YES;
        errorView.layer.backgroundColor = NSColor.windowBackgroundColor.CGColor;
        NSTextField *message = [NSTextField wrappingLabelWithString:@""];
        message.frame = NSMakeRect(18, 14, width - 145, 48);
        message.autoresizingMask = NSViewWidthSizable;
        NSButton *retry = [NSButton buttonWithTitle:desktopText(@"Reload Admin") target:self action:@selector(reloadAdmin:)];
        retry.frame = NSMakeRect(width - 122, 23, 112, 30);
        retry.autoresizingMask = NSViewMinXMargin;
        [errorView addSubview:message]; [errorView addSubview:retry];
        errorView.hidden = YES;
        [content addSubview:errorView];
        self.adminErrorView = errorView;
        self.adminErrorLabel = message;
        self.adminRetryButton = retry;
        [self reloadAdmin:nil];
        [window makeFirstResponder:webView];
        fputs("Created native Admin window\n", stderr);
    }
    if (self.adminWindow.isMiniaturized) [self.adminWindow deminiaturize:nil];
    [self.adminWindow makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
    fputs("Showed native Admin window\n", stderr);
}
- (BOOL)windowShouldClose:(NSWindow *)sender {
    if (sender == self.adminWindow && !self.quitStarted && !atomic_load(&shuttingDown)) {
        [sender orderOut:nil];
        fputs("Hid native Admin window\n", stderr);
        return NO;
    }
    return YES;
}
- (BOOL)openExternalAction:(WKNavigationAction *)action webView:(WKWebView *)webView {
    NSURL *url = action.request.URL;
    BOOL safeURL = ([url.scheme.lowercaseString isEqualToString:@"http"] || [url.scheme.lowercaseString isEqualToString:@"https"]) &&
        url.host.length && !url.user.length && !url.password.length;
    NSTimeInterval elapsed = NSProcessInfo.processInfo.systemUptime - self.adminWebView.lastUserInteraction;
    if (!safeURL || action.navigationType != WKNavigationTypeLinkActivated || !action.sourceFrame.isMainFrame ||
        !sameAdminPage(action.sourceFrame.request.URL, self.adminURL) || webView != self.adminWebView ||
        !self.adminWebView.lastUserInteraction || elapsed < 0 || elapsed > 2) return NO;
    self.adminWebView.lastUserInteraction = 0;
    if ([NSWorkspace.sharedWorkspace openURL:url]) fputs("Opened an explicit Admin link in the system browser\n", stderr);
    return YES;
}
- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action
        decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
    BOOL allowed = !self.quitStarted && !atomic_load(&shuttingDown) && webView == self.adminWebView &&
        action.targetFrame.isMainFrame && sameAdminPage(action.request.URL, self.adminURL) &&
        (!action.request.HTTPMethod || [action.request.HTTPMethod isEqualToString:@"GET"]);
    if (!allowed && !self.quitStarted && !atomic_load(&shuttingDown)) [self openExternalAction:action webView:webView];
    decisionHandler(allowed ? WKNavigationActionPolicyAllow : WKNavigationActionPolicyCancel);
}
- (void)webView:(WKWebView *)webView decidePolicyForNavigationResponse:(WKNavigationResponse *)response
        decisionHandler:(void (^)(WKNavigationResponsePolicy))decisionHandler {
    BOOL allowed = !self.quitStarted && !atomic_load(&shuttingDown) && webView == self.adminWebView &&
        response.isForMainFrame && response.canShowMIMEType && sameAdminPage(response.response.URL, self.adminURL) &&
        [response.response.MIMEType.lowercaseString isEqualToString:@"text/html"];
    decisionHandler(allowed ? WKNavigationResponsePolicyAllow : WKNavigationResponsePolicyCancel);
}
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
        forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features {
    (void)configuration; (void)features;
    if (!self.quitStarted && !atomic_load(&shuttingDown)) [self openExternalAction:action webView:webView];
    return nil;
}
- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
    (void)navigation;
    if (webView != self.adminWebView || !sameAdminPage(webView.URL, self.adminURL) || atomic_load(&shuttingDown)) return;
    self.adminErrorView.hidden = YES;
    fputs("Loaded native Admin page\n", stderr);
}
- (void)webView:(WKWebView *)webView didFailProvisionalNavigation:(WKNavigation *)navigation withError:(NSError *)error {
    (void)navigation;
    if (webView != self.adminWebView || error.code == NSURLErrorCancelled) return;
    [self showAdminError:desktopText(@"Admin could not connect. Check that Smart Stage has finished starting, then reload.")];
    fprintf(stderr, "Native Admin navigation failed (%ld)\n", (long)error.code);
}
- (void)webView:(WKWebView *)webView didFailNavigation:(WKNavigation *)navigation withError:(NSError *)error {
    [self webView:webView didFailProvisionalNavigation:navigation withError:error];
}
- (void)webViewWebContentProcessDidTerminate:(WKWebView *)webView {
    if (webView != self.adminWebView || self.quitStarted || atomic_load(&shuttingDown)) return;
    [self showAdminError:desktopText(@"The Admin page stopped responding. Reload to reconnect; the host is still running.")];
    NSTimeInterval now = NSProcessInfo.processInfo.systemUptime;
    if (now - self.adminCrashRetryStart > 60) { self.adminCrashRetryStart = now; self.adminCrashRetries = 0; }
    if (++self.adminCrashRetries > 2) return;
    __weak SSApplicationDelegate *weakSelf = self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC), dispatch_get_main_queue(), ^{
        SSApplicationDelegate *owner = weakSelf;
        if (owner && !owner.quitStarted && !atomic_load(&shuttingDown)) [owner reloadAdmin:nil];
    });
}
- (void)webView:(WKWebView *)webView runOpenPanelWithParameters:(WKOpenPanelParameters *)parameters
        initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(NSArray<NSURL *> *))completionHandler {
    (void)webView; (void)parameters; (void)frame;
    // Web content receives no file-reading privilege. The authenticated native
    // chooser and native drop queue are the only original-file entry points.
    completionHandler(nil);
}
- (void)webView:(WKWebView *)webView requestMediaCapturePermissionForOrigin:(WKSecurityOrigin *)origin
        initiatedByFrame:(WKFrameInfo *)frame type:(WKMediaCaptureType)type
        decisionHandler:(void (^)(WKPermissionDecision))decisionHandler {
    (void)webView; (void)origin; (void)frame; (void)type;
    decisionHandler(WKPermissionDecisionDeny);
}
- (void)application:(NSApplication *)application openURLs:(NSArray<NSURL *> *)urls {
    (void)application;
    NSString *failure = nil;
    if (!queueDesktopFiles(urls, &failure)) desktopFileAlert(failure);
}
- (void)application:(NSApplication *)application openFiles:(NSArray<NSString *> *)filenames {
    NSMutableArray<NSURL *> *urls = [NSMutableArray arrayWithCapacity:filenames.count];
    for (NSString *filename in filenames) {
        if (!filename.isAbsolutePath) {
            [application replyToOpenOrPrint:NSApplicationDelegateReplyFailure];
            desktopFileAlert(desktopText(@"The selected file does not have an absolute filesystem path."));
            return;
        }
        [urls addObject:[NSURL fileURLWithPath:filename]];
    }
    NSString *failure = nil;
    BOOL queued = queueDesktopFiles(urls, &failure);
    [application replyToOpenOrPrint:queued ? NSApplicationDelegateReplySuccess : NSApplicationDelegateReplyFailure];
    if (!queued) desktopFileAlert(failure);
}
- (void)chooseMedia:(id)sender {
    (void)sender;
    if (self.quitStarted || atomic_load(&shuttingDown)) return;
    if (self.filePanel) { [self.filePanel makeKeyAndOrderFront:nil]; return; }
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    self.filePanel = panel;
    panel.title = desktopText(@"Choose media for Smart Stage");
    panel.prompt = desktopText(@"Add to Show");
    panel.message = desktopText(@"Files stay in their original folders. Adding files does not start playback.");
    panel.canChooseFiles = YES;
    panel.canChooseDirectories = NO;
    panel.allowsMultipleSelection = YES;
    panel.resolvesAliases = YES;
    panel.allowedContentTypes = @[UTTypeAudio, UTTypeMovie, UTTypeImage];
    fputs("Opened native media chooser\n", stderr);
    [panel beginWithCompletionHandler:^(NSModalResponse result) {
        self.filePanel = nil;
        if (result != NSModalResponseOK) fputs("Cancelled native media chooser\n", stderr);
        if (result != NSModalResponseOK || self.quitStarted || atomic_load(&shuttingDown)) return;
        NSString *failure = nil;
        if (!queueDesktopFiles(panel.URLs, &failure)) desktopFileAlert(failure);
    }];
}
- (void)openAdmin:(id)sender {
    (void)sender;
    if (self.quitStarted || atomic_load(&shuttingDown)) return;
    // Go routes every entry point to the retained native Admin window or
    // browser policy. Keep one request when Finder reopens before Go is ready.
    atomic_store(&desktopAdminRequested, true);
    fputs("Requested native Admin window\n", stderr);
}
- (void)viewLog:(id)sender {
    (void)sender;
    const char *path = getenv("SMARTSTAGE_LOG_PATH");
    if (path) [NSWorkspace.sharedWorkspace openURL:[NSURL fileURLWithPath:[NSString stringWithUTF8String:path]]];
}
- (void)quit:(id)sender {
    (void)sender;
    // The signal context is installed before Go enables the menu. Let Go
    // close HTTP listeners, save state, and stop native playback normally.
    if (!self.adminURL) { self.quitPending = YES; return; }
    if (self.quitStarted) return;
    self.quitStarted = YES;
    for (NSMenuItem *item in self.quitItems) item.enabled = NO;
    fputs("Quitting Smart Stage from the app menu\n", stderr);
    kill(getpid(), SIGTERM);
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    self.terminationPending = YES;
    [self quit:sender];
    return NSTerminateLater;
}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)visible {
    (void)visible;
    [self openAdmin:sender];
    return NO;
}
@end

static void relocalizeDesktopMenu(NSMenu *menu) {
    menu.title = desktopRelocalize(menu.title);
    for (NSMenuItem *item in menu.itemArray) {
        item.title = desktopRelocalize(item.title);
        if (item.submenu) relocalizeDesktopMenu(item.submenu);
    }
}
void ss_desktop_language(const char *language) {
    atomic_store(&desktopLanguage, language && strcmp(language, "it") == 0 ? 1 : 0);
    if (!desktopLaunch() || atomic_load(&shuttingDown) || atomic_exchange(&desktopLanguageRefreshScheduled, true)) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            atomic_store(&desktopLanguageRefreshScheduled, false);
            if (atomic_load(&shuttingDown) || !applicationDelegate) return;
            relocalizeDesktopMenu(NSApp.mainMenu);
            relocalizeDesktopMenu(applicationDelegate.status.menu);
            applicationDelegate.status.button.toolTip = desktopText(@"Smart Stage — Open Admin, view logs, or quit");
            applicationDelegate.adminWindow.title = desktopText(@"Smart Stage — Admin");
            applicationDelegate.adminRetryButton.title = desktopText(@"Reload Admin");
            applicationDelegate.adminErrorLabel.stringValue = desktopRelocalize(applicationDelegate.adminErrorLabel.stringValue);
            applicationDelegate.filePanel.title = desktopText(@"Choose media for Smart Stage");
            applicationDelegate.filePanel.prompt = desktopText(@"Add to Show");
            applicationDelegate.filePanel.message = desktopText(@"Files stay in their original folders. Adding files does not start playback.");
            for (SSFileAlert *controller in desktopAlerts) {
                controller.alert.messageText = desktopText(@"Files could not be added");
                controller.alert.informativeText = desktopRelocalize(controller.alert.informativeText);
            }
        }
    });
}

static NSMenu *desktopMenu(BOOL applicationMenu) {
    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Smart Stage"];
    menu.autoenablesItems = NO;
    NSMenuItem *open = [[NSMenuItem alloc] initWithTitle:desktopText(@"Open Admin") action:@selector(openAdmin:) keyEquivalent:@""];
    open.target = applicationDelegate; open.enabled = NO;
    [applicationDelegate.openItems addObject:open];
    [menu addItem:open];
    NSMenuItem *choose = [[NSMenuItem alloc] initWithTitle:desktopText(@"Choose Media…") action:@selector(chooseMedia:)
        keyEquivalent:applicationMenu ? @"o" : @""];
    choose.target = applicationDelegate; choose.enabled = NO;
    [applicationDelegate.openItems addObject:choose];
    [menu addItem:choose];
    NSMenuItem *log = [[NSMenuItem alloc] initWithTitle:desktopText(@"View Log") action:@selector(viewLog:) keyEquivalent:@""];
    log.target = applicationDelegate;
    [menu addItem:log];
    [menu addItem:NSMenuItem.separatorItem];
    NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:desktopText(@"Quit Smart Stage")
        action:applicationMenu ? @selector(terminate:) : @selector(quit:)
        keyEquivalent:applicationMenu ? @"q" : @""];
    quit.target = applicationMenu ? (id)NSApp : (id)applicationDelegate;
    quit.enabled = NO;
    [applicationDelegate.quitItems addObject:quit];
    [menu addItem:quit];
    return menu;
}

static void setupDesktop(void) {
    if (!desktopLaunch()) return;
    desktopFileQueue = [NSMutableArray array];
    desktopFileCounts = [NSMutableDictionary dictionary];
    desktopPendingFileCount = 0;
    applicationDelegate = [[SSApplicationDelegate alloc] init];
    applicationDelegate.openItems = [NSMutableArray array];
    applicationDelegate.quitItems = [NSMutableArray array];
    NSApp.delegate = applicationDelegate;

    // The launcher execs the core in the same process. Set the icon explicitly
    // so the core executable's filename cannot replace the bundle artwork.
    const char *iconPath = getenv("SMARTSTAGE_APP_ICON");
    NSImage *icon = iconPath ? [[NSImage alloc] initWithContentsOfFile:[NSString stringWithUTF8String:iconPath]] : nil;
    if (icon.isValid) NSApp.applicationIconImage = icon;
    else fputs("Smart Stage: could not load the application's Dock icon\n", stderr);

    NSMenu *mainMenu = [[NSMenu alloc] initWithTitle:@""];
    NSMenuItem *applicationItem = [[NSMenuItem alloc] initWithTitle:@"Smart Stage" action:NULL keyEquivalent:@""];
    applicationItem.submenu = desktopMenu(YES);
    [mainMenu addItem:applicationItem];
    NSMenuItem *fileItem = [[NSMenuItem alloc] initWithTitle:desktopText(@"File") action:NULL keyEquivalent:@""];
    NSMenu *fileMenu = [[NSMenu alloc] initWithTitle:desktopText(@"File")];
    [fileMenu addItemWithTitle:desktopText(@"Close Window") action:@selector(performClose:) keyEquivalent:@"w"];
    fileItem.submenu = fileMenu;
    [mainMenu addItem:fileItem];
    NSMenuItem *editItem = [[NSMenuItem alloc] initWithTitle:desktopText(@"Edit") action:NULL keyEquivalent:@""];
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:desktopText(@"Edit")];
    [editMenu addItemWithTitle:desktopText(@"Undo") action:@selector(undo:) keyEquivalent:@"z"];
    NSMenuItem *redo = [editMenu addItemWithTitle:desktopText(@"Redo") action:@selector(redo:) keyEquivalent:@"z"];
    redo.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagShift;
    [editMenu addItem:NSMenuItem.separatorItem];
    [editMenu addItemWithTitle:desktopText(@"Cut") action:@selector(cut:) keyEquivalent:@"x"];
    [editMenu addItemWithTitle:desktopText(@"Copy") action:@selector(copy:) keyEquivalent:@"c"];
    [editMenu addItemWithTitle:desktopText(@"Paste") action:@selector(paste:) keyEquivalent:@"v"];
    [editMenu addItemWithTitle:desktopText(@"Select All") action:@selector(selectAll:) keyEquivalent:@"a"];
    editItem.submenu = editMenu;
    [mainMenu addItem:editItem];
    NSApp.mainMenu = mainMenu;

    NSStatusItem *status = [NSStatusBar.systemStatusBar statusItemWithLength:NSVariableStatusItemLength];
    applicationDelegate.status = status;
    status.button.title = @"Smart Stage";
    status.button.toolTip = desktopText(@"Smart Stage — Open Admin, view logs, or quit");
    status.menu = desktopMenu(NO);
    if (NSApp.activationPolicy == NSApplicationActivationPolicyRegular && icon.isValid)
        fputs("Smart Stage Dock icon and application menu ready\n", stderr);
}

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
@end
@interface SSStageView : NSView
@property(nonatomic, strong) NSTrackingArea *stageTracking;
@property(nonatomic, strong) NSCursor *stageCursor;
- (void)refreshStageCursor;
@end
@implementation SSStageView
- (NSCursor *)transparentCursor {
    if (!self.stageCursor) {
        NSImage *transparent = [[NSImage alloc] initWithSize:NSMakeSize(1, 1)];
        self.stageCursor = [[NSCursor alloc] initWithImage:transparent hotSpot:NSZeroPoint];
    }
    return self.stageCursor;
}
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:[self transparentCursor]]; }
- (void)updateTrackingAreas {
    if (self.stageTracking) [self removeTrackingArea:self.stageTracking];
    self.stageTracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect
        options:NSTrackingMouseEnteredAndExited | NSTrackingCursorUpdate | NSTrackingActiveAlways | NSTrackingInVisibleRect
        owner:self userInfo:nil];
    [self addTrackingArea:self.stageTracking];
    [super updateTrackingAreas];
}
- (void)refreshStageCursor {
    NSPoint point = NSEvent.mouseLocation;
    if (stageEnabled && self.window.isVisible && NSPointInRect(point, self.window.frame) &&
        [NSWindow windowNumberAtPoint:point belowWindowWithWindowNumber:0] == self.window.windowNumber)
        [[self transparentCursor] set];
}
- (void)cursorUpdate:(NSEvent *)event { (void)event; [self refreshStageCursor]; }
- (void)mouseEntered:(NSEvent *)event { (void)event; [self refreshStageCursor]; }
- (void)mouseExited:(NSEvent *)event { (void)event; [NSCursor.arrowCursor set]; }
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
        blackOverlay = [CALayer layer]; blackOverlay.backgroundColor = NSColor.blackColor.CGColor;
        blackOverlay.frame = view.bounds; blackOverlay.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        [view.layer addSublayer:blackOverlay];
    }
    if (!videoLayer) {
        NSView *view = stageWindow.contentView;
        videoLayer = [AVPlayerLayer playerLayerWithPlayer:nil];
        videoLayer.videoGravity = AVLayerVideoGravityResizeAspect;
        videoLayer.frame = view.bounds;
        videoLayer.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        videoLayer.hidden = YES;
        [view.layer insertSublayer:videoLayer below:blackOverlay];
    }
    [stageWindow setFrame:target.frame display:YES];
    stageDisplayID = [identity copy]; stageEnabled = YES; blackout(); [stageWindow orderFrontRegardless];
    [stageWindow invalidateCursorRectsForView:stageWindow.contentView];
    [(SSStageView *)stageWindow.contentView refreshStageCursor];
    if (powerAssertion == kIOPMNullAssertionID)
        IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleDisplaySleep, kIOPMAssertionLevelOn,
            CFSTR("Smart Stage stage output enabled"), &powerAssertion);
    return YES;
}
static void disableStage(void) {
    stopCurrent(); stageEnabled = NO; stageDisplayID = nil;
    [stageWindow orderOut:nil];
    [NSCursor.arrowCursor set];
    if (powerAssertion != kIOPMNullAssertionID) { IOPMAssertionRelease(powerAssertion); powerAssertion = kIOPMNullAssertionID; }
}
static void emergencyStop(void) {
    if (sceneMode) { sceneEmergency(); return; }
    uint64_t gen = atomic_fetch_add(&currentGeneration, 1);
    disableStage();
    emit(gen, @"escape", nil, 0, 0);
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
    if (videoLayer.player == self.player) {
        // Retire the renderer with its player. Keeping a single AVPlayerLayer
        // across replacements retained caption timers/timebases on macOS 15.
        // The window and opaque black overlay remain in place across STOP.
        [CATransaction begin]; [CATransaction setDisableActions:YES];
        videoLayer.player = nil;
        [videoLayer removeFromSuperlayer]; videoLayer = nil;
        [CATransaction commit]; [CATransaction flush];
    }
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
        if (sceneMode) { sceneCheckDevices(); return; }
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

// The scene path owns its decoders separately from the legacy diagnostic API.
// Only one foreground, one background and one retiring audio source are kept.
// Scene revisions order commands; foreground identity survives stage changes.
static _Atomic(uint64_t) requestedSceneRevision;
static _Atomic(bool) sceneEmergencyPending;
static uint64_t appliedSceneRevision, sceneGeneration, sceneEndedForegroundID;
static BOOL sceneBackgroundAudio, sceneStoppedPending;
static double sceneFadeSeconds, sceneFadeStarted, sceneFadeDuration;
static dispatch_source_t sceneFadeTimer;
static NSOperationQueue *sceneImageQueue;

@interface SSScenePlayer : NSObject
@property(nonatomic) uint64_t identifier;
@property(nonatomic, copy) NSString *path;
@property(nonatomic, copy) NSString *audioID;
@property(nonatomic, copy) NSString *pendingError;
@property(nonatomic) BOOL video, background, hasAudio, observing, started, reported, ended, disposed;
@property(nonatomic) float fadeFrom, fadeTo;
@property(nonatomic, strong) AVURLAsset *asset;
@property(nonatomic, strong) AVPlayerItem *item;
@property(nonatomic, strong) AVPlayer *player;
@property(nonatomic, strong) AVPlayerLayer *layer;
@property(nonatomic, strong) id timeObserver, endObserver, failureObserver;
- (void)prepare;
- (void)ready;
- (void)teardown;
@end
@interface SSSceneImage : NSObject
@property(nonatomic, copy) NSString *path;
@property(nonatomic, strong) CALayer *layer;
@property(nonatomic, strong) NSOperation *operation;
@property(nonatomic, copy) NSString *pendingError;
@property(nonatomic) BOOL background, disposed;
- (void)prepare;
- (void)teardown;
@end
static SSScenePlayer *sceneForeground, *sceneBackground, *sceneRetiring;
static SSSceneImage *sceneImage, *sceneBackgroundImage;
static void renderScene(void);
static void mixScene(void);
static BOOL audibleScenePlayer(SSScenePlayer *player);
static BOOL currentScene(void) {
    return sceneMode && !atomic_load(&shuttingDown) && !atomic_load(&sceneEmergencyPending) && appliedSceneRevision == atomic_load(&requestedSceneRevision);
}
static void emitScene(uint64_t generation, NSString *kind, NSString *message, double position, double duration) {
    if (!currentScene()) return;
    NSDictionary *value = @{ @"generation": @(generation), @"sceneRevision": @(appliedSceneRevision),
        @"kind": kind, @"message": message ?: @"", @"position": @(position), @"duration": @(duration),
        @"stageEnabled": jbool(stageEnabled) };
    NSData *data = [NSJSONSerialization dataWithJSONObject:value options:0 error:NULL];
    if (!data) return;
    pthread_mutex_lock(&eventMutex);
    if (events.count >= 256) [events removeObjectAtIndex:0];
    [events addObject:data];
    pthread_mutex_unlock(&eventMutex);
}
static void hideSceneStage(void) {
    stageEnabled = NO; stageDisplayID = nil;
    [stageWindow orderOut:nil];
    [NSCursor.arrowCursor set];
    if (powerAssertion != kIOPMNullAssertionID) {
        IOPMAssertionRelease(powerAssertion); powerAssertion = kIOPMNullAssertionID;
    }
}
static NSArray<SSScenePlayer *> *scenePlayers(void) {
    NSMutableArray *players = [NSMutableArray arrayWithCapacity:3];
    if (sceneForeground) [players addObject:sceneForeground];
    if (sceneBackground) [players addObject:sceneBackground];
    if (sceneRetiring) [players addObject:sceneRetiring];
    return players;
}
static void cancelSceneFade(void) {
    if (sceneFadeTimer) { dispatch_source_cancel(sceneFadeTimer); sceneFadeTimer = nil; }
}
static void completeSceneFade(void) {
    cancelSceneFade();
    if (sceneRetiring && sceneRetiring.player.volume <= 0.001f) {
        [sceneRetiring teardown]; sceneRetiring = nil;
    }
    if (sceneStoppedPending && !sceneForeground && !audibleScenePlayer(sceneRetiring) && currentScene()) {
        sceneStoppedPending = NO;
        emitScene(sceneGeneration, @"stopped", nil, 0, 0);
    }
}
static BOOL audibleScenePlayer(SSScenePlayer *player) {
    return player && player.hasAudio && !player.ended && player.started &&
        player.player.volume > 0.001f && player.player.timeControlStatus == AVPlayerTimeControlStatusPlaying;
}
static void retireScenePlayer(SSScenePlayer *player) {
    if (!player) return;
    player.layer.hidden = YES;
    if (audibleScenePlayer(player) && sceneFadeSeconds > 0) {
        // A burst of PLAY commands cannot accumulate decoder/audio tails.
        if (sceneRetiring != player) [sceneRetiring teardown];
        sceneRetiring = player;
    } else [player teardown];
}
static void mixScene(void) {
    if (!currentScene()) return;
    SSScenePlayer *owner = nil;
    BOOL foregroundWaiting = sceneForeground && sceneForeground.hasAudio && !sceneForeground.started && !sceneForeground.ended;
    if (sceneForeground.hasAudio && sceneForeground.started && !sceneForeground.ended) owner = sceneForeground;
    else if (foregroundWaiting && audibleScenePlayer(sceneRetiring)) owner = sceneRetiring;
    else if (stageEnabled && sceneBackgroundAudio && sceneBackground.hasAudio && sceneBackground.started) owner = sceneBackground;
    else if (!sceneForeground && stageEnabled && sceneBackgroundAudio && sceneBackground && !sceneBackground.started && audibleScenePlayer(sceneRetiring)) owner = sceneRetiring;
    NSArray<SSScenePlayer *> *players = scenePlayers();
    BOOL changed = NO, audible = NO;
    for (SSScenePlayer *player in players) {
        float target = player == owner ? 1.0f : 0.0f;
        if (fabsf(player.fadeTo - target) > 0.0001f) changed = YES;
        if (audibleScenePlayer(player)) audible = YES;
    }
    if (!changed && sceneFadeTimer) return;
    if (!changed) { completeSceneFade(); return; }
    cancelSceneFade();
    sceneFadeStarted = NSProcessInfo.processInfo.systemUptime;
    // Starting from silence is immediate. Only replace/fade audible output.
    sceneFadeDuration = audible ? sceneFadeSeconds : 0;
    for (SSScenePlayer *player in players) {
        player.fadeFrom = player.player.volume;
        player.fadeTo = player == owner ? 1.0f : 0.0f;
        if (sceneFadeDuration <= 0) player.player.volume = player.fadeTo;
    }
    if (sceneFadeDuration <= 0) { completeSceneFade(); return; }
    sceneFadeTimer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
    dispatch_source_set_timer(sceneFadeTimer, DISPATCH_TIME_NOW, 20 * NSEC_PER_MSEC, 2 * NSEC_PER_MSEC);
    dispatch_source_set_event_handler(sceneFadeTimer, ^{
        @autoreleasepool {
            if (!currentScene()) return; // A queued newer scene will retarget from current volumes.
            double progress = MIN(1.0, (NSProcessInfo.processInfo.systemUptime - sceneFadeStarted) / sceneFadeDuration);
            for (SSScenePlayer *player in scenePlayers())
                player.player.volume = player.fadeFrom + (player.fadeTo - player.fadeFrom) * progress;
            if (progress >= 1) completeSceneFade();
        }
    });
    dispatch_resume(sceneFadeTimer);
}
static void renderScene(void) {
    if (!currentScene()) return;
    [CATransaction begin]; [CATransaction setDisableActions:YES];
    CALayer *visible = nil;
    if (stageEnabled) {
        if (sceneImage.layer.contents) visible = sceneImage.layer;
        else if (sceneForeground.video && sceneForeground.layer.readyForDisplay && !sceneForeground.ended) visible = sceneForeground.layer;
        else if (sceneBackgroundImage.layer.contents) visible = sceneBackgroundImage.layer;
        else if (sceneBackground.layer.readyForDisplay) visible = sceneBackground.layer;
    }
    for (SSScenePlayer *player in scenePlayers()) player.layer.hidden = player.layer != visible;
    sceneImage.layer.hidden = sceneImage.layer != visible;
    sceneBackgroundImage.layer.hidden = sceneBackgroundImage.layer != visible;
    blackOverlay.hidden = visible != nil;
    videoLayer.hidden = YES;
    [CATransaction commit];
}
static void scenePlayerFailed(SSScenePlayer *player, NSString *message) {
    if (!player || player.disposed) return;
    if (!currentScene() && (player == sceneForeground || player == sceneBackground)) {
        player.pendingError = message; return;
    }
    if (player == sceneBackground) {
        [player teardown]; sceneBackground = nil;
        emitScene(sceneGeneration, @"background-error", message, 0, 0);
        renderScene(); mixScene();
    } else if (player == sceneForeground) {
        uint64_t identifier = player.identifier;
        [player teardown]; sceneForeground = nil;
        // Invalid foreground media must not leave an older cue playing.
        retireScenePlayer(sceneRetiring);
        emitScene(identifier, @"error", message, 0, 0);
        renderScene(); mixScene();
    } else if (player == sceneRetiring) { [player teardown]; sceneRetiring = nil; mixScene(); }
}
@implementation SSScenePlayer
- (void)teardown {
    if (self.disposed) return;
    self.disposed = YES;
    [self.asset cancelLoading];
    self.player.muted = YES; self.player.volume = 0; [self.player pause];
    if (self.observing) {
        [self.item removeObserver:self forKeyPath:@"status"];
        [self.player removeObserver:self forKeyPath:@"timeControlStatus"];
        if (self.video) [self.layer removeObserver:self forKeyPath:@"readyForDisplay"];
        self.observing = NO;
    }
    if (self.timeObserver) [self.player removeTimeObserver:self.timeObserver];
    if (self.endObserver) [NSNotificationCenter.defaultCenter removeObserver:self.endObserver];
    if (self.failureObserver) [NSNotificationCenter.defaultCenter removeObserver:self.failureObserver];
    self.timeObserver = nil; self.endObserver = nil; self.failureObserver = nil;
    self.layer.player = nil; [self.layer removeFromSuperlayer]; self.layer = nil;
    [self.player replaceCurrentItemWithPlayerItem:nil];
    self.player = nil; self.item = nil; self.asset = nil;
}
- (void)ready {
    if (self.disposed || !currentScene()) return;
    if (self != sceneForeground && self != sceneBackground && self != sceneRetiring) return;
    if (self.pendingError.length) { scenePlayerFailed(self, self.pendingError); return; }
    if (self.item.status == AVPlayerItemStatusFailed) {
        scenePlayerFailed(self, self.item.error.localizedDescription ?: @"Native decoder could not prepare this file"); return;
    }
    if (self.item.status != AVPlayerItemStatusReadyToPlay) return;
    if (self.background && !stageEnabled) return;
    if (!self.started && !self.ended && currentScene()) { self.player.muted = NO; [self.player play]; }
    if (self.player.timeControlStatus == AVPlayerTimeControlStatusPlaying) {
        self.started = YES;
        if (self == sceneForeground && !self.reported) {
            self.reported = YES;
            emitScene(self.identifier, @"playing", nil, seconds(self.player.currentTime), seconds(self.item.duration));
        }
        mixScene();
    }
    renderScene();
}
- (void)observeValueForKeyPath:(NSString *)keyPath ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
    (void)keyPath; (void)object; (void)change; (void)context;
    __weak SSScenePlayer *weakSelf = self;
    onMain(^{ [weakSelf ready]; });
}
- (void)prepare {
    self.asset = [AVURLAsset URLAssetWithURL:[NSURL fileURLWithPath:self.path]
        options:@{AVURLAssetPreferPreciseDurationAndTimingKey:@YES}];
    __weak SSScenePlayer *weakSelf = self;
    [self.asset loadValuesAsynchronouslyForKeys:@[@"playable", @"tracks", @"duration"] completionHandler:^{
        onMain(^{
            SSScenePlayer *player = weakSelf;
            if (!player || player.disposed) return;
            NSError *error = nil;
            for (NSString *key in @[@"playable", @"tracks", @"duration"])
                if ([player.asset statusOfValueForKey:key error:&error] != AVKeyValueStatusLoaded) {
                    scenePlayerFailed(player, error.localizedDescription ?: @"Native asset loading failed"); return;
                }
            if (!player.asset.playable || player.asset.hasProtectedContent) {
                scenePlayerFailed(player, @"File is unsupported or protected"); return;
            }
            BOOL audio = [player.asset tracksWithMediaType:AVMediaTypeAudio].count > 0;
            BOOL video = [player.asset tracksWithMediaType:AVMediaTypeVideo].count > 0;
            if (video != player.video || (!audio && !video) || (!player.background && audio != player.hasAudio)) {
                scenePlayerFailed(player, @"File changed: media tracks no longer match the validated cue"); return;
            }
            player.hasAudio = audio;
            if (audio && (!player.background || sceneBackgroundAudio)) {
                BOOL found = NO;
                for (NSDictionary *device in audioDevices()) if ([device[@"id"] isEqual:player.audioID]) found = YES;
                if (!found) { scenePlayerFailed(player, @"Selected audio output is unavailable"); return; }
            }
            player.item = [AVPlayerItem playerItemWithAsset:player.asset];
            player.player = [AVPlayer playerWithPlayerItem:player.item];
            player.player.actionAtItemEnd = AVPlayerActionAtItemEndPause;
            player.player.automaticallyWaitsToMinimizeStalling = YES;
            player.player.volume = 0; player.player.muted = YES;
            if (player.audioID.length) player.player.audioOutputDeviceUniqueID = player.audioID;
            if (audio && player.audioID.length && ![player.player.audioOutputDeviceUniqueID isEqual:player.audioID]) {
                scenePlayerFailed(player, @"AVPlayer did not accept the requested audio output"); return;
            }
            if (player.video) {
                player.layer = [AVPlayerLayer playerLayerWithPlayer:player.player];
                player.layer.videoGravity = AVLayerVideoGravityResizeAspect;
                player.layer.frame = stageWindow.contentView.bounds;
                player.layer.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
                player.layer.hidden = YES;
                [stageWindow.contentView.layer insertSublayer:player.layer below:blackOverlay];
            }
            [player.item addObserver:player forKeyPath:@"status" options:NSKeyValueObservingOptionNew context:NULL];
            [player.player addObserver:player forKeyPath:@"timeControlStatus" options:NSKeyValueObservingOptionNew context:NULL];
            if (player.video) [player.layer addObserver:player forKeyPath:@"readyForDisplay" options:NSKeyValueObservingOptionNew context:NULL];
            player.observing = YES;
            player.timeObserver = [player.player addPeriodicTimeObserverForInterval:CMTimeMake(1, 4) queue:dispatch_get_main_queue() usingBlock:^(CMTime time) {
                SSScenePlayer *source = weakSelf;
                if (source && source == sceneForeground && !source.disposed && !source.ended)
                    emitScene(source.identifier, @"progress", nil, seconds(time), seconds(source.item.duration));
            }];
            player.endObserver = [NSNotificationCenter.defaultCenter addObserverForName:AVPlayerItemDidPlayToEndTimeNotification object:player.item queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
                (void)note; SSScenePlayer *source = weakSelf;
                if (!source || source.disposed) return;
                if (source.background && (source == sceneBackground || source == sceneRetiring)) {
                    [source.player seekToTime:kCMTimeZero toleranceBefore:kCMTimeZero toleranceAfter:kCMTimeZero completionHandler:^(BOOL finished) {
                        onMain(^{ SSScenePlayer *loop = weakSelf;
                            if (finished && loop && !loop.disposed && stageEnabled && currentScene()) [loop.player play];
                        });
                    }];
                } else if (source == sceneForeground) {
                    uint64_t identifier = source.identifier;
                    source.ended = YES; sceneEndedForegroundID = identifier;
                    [source teardown]; sceneForeground = nil;
                    renderScene(); mixScene(); emitScene(identifier, @"ended", nil, 0, 0);
                } else if (source == sceneRetiring) { [source teardown]; sceneRetiring = nil; mixScene(); }
            }];
            player.failureObserver = [NSNotificationCenter.defaultCenter addObserverForName:AVPlayerItemFailedToPlayToEndTimeNotification object:player.item queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
                NSError *error = note.userInfo[AVPlayerItemFailedToPlayToEndTimeErrorKey];
                scenePlayerFailed(weakSelf, error.localizedDescription ?: @"Native playback failed");
            }];
            [player ready];
        });
    }];
}
@end

// ImageIO supplies a decoded, orientation-correct raster without a renderer.
// Reject absurd dimensions before decoding and cap the displayed raster size.
static CGImageRef copySceneImage(NSString *path, NSString **failure) {
    CGImageSourceRef source = CGImageSourceCreateWithURL((__bridge CFURLRef)[NSURL fileURLWithPath:path], NULL);
    if (!source) { if (failure) *failure = @"Image could not be opened"; return NULL; }
    NSDictionary *properties = CFBridgingRelease(CGImageSourceCopyPropertiesAtIndex(source, 0, NULL));
    double width = [properties[(NSString *)kCGImagePropertyPixelWidth] doubleValue];
    double height = [properties[(NSString *)kCGImagePropertyPixelHeight] doubleValue];
    if (width <= 0 || height <= 0 || width > 50000 || height > 50000 || width * height > 100000000) {
        CFRelease(source); if (failure) *failure = @"Image dimensions are invalid or exceed 100 megapixels"; return NULL;
    }
    NSDictionary *options = @{(NSString *)kCGImageSourceCreateThumbnailFromImageAlways:@YES,
        (NSString *)kCGImageSourceCreateThumbnailWithTransform:@YES,
        (NSString *)kCGImageSourceThumbnailMaxPixelSize:@8192,
        (NSString *)kCGImageSourceShouldCacheImmediately:@YES};
    CGImageRef image = CGImageSourceCreateThumbnailAtIndex(source, 0, (__bridge CFDictionaryRef)options);
    CFRelease(source);
    if (!image && failure) *failure = @"Native image decoder could not read this image";
    return image;
}
@implementation SSSceneImage
- (void)teardown {
    self.disposed = YES; [self.operation cancel]; self.operation = nil;
    [self.layer removeFromSuperlayer]; self.layer.contents = nil; self.layer = nil;
}
- (void)prepare {
    if (!sceneImageQueue) { sceneImageQueue = [[NSOperationQueue alloc] init]; sceneImageQueue.maxConcurrentOperationCount = 1; sceneImageQueue.qualityOfService = NSQualityOfServiceUserInitiated; }
    __weak SSSceneImage *weakSelf = self;
    NSString *path = self.path;
    __block __weak NSBlockOperation *weakOperation = nil;
    NSBlockOperation *operation = [NSBlockOperation blockOperationWithBlock:^{
        @autoreleasepool {
            if (weakOperation.cancelled) return;
            NSString *failure = nil;
            CGImageRef raster = copySceneImage(path, &failure);
            onMain(^{
                SSSceneImage *target = weakSelf;
                if (target && !target.disposed) {
                    if (raster) {
                        target.layer = [CALayer layer]; target.layer.contents = (__bridge id)raster;
                        target.layer.contentsGravity = kCAGravityResizeAspect;
                        target.layer.frame = stageWindow.contentView.bounds;
                        target.layer.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
                        target.layer.hidden = YES;
                        [stageWindow.contentView.layer insertSublayer:target.layer below:blackOverlay];
                        renderScene();
                    } else {
                        target.pendingError = failure;
                        emitScene(sceneGeneration, @"background-error", failure, 0, 0);
                    }
                    target.operation = nil;
                }
                if (raster) CGImageRelease(raster);
            });
        }
    }];
    weakOperation = operation; self.operation = operation; [sceneImageQueue addOperation:operation];
}
@end
static void stopScene(void) {
    cancelSceneFade();
    [sceneForeground teardown]; sceneForeground = nil;
    [sceneBackground teardown]; sceneBackground = nil;
    [sceneRetiring teardown]; sceneRetiring = nil;
    [sceneImage teardown]; sceneImage = nil;
    [sceneBackgroundImage teardown]; sceneBackgroundImage = nil;
    [sceneImageQueue cancelAllOperations];
    sceneStoppedPending = NO;
    blackout();
}
static void sceneEmergency(void) {
    uint64_t generation = sceneGeneration;
    atomic_store(&sceneEmergencyPending, true);
    stopScene(); hideSceneStage();
    emit(generation, @"escape", nil, 0, 0);
}
static void sceneCheckDevices(void) {
    BOOL lostDisplay = stageEnabled;
    if (stageEnabled) for (NSScreen *screen in NSScreen.screens)
        if ([displayID(screen) isEqual:stageDisplayID]) lostDisplay = NO;
    NSArray *outputs = audioDevices(); BOOL lostAudio = NO;
    for (SSScenePlayer *player in scenePlayers()) {
        if (!player.hasAudio || (player.background && !sceneBackgroundAudio) || !player.audioID.length) continue;
        BOOL found = NO;
        for (NSDictionary *output in outputs) if ([output[@"id"] isEqual:player.audioID]) found = YES;
        if (!found) lostAudio = YES;
    }
    if (lostDisplay || lostAudio) {
        stopScene(); hideSceneStage();
        emitScene(sceneGeneration, @"device-lost", lostDisplay ? @"Stage display disconnected" : @"Audio output disconnected", 0, 0);
    }
    emitScene(sceneGeneration, @"devices", nil, 0, 0);
}
static SSScenePlayer *newScenePlayer(uint64_t identifier, NSString *path, NSString *kind, NSString *audio, BOOL hasAudio, BOOL background) {
    SSScenePlayer *player = [[SSScenePlayer alloc] init];
    player.identifier = identifier; player.path = path; player.video = [kind isEqualToString:@"video"];
    player.audioID = audio; player.hasAudio = hasAudio; player.background = background;
    return player;
}
static void applyScene(uint64_t revision, uint64_t generation, uint64_t foregroundID, NSString *foregroundPath,
    NSString *foregroundKind, BOOL foregroundAudio, NSString *imagePath, NSString *backgroundPath,
    NSString *backgroundKind, BOOL backgroundAudio, NSString *audio, NSString *display, BOOL stage,
    double fade, BOOL hardStop) {
    if (revision != atomic_load(&requestedSceneRevision) || atomic_load(&shuttingDown) || atomic_load(&sceneEmergencyPending)) return;
    if (!sceneMode) { stopCurrent(); sceneMode = YES; }
    appliedSceneRevision = revision; sceneGeneration = generation;
    sceneBackgroundAudio = backgroundAudio; sceneFadeSeconds = isfinite(fade) ? MAX(0, MIN(30, fade)) : 0;
    sceneStoppedPending = !foregroundID || !foregroundPath.length;
    if (hardStop) {
        stopScene(); hideSceneStage(); emitScene(generation, @"stage", nil, 0, 0);
        emitScene(generation, @"stopped", nil, 0, 0); return;
    }
    if (stage && (!stageEnabled || ![stageDisplayID isEqual:display])) {
        if (!enableStage(display)) {
            stopScene(); hideSceneStage(); emitScene(generation, @"error", @"Selected stage display is unavailable", 0, 0); return;
        }
    } else if (!stage) hideSceneStage();
    BOOL sameForeground = sceneForeground && foregroundID == sceneForeground.identifier &&
        [foregroundPath isEqual:sceneForeground.path] && (!foregroundAudio || [audio isEqual:sceneForeground.audioID]);
    if (!sameForeground) {
        SSScenePlayer *old = sceneForeground; sceneForeground = nil;
        retireScenePlayer(old);
        if (foregroundID && foregroundPath.length && foregroundID != sceneEndedForegroundID) {
            sceneForeground = newScenePlayer(foregroundID, foregroundPath, foregroundKind, audio, foregroundAudio, NO);
            [sceneForeground prepare];
        }
    }
    BOOL backgroundVideo = [backgroundKind isEqualToString:@"video"] && backgroundPath.length;
    BOOL sameBackground = sceneBackground && backgroundVideo && [backgroundPath isEqual:sceneBackground.path] && (!backgroundAudio || [audio isEqual:sceneBackground.audioID]);
    if (!sameBackground) {
        SSScenePlayer *old = sceneBackground; sceneBackground = nil; retireScenePlayer(old);
        if (backgroundVideo) {
            sceneBackground = newScenePlayer(0, backgroundPath, backgroundKind, audio, NO, YES);
            [sceneBackground prepare];
        }
    }
    if (sceneBackground) {
        if (!stage) { sceneBackground.player.volume = 0; sceneBackground.fadeTo = 0; [sceneBackground.player pause]; }
        else if (sceneBackground.started) [sceneBackground.player play];
        else [sceneBackground ready];
    }
    if (![sceneImage.path isEqual:imagePath]) {
        [sceneImage teardown]; sceneImage = nil;
        if (imagePath.length) { sceneImage = [[SSSceneImage alloc] init]; sceneImage.path = imagePath; [sceneImage prepare]; }
    }
    NSString *backgroundImagePath = [backgroundKind isEqualToString:@"image"] ? backgroundPath : @"";
    if (![sceneBackgroundImage.path isEqual:backgroundImagePath]) {
        [sceneBackgroundImage teardown]; sceneBackgroundImage = nil;
        if (backgroundImagePath.length) {
            sceneBackgroundImage = [[SSSceneImage alloc] init]; sceneBackgroundImage.path = backgroundImagePath;
            sceneBackgroundImage.background = YES; [sceneBackgroundImage prepare];
        }
    }
    // Layers created while the stage was disabled attach when a display exists.
    for (SSScenePlayer *player in scenePlayers()) if (player.layer && !player.layer.superlayer && stageWindow) {
        player.layer.frame = stageWindow.contentView.bounds;
        [stageWindow.contentView.layer insertSublayer:player.layer below:blackOverlay];
    }
    for (SSSceneImage *image in @[sceneImage ?: (id)NSNull.null, sceneBackgroundImage ?: (id)NSNull.null])
        if ((id)image != NSNull.null && image.layer && !image.layer.superlayer && stageWindow) {
            image.layer.frame = stageWindow.contentView.bounds;
            [stageWindow.contentView.layer insertSublayer:image.layer below:blackOverlay];
        }
    renderScene();
    [sceneForeground ready]; [sceneBackground ready];
    mixScene();
    emitScene(generation, @"stage", nil, 0, 0);
    if (foregroundID && foregroundID == sceneEndedForegroundID) emitScene(foregroundID, @"ended", nil, 0, 0);
    if (sceneImage.pendingError.length) emitScene(generation, @"background-error", sceneImage.pendingError, 0, 0);
    if (sceneBackgroundImage.pendingError.length) emitScene(generation, @"background-error", sceneBackgroundImage.pendingError, 0, 0);
}
void ss_scene(const ss_scene_request *request) {
    if (!request || atomic_load(&shuttingDown)) return;
    @autoreleasepool {
        uint64_t revision = request->revision, generation = request->generation, foregroundID = request->foreground_id;
        NSString *(^copyUTF8)(const char *) = ^NSString *(const char *value) { return [NSString stringWithUTF8String:value ?: ""] ?: @""; };
        NSString *foreground = copyUTF8(request->foreground_path), *kind = copyUTF8(request->foreground_kind), *image = copyUTF8(request->image_path);
        NSString *background = copyUTF8(request->background_path), *backgroundKind = copyUTF8(request->background_kind);
        NSString *audio = copyUTF8(request->audio), *display = copyUTF8(request->display);
        BOOL hasAudio = request->foreground_has_audio, backgroundAudio = request->background_audio;
        BOOL stage = request->stage_enabled, hardStop = request->hard_stop;
        double fade = request->fade_seconds;
        pthread_mutex_lock(&commandMutex);
        if (revision <= atomic_load(&requestedSceneRevision)) { pthread_mutex_unlock(&commandMutex); return; }
        atomic_store(&requestedSceneRevision, revision);
        atomic_store(&sceneEmergencyPending, false);
        latestCommand = [^{ applyScene(revision, generation, foregroundID, foreground, kind, hasAudio, image,
            background, backgroundKind, backgroundAudio, audio, display, stage, fade, hardStop); } copy];
        BOOL wake = !commandScheduled; commandScheduled = YES;
        pthread_mutex_unlock(&commandMutex);
        if (wake) onMain(^{
            pthread_mutex_lock(&commandMutex);
            void (^next)(void) = latestCommand; latestCommand = nil; commandScheduled = NO;
            pthread_mutex_unlock(&commandMutex);
            if (next) next();
        });
    }
}

char *ss_init(void) {
    @autoreleasepool {
        if (!NSThread.isMainThread) return copyString(@"AppKit initialization must run on the process main thread");
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:desktopLaunch() ? NSApplicationActivationPolicyRegular : NSApplicationActivationPolicyAccessory];
        setupDesktop();
        [NSApp finishLaunching];
        events = [NSMutableArray array];
        keyObserver = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown handler:^NSEvent *(NSEvent *event) {
            if (event.keyCode == 53) {
                emergencyStop();
                // Panels still receive Escape so their usual Cancel action
                // works; the stage itself needs no further key handling.
                if (event.window == stageWindow) return nil;
            }
            return event;
        }];
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
static void cleanupNative(void) {
    static BOOL cleaned;
    if (cleaned) return;
    cleaned = YES;
    atomic_store(&desktopReady, false);
    atomic_store(&desktopAdminRequested, false);
    atomic_store(&desktopAdminShowScheduled, false);
    stopScene();
    disableStage();
    if (keyObserver) [NSEvent removeMonitor:keyObserver]; keyObserver = nil;
    [applicationDelegate.filePanel cancel:nil]; applicationDelegate.filePanel = nil;
    atomic_store(&desktopChooserScheduled, false);
    [applicationDelegate.adminWebView stopLoading];
    applicationDelegate.adminWebView.navigationDelegate = nil;
    applicationDelegate.adminWebView.UIDelegate = nil;
    applicationDelegate.adminWindow.delegate = nil;
    [applicationDelegate.adminWindow close];
    [applicationDelegate.adminWebView removeFromSuperview];
    applicationDelegate.adminWebView = nil;
    applicationDelegate.adminWindow = nil;
    applicationDelegate.adminErrorView = nil;
    applicationDelegate.adminErrorLabel = nil;
    applicationDelegate.adminShowPending = NO;
    for (SSFileAlert *controller in desktopAlerts.allObjects) [controller.alert.window close];
    desktopAlerts = nil;
    [NSNotificationCenter.defaultCenter removeObserver:screenObserver]; screenObserver = nil;
    for (NSNumber *selector in @[@(kAudioHardwarePropertyDevices), @(kAudioHardwarePropertyDefaultOutputDevice)]) {
        AudioObjectPropertyAddress address = {selector.unsignedIntValue, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
        AudioObjectRemovePropertyListenerBlock(kAudioObjectSystemObject, &address, dispatch_get_main_queue(), audioListener);
    }
    audioListener = nil;
    [stageWindow close]; stageWindow = nil; videoLayer = nil; blackOverlay = nil;
    if (applicationDelegate) {
        [NSStatusBar.systemStatusBar removeStatusItem:applicationDelegate.status];
        NSApp.delegate = nil; applicationDelegate = nil;
    }
    pthread_mutex_lock(&desktopFileMutex);
    desktopFileQueue = nil;
    desktopFileCounts = nil;
    desktopPendingFileCount = 0;
    pthread_mutex_unlock(&desktopFileMutex);
}
void ss_run(void) {
    @autoreleasepool {
        [NSApp run];
        cleanupNative();
    }
}
void ss_quit(void) {
    atomic_store(&shuttingDown, true); atomic_fetch_add(&currentGeneration, 1);
    onMain(^{
        if (applicationDelegate.terminationPending) {
            // NSTerminateLater enters a nested modal run loop, so it cannot
            // wait for ss_run to return. Go has already closed HTTP/storage
            // and native workers before requesting this final cleanup.
            cleanupNative();
            [NSApp replyToApplicationShouldTerminate:YES];
        }
        disableStage(); [NSApp stop:nil];
        NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];
        [NSApp postEvent:wake atStart:NO];
    });
}
void ss_desktop_admin(const char *url) {
    if (!desktopLaunch()) return;
    @autoreleasepool {
        NSString *value = [NSString stringWithUTF8String:url];
        onMain(^{
            NSURL *address = [NSURL URLWithString:value];
            if (!validAdminURL(address) || address.fragment.length || atomic_load(&shuttingDown)) return;
            applicationDelegate.adminURL = address;
            atomic_store(&desktopReady, true);
            for (NSMenuItem *item in applicationDelegate.openItems) item.enabled = YES;
            for (NSMenuItem *item in applicationDelegate.quitItems) item.enabled = YES;
            fputs("Smart Stage menu bar ready\n", stderr);
            if (applicationDelegate.quitPending) [applicationDelegate quit:nil];
            else if (applicationDelegate.adminShowPending) [applicationDelegate showAdminWindow];
        });
    }
}
int ss_desktop_has_admin_window(void) {
    // This is a capability query, not the asynchronous URL-ready state.
    return desktopLaunch() ? 1 : 0;
}
int ss_desktop_show_admin(void) {
    if (!desktopLaunch() || atomic_load(&shuttingDown)) return 0;
    if (atomic_exchange(&desktopAdminShowScheduled, true)) return 1;
    onMain(^{
        atomic_store(&desktopAdminShowScheduled, false);
        if (!applicationDelegate || applicationDelegate.quitStarted || atomic_load(&shuttingDown)) return;
        [applicationDelegate showAdminWindow];
    });
    return 1;
}
int ss_desktop_poll_admin_request(void) {
    return atomic_exchange(&desktopAdminRequested, false) ? 1 : 0;
}
int ss_desktop_can_choose_files(void) {
    return atomic_load(&desktopReady) && !atomic_load(&shuttingDown) ? 1 : 0;
}
int ss_desktop_choose_files(void) {
    if (!ss_desktop_can_choose_files()) return 0;
    // Coalesce queued work, while allowing a later click to bring an existing
    // panel forward after the operator has switched back to the browser.
    if (atomic_exchange(&desktopChooserScheduled, true)) return 1;
    onMain(^{
        atomic_store(&desktopChooserScheduled, false);
        if (!applicationDelegate || applicationDelegate.quitStarted || atomic_load(&shuttingDown)) {
            return;
        }
        [NSApp activateIgnoringOtherApps:YES];
        [applicationDelegate chooseMedia:nil];
    });
    return 1;
}
int ss_desktop_activate_browser(void) {
    if (!ss_desktop_can_choose_files()) return 0;
    onMain(^{
        if (atomic_load(&shuttingDown) || !applicationDelegate.adminURL) return;
        // Activating an existing browser does not ask it to create another tab.
        // The browser controls which window/tab is selected; no Automation
        // permission or browser-specific scripting is involved.
        NSURL *browserURL = [NSWorkspace.sharedWorkspace URLForApplicationToOpenURL:applicationDelegate.adminURL];
        NSString *identifier = browserURL ? [NSBundle bundleWithURL:browserURL].bundleIdentifier : nil;
        if (!identifier.length) return;
        for (NSRunningApplication *browser in [NSRunningApplication runningApplicationsWithBundleIdentifier:identifier]) {
            if ([browser activateWithOptions:NSApplicationActivateIgnoringOtherApps]) {
                fputs("Activated the running default browser without opening another Admin URL\n", stderr);
                break;
            }
        }
    });
    return 1;
}
void ss_desktop_error(const char *message) {
    if (!desktopLaunch()) return;
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        NSAlert *alert = [[NSAlert alloc] init];
        alert.messageText = desktopText(@"Smart Stage could not start");
        alert.informativeText = [NSString stringWithFormat:desktopText(@"%s\n\nDetails are saved in ~/Library/Logs/Smart Stage/smartstage.log."), message];
        [alert addButtonWithTitle:@"OK"];
        [alert addButtonWithTitle:desktopText(@"View Log")];
        if ([alert runModal] == NSAlertSecondButtonReturn) {
            const char *path = getenv("SMARTSTAGE_LOG_PATH");
            if (path) [NSWorkspace.sharedWorkspace openURL:[NSURL fileURLWithPath:[NSString stringWithUTF8String:path]]];
        }
    }
}
char *ss_desktop_poll_files(void) {
    @autoreleasepool {
        pthread_mutex_lock(&desktopFileMutex);
        NSData *data = desktopFileQueue.firstObject;
        char *result = data ? malloc(data.length + 1) : NULL;
        if (result) {
            memcpy(result, data.bytes, data.length); result[data.length] = 0;
            [desktopFileQueue removeObjectAtIndex:0];
        }
        pthread_mutex_unlock(&desktopFileMutex);
        return result;
    }
}
int ss_desktop_files_pending(void) {
    pthread_mutex_lock(&desktopFileMutex);
    BOOL pending = desktopPendingFileCount > 0;
    pthread_mutex_unlock(&desktopFileMutex);
    return pending ? 1 : 0;
}
void ss_desktop_files_result(uint64_t request_id, const char *message) {
    if (!desktopLaunch()) return;
    @autoreleasepool {
        NSString *failure = message ? [NSString stringWithUTF8String:message] : @"";
        onMain(^{
            pthread_mutex_lock(&desktopFileMutex);
            NSNumber *count = desktopFileCounts[@(request_id)];
            if (count) {
                desktopPendingFileCount -= count.unsignedIntegerValue;
                [desktopFileCounts removeObjectForKey:@(request_id)];
            }
            pthread_mutex_unlock(&desktopFileMutex);
            if (!count) return;
            if (failure.length) {
                fprintf(stderr, "Native media import failed: %s\n", failure.UTF8String);
                desktopFileAlert(failure);
            } else {
                fprintf(stderr, "Added %lu original media references without copying or starting playback\n", (unsigned long)count.unsignedIntegerValue);
                [applicationDelegate openAdmin:nil];
            }
        });
    }
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
        if ([@[@"png", @"jpg", @"jpeg", @"gif", @"webp", @"tif", @"tiff", @"bmp", @"heic", @"heif", @"avif", @"ico"] containsObject:file.pathExtension.lowercaseString]) {
            NSString *failure = nil;
            CGImageRef image = copySceneImage(file, &failure);
            if (!image) return errorJSON(failure);
            CGImageRelease(image);
            return json(@{@"kind": @"image", @"hasAudio": @NO, @"hasVideo": @NO, @"duration": @0});
        }
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
