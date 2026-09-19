//go:build windows && cgo

#include "bridge.h"
#include <windows.h>
#include <initguid.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <mferror.h>
#include <evr.h>
#include <mmdeviceapi.h>
#include <functiondiscoverykeys_devpkey.h>
#include <propvarutil.h>
#include <shellscalingapi.h>
#include <algorithm>
#include <atomic>
#include <condition_variable>
#include <deque>
#include <memory>
#include <mutex>
#include <optional>
#include <sstream>
#include <string>
#include <thread>
#include <vector>

namespace {
template<class T> struct Com {
    T *p = nullptr;
    Com() = default;
    Com(const Com&) = delete;
    Com& operator=(const Com&) = delete;
    ~Com() { if (p) p->Release(); }
    T **out() { if (p) p->Release(); p = nullptr; return &p; }
    T *operator->() const { return p; }
    explicit operator bool() const { return p != nullptr; }
};
struct Apartment {
    HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    ~Apartment() { if (SUCCEEDED(hr)) CoUninitialize(); }
};
std::wstring wide(const std::string &s) {
    int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s.data(), (int)s.size(), nullptr, 0);
    std::wstring w(n, L'\0');
    if (n) MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s.data(), (int)s.size(), w.data(), n);
    return w;
}
std::string utf8(const wchar_t *s) {
    if (!s) return "";
    int n = WideCharToMultiByte(CP_UTF8, 0, s, -1, nullptr, 0, nullptr, nullptr);
    std::string out(n, '\0');
    if (n) WideCharToMultiByte(CP_UTF8, 0, s, -1, out.data(), n, nullptr, nullptr);
    if (!out.empty()) out.pop_back();
    return out;
}
std::string quote(const std::string &s) {
    const char *hex = "0123456789abcdef";
    std::string out = "\"";
    for (unsigned char c : s) {
        if (c == '\\' || c == '"') { out += '\\'; out += c; }
        else if (c < 32) { out += "\\u00"; out += hex[c >> 4]; out += hex[c & 15]; }
        else out += c;
    }
    return out + "\"";
}
char *copy(const std::string &s) {
    auto *p = (char*)malloc(s.size()+1);
    if (p) memcpy(p, s.c_str(), s.size()+1);
    return p;
}
std::string failure(HRESULT hr, const char *action) {
    wchar_t *message = nullptr;
    FormatMessageW(FORMAT_MESSAGE_ALLOCATE_BUFFER | FORMAT_MESSAGE_FROM_SYSTEM | FORMAT_MESSAGE_IGNORE_INSERTS,
                   nullptr, hr, 0, (LPWSTR)&message, 0, nullptr);
    std::ostringstream s; s << action << " (0x" << std::hex << (uint32_t)hr << ")";
    if (message) { s << ": " << utf8(message); LocalFree(message); }
    return s.str();
}
void check(HRESULT hr, const char *action) { if (FAILED(hr)) throw failure(hr, action); }

constexpr UINT commandMessage = WM_APP+1, readyMessage = WM_APP+2, deviceMessage = WM_APP+3, quitMessage = WM_APP+4;
HWND controlWindow = nullptr, stageWindow = nullptr, videoWindow = nullptr;
std::atomic<uint64_t> generation{0};
std::atomic<bool> quitting{false};
std::atomic<bool> cleanupQuitting{false};
bool stageEnabled = false;
std::string stageDisplay;
std::mutex eventMutex;
std::deque<std::string> eventQueue;

void emit(uint64_t g, const char *kind, const std::string &message = "", double pos = 0, double duration = 0) {
    std::ostringstream s;
    s << "{\"generation\":" << g << ",\"kind\":" << quote(kind)
      << ",\"message\":" << quote(message) << ",\"position\":" << pos
      << ",\"duration\":" << duration << ",\"stageEnabled\":" << (stageEnabled ? "true" : "false") << "}";
    std::lock_guard<std::mutex> lock(eventMutex);
    if (eventQueue.size() >= 256) eventQueue.pop_front();
    eventQueue.push_back(s.str());
}

