#ifndef SMARTSTAGE_BRIDGE_H
#define SMARTSTAGE_BRIDGE_H
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
/* All strings are UTF-8. Input strings are copied before the call returns.
 * Returned strings are malloc-owned, released only using ss_free.
 * No Go pointer/callback is retained. Events are polled, not called into Go.
 * init/run own the process main OS thread; all other entries are thread-safe.
 * Exactly one instance exists per process. Stop invalidates generations inline.
 */
char *ss_init(void);
void ss_run(void);
void ss_quit(void);
void ss_free(char *value);
char *ss_devices(void);
char *ss_inspect(const char *path);
char *ss_poll(void);
void ss_start(uint64_t generation, const char *path, const char *audio,
              const char *display, int video);
void ss_stop(uint64_t generation);
void ss_stage(uint64_t generation, const char *display, int enabled);
#ifdef __cplusplus
}
#endif
#endif
