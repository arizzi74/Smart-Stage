// Inspect the process registered with LaunchServices, not just its Info.plist.
// This external observer needs neither Accessibility permission nor app hooks.
#import <AppKit/AppKit.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static NSData *renderIcon(NSImage *image) {
    if (!image) return nil;
    const NSInteger side = 128;
    NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc]
        initWithBitmapDataPlanes:NULL pixelsWide:side pixelsHigh:side
        bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO
        colorSpaceName:NSDeviceRGBColorSpace bitmapFormat:0
        bytesPerRow:side * 4 bitsPerPixel:32];
    if (!bitmap) return nil;
    memset(bitmap.bitmapData, 0, (size_t)(side * side * 4));
    NSGraphicsContext *context = [NSGraphicsContext graphicsContextWithBitmapImageRep:bitmap];
    if (!context) return nil;
    [NSGraphicsContext saveGraphicsState];
    NSGraphicsContext.currentContext = context;
    context.imageInterpolation = NSImageInterpolationHigh;
    [image drawInRect:NSMakeRect(0, 0, side, side) fromRect:NSZeroRect
           operation:NSCompositingOperationCopy fraction:1.0
      respectFlipped:NO hints:nil];
    [NSGraphicsContext restoreGraphicsState];
    return [NSData dataWithBytes:bitmap.bitmapData length:(NSUInteger)(side * side * 4)];
}

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc != 3) {
            fputs("Usage: inspect-macos-app PID SOURCE_ICNS\n", stderr);
            return 2;
        }
        NSRunningApplication *app = [NSRunningApplication
            runningApplicationWithProcessIdentifier:(pid_t)strtol(argv[1], NULL, 10)];
        if (!app || app.terminated) {
            fputs("The application is not registered as running\n", stderr);
            return 1;
        }
        NSImage *source = [[NSImage alloc] initWithContentsOfFile:
                          [NSString stringWithUTF8String:argv[2]]];
        NSData *actual = renderIcon(app.icon);
        NSData *expected = renderIcon(source);
        NSDictionary *result = @{
            @"processIdentifier": @(app.processIdentifier),
            @"activationPolicy": @(app.activationPolicy),
            @"bundleIdentifier": app.bundleIdentifier ?: @"",
            @"bundlePath": app.bundleURL.path ?: @"",
            @"localizedName": app.localizedName ?: @"",
            @"finishedLaunching": @(app.finishedLaunching),
            @"runtimeIconRGBA": [actual base64EncodedStringWithOptions:0] ?: @"",
            @"sourceIconRGBA": [expected base64EncodedStringWithOptions:0] ?: @""
        };
        NSError *error = nil;
        NSData *json = [NSJSONSerialization dataWithJSONObject:result options:0 error:&error];
        if (!json) {
            fprintf(stderr, "Cannot encode running application observations: %s\n", error.description.UTF8String);
            return 1;
        }
        fwrite(json.bytes, 1, json.length, stdout);
        fputc('\n', stdout);
        return 0;
    }
}