struct Monitor { std::string id, name; RECT rect; bool primary = false, mirrored = false; };
std::vector<Monitor> monitors() {
    std::vector<Monitor> result;
    EnumDisplayMonitors(nullptr, nullptr, [](HMONITOR monitor, HDC, LPRECT, LPARAM data)->BOOL {
        auto &list = *(std::vector<Monitor>*)data;
        MONITORINFOEXW info{}; info.cbSize = sizeof(info);
        if (!GetMonitorInfoW(monitor, &info)) return TRUE;
        Monitor m; m.rect = info.rcMonitor; m.primary = (info.dwFlags & MONITORINFOF_PRIMARY) != 0;
        m.name = utf8(info.szDevice);
        UINT32 nPaths = 0, nModes = 0;
        if (GetDisplayConfigBufferSizes(QDC_ONLY_ACTIVE_PATHS, &nPaths, &nModes) == ERROR_SUCCESS) {
            std::vector<DISPLAYCONFIG_PATH_INFO> paths(nPaths);
            std::vector<DISPLAYCONFIG_MODE_INFO> modes(nModes);
            if (QueryDisplayConfig(QDC_ONLY_ACTIVE_PATHS, &nPaths, paths.data(), &nModes, modes.data(), nullptr) == ERROR_SUCCESS) {
                unsigned matches = 0;
                for (UINT32 i=0; i<nPaths; ++i) {
                    auto &p = paths[i];
                    DISPLAYCONFIG_SOURCE_DEVICE_NAME src{};
                    src.header = {DISPLAYCONFIG_DEVICE_INFO_GET_SOURCE_NAME, sizeof(src), p.sourceInfo.adapterId, p.sourceInfo.id};
                    if (DisplayConfigGetDeviceInfo(&src.header) != ERROR_SUCCESS || wcscmp(src.viewGdiDeviceName, info.szDevice) != 0) continue;
                    DISPLAYCONFIG_TARGET_DEVICE_NAME target{};
                    target.header = {DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_NAME, sizeof(target), p.targetInfo.adapterId, p.targetInfo.id};
                    if (DisplayConfigGetDeviceInfo(&target.header) != ERROR_SUCCESS) continue;
                    std::string id = utf8(target.monitorDevicePath);
                    if (!id.empty() && (m.id.empty() || id < m.id)) {
                        m.id = id;
                        if (target.monitorFriendlyDeviceName[0]) m.name = utf8(target.monitorFriendlyDeviceName);
                    }
                    ++matches;
                }
                m.mirrored = matches > 1;
            }
        }
        // Do not pretend a transient monitor index is a persistent identity.
        if (!m.id.empty()) list.push_back(m);
        return TRUE;
    }, (LPARAM)&result);
    return result;
}
struct Audio { std::string id, name; bool isDefault = false; };
std::vector<Audio> audioDevices() {
    Com<IMMDeviceEnumerator> enumerator;
    check(CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_INPROC_SERVER,
                          __uuidof(IMMDeviceEnumerator), (void**)enumerator.out()), "Enumerate audio endpoints");
    std::string defaultID;
    Com<IMMDevice> def;
    if (SUCCEEDED(enumerator->GetDefaultAudioEndpoint(eRender, eMultimedia, def.out()))) {
        LPWSTR id = nullptr;
        if (SUCCEEDED(def->GetId(&id))) { defaultID = utf8(id); CoTaskMemFree(id); }
    }
    Com<IMMDeviceCollection> devices;
    check(enumerator->EnumAudioEndpoints(eRender, DEVICE_STATE_ACTIVE, devices.out()), "List render endpoints");
    UINT count = 0; check(devices->GetCount(&count), "Count audio endpoints");
    std::vector<Audio> result;
    for (UINT i=0; i<count; ++i) {
        Com<IMMDevice> d; Com<IPropertyStore> properties;
        if (FAILED(devices->Item(i, d.out()))) continue;
        LPWSTR id = nullptr; if (FAILED(d->GetId(&id))) continue;
        Audio a; a.id = utf8(id); CoTaskMemFree(id); a.name = a.id; a.isDefault = a.id == defaultID;
        if (SUCCEEDED(d->OpenPropertyStore(STGM_READ, properties.out()))) {
            PROPVARIANT v; PropVariantInit(&v);
            if (SUCCEEDED(properties->GetValue(PKEY_Device_FriendlyName, &v)) && v.vt == VT_LPWSTR) a.name = utf8(v.pwszVal);
            PropVariantClear(&v);
        }
        result.push_back(a);
    }
    return result;
}

