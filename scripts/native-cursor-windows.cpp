// Native cursor regression observer. Production bridge owns the stage, media
// players and cursor. The separate operator window is test-only.
#include "../internal/platform/bridge_windows.cpp"
#include <cstdio>

namespace {
HWND operatorWindow = nullptr;
std::thread operatorThread;
std::mutex operatorMutex;
std::condition_variable operatorReady;
bool operatorInitialized = false;
std::string operatorError;
POINT savedPointer{}, probePoint{};
bool pointerSaved = false, initialized = false, bitmapVerified = false, messageVerified = false;
bool separateOperatorThreadVerified = false;
bool globalAvailable = false, globalPassed = false;
std::string unavailableReason, currentCheck, probeError, baseline;
std::string beforeMouseInput, afterMouseInput;
UINT mouseInputsInserted = 0;
DWORD mouseInputError = ERROR_SUCCESS;
std::vector<std::string> passedChecks;
ss_scene_request desired{};
uint64_t revision = 0;

void require(bool condition, const std::string &message) {
    if (!condition) throw message;
}
std::string windowClass(HWND window) {
    wchar_t name[256]{};
    if (window) GetClassNameW(window, name, 256);
    return utf8(name);
}
CURSORINFO cursorInfo() {
    CURSORINFO info{}; info.cbSize = sizeof(info);
    require(GetCursorInfo(&info) != FALSE, "GetCursorInfo failed: " + std::to_string(GetLastError()));
    return info;
}
std::string observation() {
    POINT point{};
    GetCursorPos(&point);
    HWND hit = WindowFromPoint(point);
    DWORD pid = 0;
    GetWindowThreadProcessId(hit, &pid);
    CURSORINFO cursor{}; cursor.cbSize = sizeof(cursor);
    BOOL observed = GetCursorInfo(&cursor);
    std::ostringstream result;
    result << "{\"x\":" << point.x << ",\"y\":" << point.y
        << ",\"hitClass\":" << quote(windowClass(hit))
        << ",\"hitRootClass\":" << quote(windowClass(GetAncestor(hit, GA_ROOT)))
        << ",\"hitPID\":" << pid
        << ",\"hitIsStage\":" << ((hit == stageWindow || IsChild(stageWindow, hit)) ? "true" : "false")
        << ",\"hitIsOperator\":" << (hit == operatorWindow ? "true" : "false")
        << ",\"cursorInfoAvailable\":" << (observed ? "true" : "false")
        << ",\"cursorFlags\":" << cursor.flags
        << ",\"cursorHandle\":" << reinterpret_cast<uintptr_t>(cursor.hCursor)
        << ",\"cursorIsOwnedTransparent\":" << (cursor.hCursor && cursor.hCursor == stageCursor ? "true" : "false") << '}';
    return result.str();
}
void pump() {
    MSG message{};
    for (int count = 0; count < 256 && PeekMessageW(&message, nullptr, 0, 0, PM_REMOVE); ++count) {
        require(message.message != WM_QUIT, "Unexpected application exit during cursor checks");
        TranslateMessage(&message);
        DispatchMessageW(&message);
    }
    char *raw;
    while ((raw = ss_poll())) {
        std::string event(raw);
        ss_free(raw);
        require(event.find("\"kind\":\"error\"") == std::string::npos &&
                event.find("\"kind\":\"background-error\"") == std::string::npos,
                "Native playback failed during cursor check: " + event);
    }
}
template<class Predicate> bool waitFor(Predicate predicate, DWORD milliseconds = 1500) {
    ULONGLONG deadline = GetTickCount64() + milliseconds;
    do {
        pump();
        if (predicate()) return true;
        Sleep(10);
    } while (GetTickCount64() < deadline);
    return false;
}
bool hitStage() {
    POINT point{};
    if (!GetCursorPos(&point)) return false;
    HWND hit = WindowFromPoint(point);
    return hit == stageWindow || IsChild(stageWindow, hit);
}
bool hiddenGlobally() {
    CURSORINFO info = cursorInfo();
    // The intentionally transparent shape is still a selected/shown cursor;
    // its all-AND/no-XOR bitmap proves that it renders no visible pixels.
    return hitStage() && info.hCursor == stageCursor;
}
bool arrowGlobally() {
    CURSORINFO info = cursorInfo();
    return (info.flags & CURSOR_SHOWING) && info.hCursor == LoadCursorW(nullptr, IDC_ARROW);
}
void passed(const char *name) { passedChecks.emplace_back(name); }
void verifyHidden(const char *name) {
    currentCheck = name;
    require(waitFor(hiddenGlobally), currentCheck + ": stage pointer did not become transparent");
    passed(name);
}
void verifyStationary() {
    POINT point{};
    require(GetCursorPos(&point) && point.x == probePoint.x && point.y == probePoint.y,
            "Cursor check unexpectedly moved the stationary pointer");
}
LRESULT CALLBACK operatorProc(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    if (message == WM_SETCURSOR) {
        SetCursor(LoadCursorW(nullptr, IDC_ARROW));
        return TRUE;
    }
    if (message == WM_CLOSE) { DestroyWindow(window); PostQuitMessage(0); return 0; }
    return DefWindowProcW(window, message, wparam, lparam);
}
void createOperatorWindow() {
    operatorThread = std::thread([] {
        HRESULT apartment = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
        WNDCLASSW cls{};
        cls.lpfnWndProc = operatorProc;
        cls.hInstance = GetModuleHandleW(nullptr);
        cls.hCursor = LoadCursorW(nullptr, IDC_ARROW);
        cls.lpszClassName = L"SmartStageCursorIndependentBaseline";
        cls.hbrBackground = static_cast<HBRUSH>(GetStockObject(WHITE_BRUSH));
        std::string error;
        if (FAILED(apartment)) error = failure(apartment, "Initialize independent operator COM");
        else if (!RegisterClassW(&cls)) error = "Register independent cursor baseline window";
        else {
            operatorWindow = CreateWindowExW(WS_EX_TOPMOST, cls.lpszClassName, L"Smart Stage cursor probe", WS_POPUP,
                probePoint.x - 150, probePoint.y - 100, 300, 200, nullptr, nullptr, cls.hInstance, nullptr);
            if (!operatorWindow) error = "Create independent cursor baseline window";
            else {
                ShowWindow(operatorWindow, SW_SHOW);
                SetWindowPos(operatorWindow, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW);
                SetForegroundWindow(operatorWindow);
            }
        }
        {
            std::lock_guard<std::mutex> lock(operatorMutex);
            operatorError = error;
            operatorInitialized = true;
        }
        operatorReady.notify_one();
        if (error.empty()) {
            MSG message{};
            while (GetMessageW(&message, nullptr, 0, 0) > 0) {
                TranslateMessage(&message); DispatchMessageW(&message);
            }
            if (IsWindow(operatorWindow)) DestroyWindow(operatorWindow);
        }
        if (SUCCEEDED(apartment)) CoUninitialize();
    });
    std::unique_lock<std::mutex> lock(operatorMutex);
    require(operatorReady.wait_for(lock, std::chrono::seconds(5), [] { return operatorInitialized; }),
            "Independent operator UI thread did not initialize");
    require(operatorError.empty(), operatorError);
}
void verifyTransparentBitmap() {
    currentCheck = "transparentCursorBitmap";
    require(stageCursor != nullptr, "Production stage has no owned cursor");
    ICONINFO icon{};
    require(GetIconInfo(stageCursor, &icon) != FALSE, "Read production cursor bitmap");
    BITMAP bitmap{};
    bool transparent = !icon.fIcon && !icon.hbmColor && icon.hbmMask &&
        GetObjectW(icon.hbmMask, sizeof(bitmap), &bitmap) == sizeof(bitmap) &&
        bitmap.bmWidth == 32 && bitmap.bmHeight == 64 && bitmap.bmBitsPixel == 1;
    if (icon.hbmMask) DeleteObject(icon.hbmMask);
    if (icon.hbmColor) DeleteObject(icon.hbmColor);
    require(transparent, "Owned cursor is not a monochrome 32x32 cursor");
    // Observe actual rasterization rather than assuming the driver-dependent
    // row layout returned by GetBitmapBits for the combined AND/XOR mask.
    BITMAPINFO format{};
    format.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    format.bmiHeader.biWidth = 32;
    format.bmiHeader.biHeight = -32;
    format.bmiHeader.biPlanes = 1;
    format.bmiHeader.biBitCount = 32;
    format.bmiHeader.biCompression = BI_RGB;
    void *pixels = nullptr;
    HBITMAP target = CreateDIBSection(nullptr, &format, DIB_RGB_COLORS, &pixels, nullptr, 0);
    HDC dc = CreateCompatibleDC(nullptr);
    if (!target || !dc || !pixels) {
        if (target) DeleteObject(target);
        if (dc) DeleteDC(dc);
        throw std::string("Create cursor rasterization observation surface");
    }
    std::vector<DWORD> expected(32 * 32);
    for (int y = 0; y < 32; ++y)
        for (int x = 0; x < 32; ++x)
            expected[y * 32 + x] = (x + y) % 2 ? 0x00f13480 : 0x0012db65;
    memcpy(pixels, expected.data(), expected.size() * sizeof(DWORD));
    HGDIOBJ previous = SelectObject(dc, target);
    bool rendered = DrawIconEx(dc, 0, 0, stageCursor, 32, 32, 0, nullptr, DI_NORMAL) != FALSE;
    GdiFlush();
    for (size_t index = 0; rendered && index < expected.size(); ++index)
        rendered = (static_cast<DWORD *>(pixels)[index] & 0x00ffffff) == expected[index];
    SelectObject(dc, previous);
    DeleteDC(dc);
    DeleteObject(target);
    require(rendered, "Production cursor changed visible pixels when drawn on a colored checkerboard");
    bitmapVerified = true;
}
void verifyMessages() {
    currentCheck = "stageCursorMessageHandling";
    stageEnabled = true;
    SetCursor(LoadCursorW(nullptr, IDC_ARROW));
    SendMessageW(stageWindow, WM_SETCURSOR, reinterpret_cast<WPARAM>(stageWindow), MAKELPARAM(HTCLIENT, WM_MOUSEMOVE));
    require(GetCursor() == stageCursor, "Stage WM_SETCURSOR did not select the owned transparent cursor");
    stageEnabled = false;
    SetCursor(LoadCursorW(nullptr, IDC_ARROW));
    SendMessageW(controlWindow, WM_SETCURSOR, reinterpret_cast<WPARAM>(controlWindow), MAKELPARAM(HTCLIENT, WM_MOUSEMOVE));
    require(GetCursor() != stageCursor, "Non-stage window inherited the stage cursor");
    messageVerified = true;
}
void applyScene() {
    desired.revision = ++revision;
    ss_scene(&desired);
}
void moveTo(POINT point) {
    require(SetCursorPos(point.x, point.y) != FALSE, "Move actual cursor for native observation");
}
void checkGlobal(const std::string &display, const std::string &video, const std::string &image) {
    auto list = monitors();
    auto monitor = std::find_if(list.begin(), list.end(), [&](const Monitor &item) { return item.id == display; });
    require(monitor != list.end(), "Selected monitor is unavailable for cursor probe");
    const RECT area = monitor->rect;
    probePoint = {(area.left + area.right) / 2, (area.top + area.bottom) / 2};
    // Admin runs its own STA and input queue. Mirror that boundary in the
    // independent operator baseline so GetCursor cannot masquerade as global
    // cursor visibility merely because both windows share the stage thread.
    createOperatorWindow();
    require(GetWindowThreadProcessId(operatorWindow, nullptr) != GetCurrentThreadId(),
            "Operator baseline unexpectedly shares the stage input thread");
    separateOperatorThreadVerified = true;
    currentCheck = "independentDesktopCursorBaseline";
    if (!pointerSaved || !SetCursorPos(probePoint.x + 1, probePoint.y)) {
        baseline = observation();
        unavailableReason = "Independent native baseline could not access the current input desktop pointer";
        return;
    }
    pump();
    if (!SetCursorPos(probePoint.x, probePoint.y)) {
        baseline = observation();
        unavailableReason = "Independent native baseline could not position the current input desktop pointer";
        return;
    }
    // Hosted desktops may start in CURSOR_SUPPRESSED (touch/pen input mode).
    // SetCursorPos alone changes coordinates without delivering mouse input.
    // Drive a bounded, reversible pair of actual mouse movements before the
    // independent baseline; never inject input during stationary-stage tests.
    beforeMouseInput = observation();
    INPUT inputs[2]{};
    for (INPUT &input : inputs) {
        input.type = INPUT_MOUSE;
        input.mi.dwFlags = MOUSEEVENTF_MOVE | MOUSEEVENTF_MOVE_NOCOALESCE;
    }
    inputs[0].mi.dx = 1;
    inputs[1].mi.dx = -1;
    SetLastError(ERROR_SUCCESS);
    mouseInputsInserted = SendInput(2, inputs, sizeof(INPUT));
    mouseInputError = mouseInputsInserted == 2 ? ERROR_SUCCESS : GetLastError();
    waitFor([] {
        CURSORINFO info{}; info.cbSize = sizeof(info);
        return GetCursorInfo(&info) && (info.flags & CURSOR_SHOWING);
    }, 300);
    if (!SetCursorPos(probePoint.x, probePoint.y)) {
        baseline = observation();
        afterMouseInput = baseline;
        unavailableReason = "Independent native baseline could not position the pointer after mouse input";
        return;
    }
    bool reachable = waitFor([] {
        CURSORINFO info{}; info.cbSize = sizeof(info);
        POINT position{};
        return WindowFromPoint(probePoint) == operatorWindow && GetCursorInfo(&info) &&
            (info.flags & CURSOR_SHOWING) && info.hCursor == LoadCursorW(nullptr, IDC_ARROW) &&
            GetCursorPos(&position) && position.x == probePoint.x && position.y == probePoint.y;
    }, 2500);
    baseline = observation();
    afterMouseInput = baseline;
    if (!reachable) {
        unavailableReason = "Independent visible topmost test window could not own a globally visible arrow cursor; desktop/input capability unavailable";
        return;
    }
    globalAvailable = true;
    passed("independentDesktopCursorBaseline");
    require(enableStage(display), "Enable production stage");
    verifyHidden("stationaryStageActivation");
    verifyStationary();
    // Simulate a renderer/application selecting an ordinary visible cursor.
    // No WM_SETCURSOR or mouse move follows; production's timer must repair it.
    SetCursor(LoadCursorW(nullptr, IDC_ARROW));
    require(arrowGlobally(), "Visible reset control did not change the global cursor");
    verifyHidden("stationaryRendererResetRecovery");
    verifyStationary();
    moveTo({probePoint.x + 25, probePoint.y + 25});
    verifyHidden("movingOverBlankStage");
    moveTo(probePoint);
    verifyHidden("returnToStageCenter");
    SetWindowPos(operatorWindow, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_SHOWWINDOW);
    currentCheck = "operatorWindowVisibleWhileStageEnabled";
    require(waitFor([] { return stageEnabled && WindowFromPoint(probePoint) == operatorWindow && arrowGlobally(); }),
            "Stage cursor suppression affected the independent operator window");
    passed("operatorWindowVisibleWhileStageEnabled");
    SetWindowPos(stageWindow, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
    verifyHidden("stationaryReturnFromOperatorWindow");
    disableStage();
    currentCheck = "stationaryStageOffRestoresCursor";
    require(waitFor(arrowGlobally), "Stage off did not restore the global pointer");
    verifyStationary();
    passed("stationaryStageOffRestoresCursor");
    for (int index = 0; index < 3; ++index) {
        require(enableStage(display), "Repeated stage activation failed");
        require(waitFor(hiddenGlobally), "Repeated stage activation did not hide cursor");
        disableStage();
        require(waitFor(arrowGlobally), "Repeated stage off did not restore cursor");
    }
    passed("repeatedStageTogglesRestoreCursor");
    desired.display = display.c_str();
    desired.audio = "";
    desired.generation = 1;
    desired.background_path = video.c_str();
    desired.background_kind = "video";
    desired.stage_enabled = 1;
    applyScene();
    currentCheck = "backgroundVideoCursor";
    require(waitFor([] { return sceneBackground && sceneBackground->playing && IsWindowVisible(backgroundWindow); }, 12000),
            "Background video did not start for cursor observation");
    moveTo({probePoint.x + 20, probePoint.y});
    verifyHidden("backgroundVideoCursor");
    require(WindowFromPoint(cursorInfo().ptScreenPos) == backgroundWindow || IsChild(backgroundWindow, WindowFromPoint(cursorInfo().ptScreenPos)),
            "Actual pointer hit was not the background video window");
    desired.foreground_id = desired.generation = 10;
    desired.foreground_path = video.c_str();
    desired.foreground_kind = "video";
    applyScene();
    currentCheck = "foregroundVideoCursor";
    require(waitFor([] { return sceneForeground && sceneForeground->playing && IsWindowVisible(sceneForeground->target); }, 12000),
            "Foreground video did not start for cursor observation");
    moveTo(probePoint);
    verifyHidden("foregroundVideoCursor");
    HWND foregroundTarget = sceneForeground->target;
    require(WindowFromPoint(probePoint) == foregroundTarget || IsChild(foregroundTarget, WindowFromPoint(probePoint)),
            "Actual pointer hit was not the foreground video window");
    desired.image_path = image.c_str();
    applyScene();
    require(waitFor([] { return sceneImage && visualImage() == sceneImage.get(); }, 12000), "Image overlay did not become visible");
    verifyHidden("imageOverlayCursor");
    desired.foreground_id = 0;
    desired.generation = 11;
    desired.foreground_path = "";
    desired.foreground_kind = "";
    desired.image_path = "";
    desired.background_path = image.c_str();
    desired.background_kind = "image";
    applyScene();
    require(waitFor([] { return sceneBackground && visualImage() == sceneBackground.get(); }, 12000), "Image background did not become visible");
    verifyHidden("imageBackgroundCursor");
    desired.stage_enabled = 0;
    applyScene();
    require(waitFor([] { return !stageEnabled && arrowGlobally(); }), "Scene stage off did not restore cursor");
    passed("sceneStageOffRestoresCursor");
    desired.stage_enabled = 1;
    applyScene();
    require(waitFor(hiddenGlobally), "Stage reactivation before Escape did not hide cursor");
    SendMessageW(stageWindow, WM_KEYDOWN, VK_ESCAPE, 0);
    require(waitFor([] { return !stageEnabled && arrowGlobally(); }), "Native Escape did not restore cursor");
    passed("nativeEscapeRestoresCursor");
    applyScene();
    require(waitFor(hiddenGlobally), "Stage reactivation before Quit did not hide cursor");
    globalPassed = true;
}
}

int wmain(int argc, wchar_t **argv) {
    if (argc != 4) return 2;
    pointerSaved = GetCursorPos(&savedPointer) != FALSE;
    try {
        char *error = ss_init();
        if (error) { std::string message(error); ss_free(error); throw message; }
        initialized = true;
        verifyTransparentBitmap();
        verifyMessages();
        checkGlobal(utf8(argv[1]), utf8(argv[2]), utf8(argv[3]));
    } catch (const std::string &error) { probeError = error; }
    std::string finalObservation = observation();
    HCURSOR ownedCursor = stageCursor;
    if (initialized) { ss_quit(); ss_run(); }
    if (globalPassed && probeError.empty()) {
        CURSORINFO info{}; info.cbSize = sizeof(info);
        if (!GetCursorInfo(&info) || info.hCursor == ownedCursor || !(info.flags & CURSOR_SHOWING))
            probeError = "Application shutdown did not restore a visible global cursor";
        else passed("applicationQuitRestoresCursor");
    }
    if (operatorWindow) PostMessageW(operatorWindow, WM_CLOSE, 0, 0);
    if (operatorThread.joinable()) operatorThread.join();
    if (pointerSaved) SetCursorPos(savedPointer.x, savedPointer.y);
    std::ostringstream result;
    result << "{\"status\":" << quote(!probeError.empty() ? "failed" : globalPassed ? "passed" : "unavailable")
           << ",\"transparentCursorBitmapVerified\":" << (bitmapVerified ? "true" : "false")
           << ",\"stageCursorMessageHandling\":" << (messageVerified ? "true" : "false")
           << ",\"operatorUsesSeparateInputThread\":" << (separateOperatorThreadVerified ? "true" : "false")
           << ",\"globalCursor\":{\"available\":" << (globalAvailable ? "true" : "false")
           << ",\"status\":" << quote(!probeError.empty() ? "failed" : globalPassed ? "passed" : "unavailable")
           << ",\"unavailableReason\":" << quote(unavailableReason)
           << ",\"baselineMouseInput\":{\"requested\":2,\"inserted\":" << mouseInputsInserted
           << ",\"win32Error\":" << mouseInputError
           << ",\"before\":" << (beforeMouseInput.empty() ? "null" : beforeMouseInput)
           << ",\"after\":" << (afterMouseInput.empty() ? "null" : afterMouseInput) << '}'
           << ",\"baseline\":" << (baseline.empty() ? "null" : baseline)
           << ",\"lastObservation\":" << finalObservation << ",\"passedChecks\":[";
    for (size_t index = 0; index < passedChecks.size(); ++index) { if (index) result << ','; result << quote(passedChecks[index]); }
    result << "]},\"physicalMouseVerified\":false";
    if (!probeError.empty()) result << ",\"failedCheck\":" << quote(currentCheck) << ",\"error\":" << quote(probeError);
    result << '}';
    puts(result.str().c_str());
    if (!probeError.empty()) fprintf(stderr, "%s\n", result.str().c_str());
    return probeError.empty() ? 0 : 1;
}
