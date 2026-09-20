// Finder launcher: keep the core executable's pairing keys and Ctrl+C in Terminal.
#import <Foundation/Foundation.h>
#include <stdio.h>
#include <unistd.h>

int main(void) {
    @autoreleasepool {
        NSString *command = [[NSBundle mainBundle] pathForResource:@"Start Smart Stage" ofType:@"command"];
        if (!command) {
            fputs("Smart Stage: missing bundled terminal command\n", stderr);
            return 1;
        }
        // Pass the path as one argument, including spaces, quotes and Unicode.
        // Launch Services opens a .command document; no AppleScript automation is used.
        execl("/usr/bin/open", "open", "-b", "com.apple.Terminal", [command fileSystemRepresentation], (char *)NULL);
        perror("Smart Stage: cannot open Terminal");
        return 1;
    }
}