struct Request {
    enum Kind { Play, Stop, Stage } kind;
    uint64_t gen;
    std::string path, audio, display;
    bool video = false, enabled = false;
    HWND target = nullptr;
};
struct Playback {
    uint64_t gen = 0;
    std::string audio;
    bool video = false, playing = false;
    double duration = 0;
    Com<IMFMediaSource> source;
    Com<IMFMediaSession> session;
    Com<IMFVideoDisplayControl> display;
    Com<IMFSimpleAudioVolume> volume;
    Com<IMFPresentationClock> clock;
    std::string error;
    ~Playback() {
        if (session) session->Shutdown();
        if (source) source->Shutdown();
    }
};
std::unique_ptr<Playback> active;
std::mutex workerMutex, cleanupMutex;
std::condition_variable workerCV, cleanupCV;
std::optional<Request> pending;
std::deque<std::unique_ptr<Playback>> retired;
std::thread loader, cleaner;

void retire(std::unique_ptr<Playback> p) {
    if (!p) return;
    { std::lock_guard<std::mutex> lock(cleanupMutex); retired.push_back(std::move(p)); }
    cleanupCV.notify_one();
}
void black() {
    if (videoWindow) ShowWindow(videoWindow, SW_HIDE);
    if (stageWindow && stageEnabled) RedrawWindow(stageWindow, nullptr, nullptr, RDW_INVALIDATE | RDW_ERASE | RDW_UPDATENOW);
}
void stopCurrent() {
    black();
    if (active) {
        if (active->volume) active->volume->SetMute(TRUE);
        if (active->session) active->session->Stop();
        retire(std::move(active));
    }
}
bool enableStage(const std::string &id) {
    auto list = monitors();
    auto found = std::find_if(list.begin(), list.end(), [&](const Monitor &m) { return m.id == id; });
    if (found == list.end()) return false;
    const RECT &r = found->rect;
    SetWindowPos(stageWindow, HWND_TOPMOST, r.left, r.top, r.right-r.left, r.bottom-r.top, SWP_NOACTIVATE);
    SetWindowPos(videoWindow, nullptr, 0, 0, r.right-r.left, r.bottom-r.top, SWP_NOZORDER | SWP_NOACTIVATE);
    stageDisplay = id; stageEnabled = true;
    black(); ShowWindow(stageWindow, SW_SHOWNOACTIVATE);
    SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED);
    return true;
}
void disableStage() {
    stopCurrent(); stageEnabled = false; stageDisplay.clear();
    ShowWindow(stageWindow, SW_HIDE);
    SetThreadExecutionState(ES_CONTINUOUS);
}

