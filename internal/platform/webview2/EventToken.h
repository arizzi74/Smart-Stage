// Case-compatible SDK include for cross compilation on Linux.
// Keep this directory out of -I search paths: on case-insensitive Windows
// filesystems the system include must resolve to MinGW, not back to this file.
#include <eventtoken.h>
