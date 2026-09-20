// Finder launcher: become the core process, without a terminal or helper daemon.
#import <AppKit/AppKit.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int fail(NSString *message) {
    fprintf(stderr, "Smart Stage: %s\n", message.UTF8String);
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"Smart Stage could not start";
    alert.informativeText = message;
    [alert runModal];
    return 1;
}

int main(int argc, char *argv[]) {
    @autoreleasepool {
        NSString *core = [[[NSBundle mainBundle] executablePath].stringByDeletingLastPathComponent stringByAppendingPathComponent:@"smartstage"];
        NSString *icon = [NSBundle.mainBundle pathForResource:@"smartstage" ofType:@"icns"];
        if (!icon) return fail(@"The application's icon is missing. Reinstall Smart Stage and try again.");
        NSString *directory = [NSHomeDirectory() stringByAppendingPathComponent:@"Library/Logs/Smart Stage"];
        NSError *error = nil;
        if (![NSFileManager.defaultManager createDirectoryAtPath:directory withIntermediateDirectories:YES
                attributes:@{NSFilePosixPermissions: @0700} error:&error])
            return fail([NSString stringWithFormat:@"Cannot create the log folder: %@", error.localizedDescription]);
        NSString *log = [directory stringByAppendingPathComponent:@"smartstage.log"];
        // Preserve the previous launch's diagnostics, but bound accumulated logs.
        NSDictionary *attributes = [NSFileManager.defaultManager attributesOfItemAtPath:log error:NULL];
        if ([attributes[NSFileSize] unsignedLongLongValue] > 10 * 1024 * 1024) {
            NSString *previous = [log stringByAppendingString:@".1"];
            [NSFileManager.defaultManager removeItemAtPath:previous error:NULL];
            [NSFileManager.defaultManager moveItemAtPath:log toPath:previous error:NULL];
        }
        int output = open(log.fileSystemRepresentation, O_WRONLY | O_APPEND | O_CREAT | O_NOFOLLOW, 0600);
        if (output < 0) return fail(@"Cannot open ~/Library/Logs/Smart Stage/smartstage.log for writing.");
        int input = open("/dev/null", O_RDONLY);
        if (input < 0 || dup2(input, STDIN_FILENO) < 0 || dup2(output, STDOUT_FILENO) < 0 || dup2(output, STDERR_FILENO) < 0)
            return fail(@"Cannot configure the application's log output.");
        if (input > STDERR_FILENO) close(input);
        if (output > STDERR_FILENO) close(output);
        if (setenv("SMARTSTAGE_APP_LAUNCH", "1", 1) != 0 ||
            setenv("SMARTSTAGE_APP_ICON", icon.fileSystemRepresentation, 1) != 0 ||
            setenv("SMARTSTAGE_LOG_PATH", log.fileSystemRepresentation, 1) != 0)
            return fail(@"Cannot configure the application environment.");
        if (chdir(NSHomeDirectory().fileSystemRepresentation) != 0)
            return fail(@"Cannot open your home folder.");
        fprintf(stderr, "\nStarting Smart Stage app — %s\n", NSDate.date.description.UTF8String);
        // Retain arguments from `open --args`; do not parse a shell command.
        char **arguments = calloc((size_t)argc + 1, sizeof(char *));
        if (!arguments) return fail(@"Not enough memory to start Smart Stage.");
        arguments[0] = (char *)core.fileSystemRepresentation;
        int count = 1;
        for (int i = 1; i < argc; ++i)
            if (strncmp(argv[i], "-psn_", 5) != 0) arguments[count++] = argv[i];
        execv(arguments[0], arguments);
        free(arguments);
        return fail(@"Cannot start the bundled Smart Stage executable. Reinstall the app and try again.");
    }
}