void addBranch(Playback &p, IMFTopology *topology, IMFPresentationDescriptor *pd,
               IMFStreamDescriptor *sd, IMFActivate *sink) {
    Com<IMFTopologyNode> src, dst;
    check(MFCreateTopologyNode(MF_TOPOLOGY_SOURCESTREAM_NODE, src.out()), "Create source node");
    check(src->SetUnknown(MF_TOPONODE_SOURCE, p.source.p), "Set topology source");
    check(src->SetUnknown(MF_TOPONODE_PRESENTATION_DESCRIPTOR, pd), "Set presentation descriptor");
    check(src->SetUnknown(MF_TOPONODE_STREAM_DESCRIPTOR, sd), "Set stream descriptor");
    check(MFCreateTopologyNode(MF_TOPOLOGY_OUTPUT_NODE, dst.out()), "Create renderer node");
    check(dst->SetObject(sink), "Set renderer activation");
    check(dst->SetUINT32(MF_TOPONODE_STREAMID, 0), "Set renderer stream");
    check(dst->SetUINT32(MF_TOPONODE_NOSHUTDOWN_ON_REMOVE, FALSE), "Set renderer lifetime");
    check(topology->AddNode(src.p), "Add source node");
    check(topology->AddNode(dst.p), "Add renderer node");
    check(src->ConnectOutput(0, dst.p, 0), "Connect source to renderer");
}
std::unique_ptr<Playback> prepare(const Request &r) {
    auto p = std::make_unique<Playback>(); p->gen = r.gen; p->audio = r.audio;
    try {
        Com<IMFSourceResolver> resolver; Com<IUnknown> object;
        check(MFCreateSourceResolver(resolver.out()), "Create media resolver");
        MF_OBJECT_TYPE type;
        check(resolver->CreateObjectFromURL(wide(r.path).c_str(), MF_RESOLUTION_MEDIASOURCE | MF_RESOLUTION_READ,
                                           nullptr, &type, object.out()), "Open local media");
        check(object->QueryInterface(__uuidof(IMFMediaSource), (void**)p->source.out()), "Get media source");
        if (r.gen != generation.load()) return p;
        Com<IMFPresentationDescriptor> pd; Com<IMFTopology> topology;
        check(p->source->CreatePresentationDescriptor(pd.out()), "Read media tracks");
        UINT64 duration = 0;
        if (SUCCEEDED(pd->GetUINT64(MF_PD_DURATION, &duration))) p->duration = duration / 10000000.0;
        check(MFCreateTopology(topology.out()), "Create playback topology");
        DWORD count = 0; check(pd->GetStreamDescriptorCount(&count), "Count tracks");
        bool audio = false;
        for (DWORD i=0; i<count; ++i) {
            BOOL selected; Com<IMFStreamDescriptor> sd; Com<IMFMediaTypeHandler> handler; GUID major;
            check(pd->GetStreamDescriptorByIndex(i, &selected, sd.out()), "Read track descriptor");
            check(sd->GetMediaTypeHandler(handler.out()), "Read track type");
            check(handler->GetMajorType(&major), "Read track major type");
            Com<IMFActivate> sink;
            if (major == MFMediaType_Audio && !audio) {
                if (r.audio.empty()) throw std::string("Select an available audio output");
                check(MFCreateAudioRendererActivate(sink.out()), "Create audio renderer");
                check(sink->SetString(MF_AUDIO_RENDERER_ATTRIBUTE_ENDPOINT_ID, wide(r.audio).c_str()), "Select audio endpoint");
                audio = true;
            } else if (major == MFMediaType_Video && !p->video) {
                if (!r.video || !r.target) throw std::string("Video requires a selected stage display");
                check(MFCreateVideoRendererActivate(r.target, sink.out()), "Create video renderer");
                p->video = true;
            } else { pd->DeselectStream(i); continue; }
            check(pd->SelectStream(i), "Select media track");
            addBranch(*p, topology.p, pd.p, sd.p, sink.p);
        }
        if (!audio && !p->video) throw std::string("No supported audio or video track");
        if (r.gen != generation.load()) return p;
        check(MFCreateMediaSession(nullptr, p->session.out()), "Create media session");
        check(p->session->SetTopology(0, topology.p), "Prepare native decoders");
    } catch (const std::string &e) { p->error = e; }
    return p;
}
void loadLoop() {
    Apartment apartment;
    for (;;) {
        Request r;
        { std::unique_lock<std::mutex> lock(workerMutex);
          workerCV.wait(lock, [] { return quitting.load() || pending.has_value(); });
          if (quitting.load()) return;
          r = *pending; pending.reset(); }
        if (r.gen != generation.load()) continue;
        auto p = prepare(r);
        if (quitting.load() || r.gen != generation.load()) { retire(std::move(p)); continue; }
        Playback *raw = p.release();
        if (!PostMessageW(controlWindow, readyMessage, 0, (LPARAM)raw)) retire(std::unique_ptr<Playback>(raw));
    }
}
void cleanupLoop() {
    Apartment apartment;
    for (;;) {
        std::unique_ptr<Playback> p;
        { std::unique_lock<std::mutex> lock(cleanupMutex);
          cleanupCV.wait(lock, [] { return cleanupQuitting.load() || !retired.empty(); });
          if (retired.empty() && cleanupQuitting.load()) return;
          p = std::move(retired.front()); retired.pop_front(); }
        p.reset(); // Shutdown can block; never do it on the UI/STOP path.
    }
}

