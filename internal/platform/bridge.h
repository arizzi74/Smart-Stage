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
typedef struct {
    uint64_t revision, generation, foreground_id;
    const char *foreground_path, *foreground_kind, *image_path;
    const char *background_path, *background_kind, *audio, *display;
    int foreground_has_audio, background_audio, stage_enabled, hard_stop;
    double fade_seconds;
} ss_scene_request;
/* Complete, latest-revision-wins scene. All pointed-to strings are copied.
 * Foreground events use foreground_id as generation; stopped uses generation.
 * stage/background-error events include sceneRevision. */
void ss_scene(const ss_scene_request *request);
#if defined(__APPLE__) || defined(_WIN32)
void ss_desktop_admin(const char *url);
void ss_desktop_error(const char *message);
char *ss_desktop_poll_files(void);
int ss_desktop_files_pending(void);
void ss_desktop_files_result(uint64_t request_id, const char *message);
int ss_desktop_poll_admin_request(void);
int ss_desktop_can_choose_files(void);
int ss_desktop_choose_files(void);
int ss_desktop_activate_browser(void);
int ss_desktop_has_admin_window(void);
int ss_desktop_show_admin(void);
#endif
#ifdef _WIN32
void ss_desktop_identity(const char *key);
void ss_desktop_log_path(const char *path);
int ss_desktop_reopen(const char *key);
int ss_desktop_poll_quit_request(void);
int ss_desktop_poll_emergency_request(void);
void ss_windows_desktop_shutdown(void);
#endif
#ifdef __cplusplus
}
#endif
#endif
