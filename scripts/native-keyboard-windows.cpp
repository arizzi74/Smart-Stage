// Test-only driver: send an ordinary Win32 key message to production windows.
#include <windows.h>
#include <chrono>
#include <cstdio>
#include <cwchar>
#include <cstring>
#include <thread>
#include "../internal/platform/bridge.h"

static HWND visibleStage;
static HWND appControl;
static BOOL CALLBACK findStage(HWND window, LPARAM) {
    DWORD pid = 0;
    GetWindowThreadProcessId(window, &pid);
    wchar_t name[64];
    GetClassNameW(window, name, 64);
    if (pid == GetCurrentProcessId() && wcscmp(name, L"SmartStageNative") == 0) {
        if (IsWindowVisible(window)) visibleStage = window;
        wchar_t title[64];
        GetWindowTextW(window, title, 64);
        if (wcscmp(title, L"Smart Stage") == 0) appControl = window;
    }
    return TRUE;
}
int main(int argc, char **argv) {
    if (argc != 3) return 2;
    char *error = ss_init();
    if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
    bool passed = false;
    std::thread driver([&] {
        bool sent = false;
        for (int polls = 0; polls < 500; ++polls) {
            char *raw;
            while ((raw = ss_poll())) {
                bool stopped = strstr(raw, "\"kind\":\"stopped\"") && strstr(raw, "\"stageEnabled\":true");
                bool escaped = strstr(raw, "\"kind\":\"escape\"") != nullptr;
                bool disabled = strstr(raw, "\"stageEnabled\":false") != nullptr;
                ss_free(raw);
                if (stopped && !sent) {
                    EnumWindows(findStage, 0);
                    if (!visibleStage) { fprintf(stderr, "No visible native stage\n"); ss_quit(); return; }
                    HWND target = strcmp(argv[2], "stage") == 0 ? visibleStage : appControl;
                    sent = target && PostMessageW(target, WM_KEYDOWN, VK_ESCAPE, 1);
                } else if (escaped) {
                    passed = sent && disabled && !IsWindowVisible(visibleStage);
                    ss_quit(); return;
                }
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(20));
        }
        fprintf(stderr, "No Escape result before timeout\n"); ss_quit();
    });
    ss_stage(1, argv[1], 1);
    ss_run(); driver.join();
    if (!passed) return 4;
    printf("{\"nativeEscapeEvent\":true,\"stageDisabled\":true,\"stageWindowHidden\":true,\"target\":\"%s\"}\n", argv[2]);
    return 0;
}