void tick() {
    if (!active || active->gen != generation.load()) return;
    const uint64_t g = active->gen;
    for (int i=0; i<16 && active; ++i) {
        Com<IMFMediaEvent> event;
        HRESULT hr = active->session->GetEvent(MF_EVENT_FLAG_NO_WAIT, event.out());
        if (hr == MF_E_NO_EVENTS_AVAILABLE) break;
        if (FAILED(hr)) { stopCurrent(); emit(g, "error", failure(hr, "Read playback event")); return; }
        MediaEventType type; HRESULT status;
        event->GetType(&type); event->GetStatus(&status);
        if (FAILED(status)) { stopCurrent(); emit(g, "error", failure(status, "Native playback failed")); return; }
        if (type == MESessionTopologyStatus && MFGetAttributeUINT32(event.p, MF_EVENT_TOPOLOGY_STATUS, 0) == MF_TOPOSTATUS_READY) {
            if (g != generation.load()) return;
            if (active->video) {
                hr = MFGetService(active->session.p, MR_VIDEO_RENDER_SERVICE, __uuidof(IMFVideoDisplayControl), (void**)active->display.out());
                if (SUCCEEDED(hr)) {
                    RECT rect; GetClientRect(videoWindow, &rect);
                    active->display->SetAspectRatioMode(MFVideoARMode_PreservePicture);
                    active->display->SetBorderColor(RGB(0,0,0));
                    active->display->SetVideoPosition(nullptr, &rect);
                } else { stopCurrent(); emit(g, "error", failure(hr, "Configure stage renderer")); return; }
            }
            MFGetService(active->session.p, MR_POLICY_VOLUME_SERVICE, __uuidof(IMFSimpleAudioVolume), (void**)active->volume.out());
            Com<IMFClock> clock;
            if (SUCCEEDED(active->session->GetClock(clock.out()))) clock->QueryInterface(__uuidof(IMFPresentationClock), (void**)active->clock.out());
            PROPVARIANT start; PropVariantInit(&start); start.vt = VT_I8; start.hVal.QuadPart = 0;
            if (g != generation.load()) return;
            hr = active->session->Start(&GUID_NULL, &start); PropVariantClear(&start);
            if (FAILED(hr)) { stopCurrent(); emit(g, "error", failure(hr, "Start media session")); return; }
        } else if (type == MESessionStarted) {
            if (g != generation.load()) return;
            active->playing = true;
            if (active->video && stageEnabled) ShowWindow(videoWindow, SW_SHOWNOACTIVATE);
            emit(g, "playing", "", 0, active->duration);
        } else if (type == MEEndOfPresentation || type == MESessionEnded) {
            stopCurrent(); emit(g, "ended"); return;
        }
    }
    static ULONGLONG lastProgress = 0;
    if (active && active->playing && active->clock && GetTickCount64()-lastProgress >= 250) {
        MFTIME time = 0;
        if (SUCCEEDED(active->clock->GetTime(&time))) emit(g, "progress", "", time/10000000.0, active->duration);
        lastProgress = GetTickCount64();
    }
}
void checkDevices() {
    auto displays = monitors();
    bool lostDisplay = stageEnabled && std::none_of(displays.begin(), displays.end(), [](const Monitor &m) { return m.id == stageDisplay; });
    bool lostAudio = false;
    if (active && !active->audio.empty()) {
        try { auto devices = audioDevices(); lostAudio = std::none_of(devices.begin(), devices.end(), [](const Audio &a) { return a.id == active->audio; }); }
        catch (...) { lostAudio = true; }
    }
    if (lostDisplay || lostAudio) {
        uint64_t g = generation.fetch_add(1); stopCurrent();
        if (lostDisplay) disableStage();
        emit(g, "device-lost", lostDisplay ? "Stage display disconnected; select and enable it again" : "Audio output disconnected; playback stopped");
    }
    emit(generation.load(), "devices");
}
class DeviceNotifications final : public IMMNotificationClient {
    std::atomic<ULONG> refs{1};
public:
    HRESULT STDMETHODCALLTYPE QueryInterface(REFIID id, void **out) override {
        if (!out) return E_POINTER;
        *out = nullptr;
        if (id != __uuidof(IUnknown) && id != __uuidof(IMMNotificationClient)) return E_NOINTERFACE;
        *out = this; AddRef(); return S_OK;
    }
    ULONG STDMETHODCALLTYPE AddRef() override { return ++refs; }
    ULONG STDMETHODCALLTYPE Release() override { auto n = --refs; if (!n) delete this; return n; }
    HRESULT changed() { PostMessageW(controlWindow, deviceMessage, 0, 0); return S_OK; }
    HRESULT STDMETHODCALLTYPE OnDeviceStateChanged(LPCWSTR, DWORD) override { return changed(); }
    HRESULT STDMETHODCALLTYPE OnDeviceAdded(LPCWSTR) override { return changed(); }
    HRESULT STDMETHODCALLTYPE OnDeviceRemoved(LPCWSTR) override { return changed(); }
    HRESULT STDMETHODCALLTYPE OnDefaultDeviceChanged(EDataFlow, ERole, LPCWSTR) override { return changed(); }
    HRESULT STDMETHODCALLTYPE OnPropertyValueChanged(LPCWSTR, const PROPERTYKEY) override { return changed(); }
};
Com<IMMDeviceEnumerator> notificationEnumerator;
DeviceNotifications *notifications = nullptr;

LRESULT CALLBACK windowProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
    if (msg == commandMessage) {
        std::unique_ptr<Request> r((Request*)lp);
        if (r->gen != generation.load()) return 0;
        stopCurrent();
        if (r->kind == Request::Stop) { emit(r->gen, "stopped"); return 0; }
        if (r->kind == Request::Stage) {
            if (r->enabled && !enableStage(r->display)) { emit(r->gen, "error", "Selected stage display is unavailable"); return 0; }
            if (!r->enabled) disableStage();
            emit(r->gen, "stopped"); return 0;
        }
        if (r->video && !enableStage(r->display)) { emit(r->gen, "error", "Select an available stage display before playing video"); return 0; }
        { std::lock_guard<std::mutex> lock(cleanupMutex);
          if (retired.size() >= 8) { emit(r->gen, "error", "Native cleanup is busy; stop and retry"); return 0; } }
        r->target = videoWindow;
        { std::lock_guard<std::mutex> lock(workerMutex); pending = *r; }
        workerCV.notify_one(); return 0;
    }
    if (msg == readyMessage) {
        std::unique_ptr<Playback> p((Playback*)lp);
        if (p->gen != generation.load() || quitting.load()) { retire(std::move(p)); return 0; }
        if (!p->error.empty()) { emit(p->gen, "error", p->error); retire(std::move(p)); return 0; }
        active = std::move(p); return 0;
    }
    if (msg == WM_TIMER) { tick(); return 0; }
    if (msg == deviceMessage || msg == WM_DISPLAYCHANGE || msg == WM_DEVICECHANGE) { checkDevices(); return 0; }
    if (msg == WM_KEYDOWN && wp == VK_ESCAPE) {
        uint64_t g = generation.fetch_add(1); stopCurrent(); emit(g, "escape"); return 0;
    }
    if (msg == WM_CLOSE && hwnd != controlWindow) { uint64_t g = generation.fetch_add(1); stopCurrent(); emit(g, "escape"); return 0; }
    if (msg == WM_SETCURSOR && hwnd != controlWindow) { SetCursor(nullptr); return TRUE; }
    if (msg == WM_ERASEBKGND) { RECT r; GetClientRect(hwnd,&r); FillRect((HDC)wp,&r,(HBRUSH)GetStockObject(BLACK_BRUSH)); return TRUE; }
    if (msg == WM_PAINT) {
        PAINTSTRUCT ps; HDC dc = BeginPaint(hwnd, &ps);
        FillRect(dc, &ps.rcPaint, (HBRUSH)GetStockObject(BLACK_BRUSH)); EndPaint(hwnd, &ps);
        if (hwnd == videoWindow && active && active->display && active->gen == generation.load()) active->display->RepaintVideo();
        return 0;
    }
    if (msg == quitMessage) { stopCurrent(); disableStage(); PostQuitMessage(0); return 0; }
    return DefWindowProcW(hwnd, msg, wp, lp);
}
void submit(Request *r) {
    generation.store(r->gen);
    if (!PostMessageW(controlWindow, commandMessage, 0, (LPARAM)r)) delete r;
}
} // namespace

extern "C" char *ss_init() {
    HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    if (FAILED(hr)) return copy(failure(hr, "Initialize COM MTA"));
    hr = MFStartup(MF_VERSION, MFSTARTUP_FULL);
    if (FAILED(hr)) { CoUninitialize(); return copy(failure(hr, "Media Foundation unavailable; check Windows multimedia components")); }
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    WNDCLASSW cls{}; cls.lpfnWndProc = windowProc; cls.hInstance = GetModuleHandleW(nullptr);
    cls.lpszClassName = L"SmartStageNative"; cls.hbrBackground = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (!RegisterClassW(&cls)) return copy("Cannot register native stage window");
    // Hidden top-level control window receives broadcast display-change events.
    controlWindow = CreateWindowExW(0, cls.lpszClassName, L"Smart Stage", 0, 0,0,0,0, nullptr,nullptr,cls.hInstance,nullptr);
    stageWindow = CreateWindowExW(WS_EX_TOPMOST | WS_EX_TOOLWINDOW, cls.lpszClassName, L"", WS_POPUP | WS_CLIPCHILDREN, 0,0,1,1,nullptr,nullptr,cls.hInstance,nullptr);
    videoWindow = CreateWindowExW(0, cls.lpszClassName, L"", WS_CHILD, 0,0,1,1,stageWindow,nullptr,cls.hInstance,nullptr);
    if (!controlWindow || !stageWindow || !videoWindow) return copy("Cannot create native stage windows in this desktop session");
    if (SUCCEEDED(CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_INPROC_SERVER,
                                  __uuidof(IMMDeviceEnumerator), (void**)notificationEnumerator.out()))) {
        notifications = new DeviceNotifications();
        notificationEnumerator->RegisterEndpointNotificationCallback(notifications);
    }
    SetTimer(controlWindow, 1, 20, nullptr);
    loader = std::thread(loadLoop); cleaner = std::thread(cleanupLoop);
    return nullptr;
}
extern "C" void ss_run() {
    MSG msg;
    while (GetMessageW(&msg, nullptr, 0, 0) > 0) { TranslateMessage(&msg); DispatchMessageW(&msg); }
    if (notifications) { notificationEnumerator->UnregisterEndpointNotificationCallback(notifications); notifications->Release(); notifications = nullptr; }
    quitting.store(true); workerCV.notify_all();
    if (loader.joinable()) loader.join();
    // Drain completed loads/commands before releasing HWND targets.
    while (PeekMessageW(&msg, controlWindow, commandMessage, readyMessage, PM_REMOVE)) {
        if (msg.message == readyMessage) retire(std::unique_ptr<Playback>((Playback*)msg.lParam));
        else delete (Request*)msg.lParam;
    }
    cleanupQuitting.store(true); cleanupCV.notify_all(); if (cleaner.joinable()) cleaner.join();
    DestroyWindow(stageWindow); DestroyWindow(controlWindow);
    if (notificationEnumerator.p) { notificationEnumerator.p->Release(); notificationEnumerator.p = nullptr; }
    MFShutdown(); CoUninitialize();
}
extern "C" void ss_quit() { generation.fetch_add(1); PostMessageW(controlWindow, quitMessage, 0, 0); }
extern "C" void ss_free(char *p) { free(p); }
extern "C" char *ss_poll() {
    std::lock_guard<std::mutex> lock(eventMutex);
    if (eventQueue.empty()) return nullptr;
    auto s = std::move(eventQueue.front()); eventQueue.pop_front(); return copy(s);
}
extern "C" char *ss_devices() {
    Apartment apartment;
    try {
        check(apartment.hr, "Initialize enumeration COM");
        std::ostringstream s; s << "{\"audio\":["; bool first = true;
        for (const auto &a : audioDevices()) {
            if (!first) s << ',';
            first = false;
            s << "{\"id\":" << quote(a.id) << ",\"name\":" << quote(a.name) << ",\"default\":" << (a.isDefault?"true":"false") << "}";
        }
        s << "],\"displays\":["; first = true;
        for (const auto &m : monitors()) {
            if (!first) s << ',';
            first = false;
            s << "{\"id\":" << quote(m.id) << ",\"name\":" << quote(m.name)
              << ",\"x\":" << m.rect.left << ",\"y\":" << m.rect.top
              << ",\"width\":" << m.rect.right-m.rect.left << ",\"height\":" << m.rect.bottom-m.rect.top
              << ",\"primary\":" << (m.primary?"true":"false") << ",\"mirrored\":" << (m.mirrored?"true":"false") << "}";
        }
        s << "]}"; return copy(s.str());
    } catch (const std::string &e) { return copy("{\"error\":"+quote(e)+"}"); }
}
extern "C" char *ss_inspect(const char *path) {
    Apartment apartment;
    try {
        check(apartment.hr, "Initialize inspection COM");
        Com<IMFSourceReader> reader; Com<IMFAttributes> attrs;
        check(MFCreateAttributes(attrs.out(), 1), "Create reader attributes");
        check(attrs->SetUINT32(MF_SOURCE_READER_ENABLE_VIDEO_PROCESSING, TRUE), "Enable native video decoding");
        check(MFCreateSourceReaderFromURL(wide(path).c_str(), attrs.p, reader.out()), "Inspect local media");
        check(reader->SetStreamSelection(MF_SOURCE_READER_ALL_STREAMS, FALSE), "Isolate inspection tracks");
        bool audio = false, video = false;
        for (DWORD i=0; i<128; ++i) {
            Com<IMFMediaType> original;
            HRESULT hr = reader->GetNativeMediaType(i, 0, original.out());
            if (hr == MF_E_INVALIDSTREAMNUMBER) break;
            check(hr, "Read native track format"); GUID major;
            check(original->GetGUID(MF_MT_MAJOR_TYPE, &major), "Read track metadata");
            if (major != MFMediaType_Audio && major != MFMediaType_Video) continue;
            if ((major == MFMediaType_Audio && audio) || (major == MFMediaType_Video && video)) continue;
            Com<IMFMediaType> decoded; check(MFCreateMediaType(decoded.out()), "Create decoder format");
            check(decoded->SetGUID(MF_MT_MAJOR_TYPE, major), "Set decoder major type");
            check(decoded->SetGUID(MF_MT_SUBTYPE, major == MFMediaType_Audio ? MFAudioFormat_PCM : MFVideoFormat_RGB32), "Set decoded sample format");
            check(reader->SetCurrentMediaType(i, nullptr, decoded.p), "Prepare native decoder");
            check(reader->SetStreamSelection(i, TRUE), "Select inspection track");
            // A deselected track is not buffered while another track is read.
            // Rewind before each independent decoder probe in a multi-track file.
            PROPVARIANT origin; PropVariantInit(&origin); origin.vt = VT_I8; origin.hVal.QuadPart = 0;
            HRESULT seek = reader->SetCurrentPosition(GUID_NULL, origin); PropVariantClear(&origin);
            check(seek, "Rewind native inspection track");
            bool gotSample = false;
            for (int attempts=0; attempts<64 && !gotSample; ++attempts) {
                Com<IMFSample> sample; DWORD flags = 0; LONGLONG timestamp = 0;
                check(reader->ReadSample(i, 0, nullptr, &flags, &timestamp, sample.out()), "Decode first native sample");
                if (flags & MF_SOURCE_READERF_ERROR) throw std::string("Native decoder reported a sample error");
                gotSample = (bool)sample;
                if (flags & MF_SOURCE_READERF_ENDOFSTREAM) break;
            }
            if (!gotSample) throw std::string("No decodable sample in media track");
            reader->SetStreamSelection(i, FALSE);
            if (major == MFMediaType_Audio) audio = true; else video = true;
        }
        if (!audio && !video) throw std::string("No supported audio or video track");
        double duration = 0; PROPVARIANT v; PropVariantInit(&v);
        if (SUCCEEDED(reader->GetPresentationAttribute(MF_SOURCE_READER_MEDIASOURCE, MF_PD_DURATION, &v)) && v.vt == VT_UI8) duration = v.uhVal.QuadPart/10000000.0;
        PropVariantClear(&v);
        std::ostringstream s; s << "{\"kind\":" << quote(video?"video":"audio") << ",\"hasAudio\":" << (audio?"true":"false")
          << ",\"hasVideo\":" << (video?"true":"false") << ",\"duration\":" << duration << "}";
        return copy(s.str());
    } catch (const std::string &e) { return copy("{\"error\":"+quote(e)+"}"); }
}
extern "C" void ss_start(uint64_t g, const char *path, const char *audio, const char *display, int video) {
    auto *r = new Request{Request::Play, g, path, audio, display}; r->video = video != 0; submit(r);
}
extern "C" void ss_stop(uint64_t g) { submit(new Request{Request::Stop, g, "", "", ""}); }
extern "C" void ss_stage(uint64_t g, const char *display, int enabled) {
    auto *r = new Request{Request::Stage, g, "", "", display}; r->enabled = enabled != 0; submit(r);
}
