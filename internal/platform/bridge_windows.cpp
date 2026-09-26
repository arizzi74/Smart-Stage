//go:build windows && cgo

#include "bridge.h"
#include <windows.h>
#include <initguid.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <mferror.h>
#include <evr.h>
#include <wincodec.h>
#include <cmath>
#include <array>
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

void openLocalMedia(const std::string &path, IMFMediaSource **source) {
    Com<IMFSourceResolver> resolver; Com<IUnknown> object;
    check(MFCreateSourceResolver(resolver.out()), "Create media resolver");
    MF_OBJECT_TYPE type = MF_OBJECT_INVALID;
    const auto filename = wide(path);
    const DWORD flags = MF_RESOLUTION_MEDIASOURCE | MF_RESOLUTION_READ;
    HRESULT hr = resolver->CreateObjectFromURL(filename.c_str(), flags, nullptr, &type, object.out());
    if (hr == MF_E_UNSUPPORTED_BYTESTREAM_TYPE) {
        // A supported file can have a misleading extension (for example WAV
        // content named .mp3). Ask Windows to try its other registered handlers
        // before rejecting it. Both inspection and playback use this policy.
        hr = resolver->CreateObjectFromURL(filename.c_str(),
            flags | MF_RESOLUTION_CONTENT_DOES_NOT_HAVE_TO_MATCH_EXTENSION_OR_MIME_TYPE,
            nullptr, &type, object.out());
    }
    check(hr, "Open local media");
    check(object->QueryInterface(__uuidof(IMFMediaSource), (void**)source), "Get media source");
}

constexpr UINT commandMessage = WM_APP+1, readyMessage = WM_APP+2, deviceMessage = WM_APP+3, quitMessage = WM_APP+4, sceneMessage = WM_APP+5;
HWND controlWindow = nullptr, stageWindow = nullptr, videoWindow = nullptr;
HWND sceneVideoWindows[3] = {nullptr, nullptr, nullptr};
HWND backgroundWindows[2] = {nullptr, nullptr}, backgroundWindow = nullptr, transitionWindow = nullptr;
std::atomic<uint64_t> generation{0};
std::atomic<bool> quitting{false};
std::atomic<bool> cleanupQuitting{false};
bool stageEnabled = false;
std::string stageDisplay;
HCURSOR stageCursor = nullptr;
bool stageCursorSelected = false;
std::mutex eventMutex;
std::deque<std::string> eventQueue;

HCURSOR createStageCursor() {
    // Some virtual display drivers reject monochrome pointer shapes. Supply a
    // real 32-bit color shape instead, with zero RGB/alpha and a transparent
    // AND mask so both alpha-aware and mask-based rendering leave no pixels.
    // Cover the full 64x64 virtio cursor resource, including pixels left by a
    // larger previous pointer on drivers that only update the supplied area.
    constexpr int size = 64;
    BITMAPINFO format{};
    format.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    format.bmiHeader.biWidth = size;
    format.bmiHeader.biHeight = -size;
    format.bmiHeader.biPlanes = 1;
    format.bmiHeader.biBitCount = 32;
    format.bmiHeader.biCompression = BI_RGB;
    void *pixels = nullptr;
    HBITMAP color = CreateDIBSection(nullptr, &format, DIB_RGB_COLORS, &pixels, nullptr, 0);
    if (!color) return nullptr;
    memset(pixels, 0, size * size * sizeof(DWORD));
    std::array<BYTE, size * size / 8> maskBits;
    maskBits.fill(0xff);
    HBITMAP mask = CreateBitmap(size, size, 1, 1, maskBits.data());
    HCURSOR cursor = nullptr;
    if (mask) {
        ICONINFO icon{};
        icon.fIcon = FALSE;
        icon.hbmColor = color;
        icon.hbmMask = mask;
        cursor = reinterpret_cast<HCURSOR>(CreateIconIndirect(&icon));
    }
    DWORD error = cursor ? ERROR_SUCCESS : GetLastError();
    // CreateIconIndirect copies the bitmaps; the cursor owns its own image.
    if (mask) DeleteObject(mask);
    DeleteObject(color);
    if (!cursor) SetLastError(error);
    return cursor;
}
bool stageSurface(HWND window) {
    return stageWindow && (window == stageWindow || IsChild(stageWindow, window));
}
void selectStageCursor() {
    // Use an actual transparent shape, so cursor-shape consumers (including
    // remote/virtual desktops) receive a blank image rather than a null handle.
    SetCursor(stageCursor);
    stageCursorSelected = true;
}
void syncStageCursor() {
    if (!stageEnabled && !stageCursorSelected) return;
    POINT position;
    if (!GetCursorPos(&position)) return;
    HWND target = WindowFromPoint(position);
    if (stageEnabled && IsWindowVisible(stageWindow) && stageSurface(target)) {
        selectStageCursor();
    } else if (stageCursorSelected) {
        CURSORINFO cursor{}; cursor.cbSize = sizeof(cursor);
        // Restore only our own cursor. Another window may already have chosen
        // its text/link/resize cursor, which must not be replaced by this timer.
        if (GetCursorInfo(&cursor) && cursor.hCursor == stageCursor)
            SetCursor(LoadCursorW(nullptr, IDC_ARROW));
        stageCursorSelected = false;
    }
}
void cancelVisualTransition();
void hideStageWindow() {
    cancelVisualTransition();
    stageEnabled = false; stageDisplay.clear();
    ShowWindow(stageWindow, SW_HIDE);
    syncStageCursor();
    SetThreadExecutionState(ES_CONTINUOUS);
}

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
    int sceneRole = 0;
    uint64_t token = 0;
    bool image = false, muteTrack = false;
};
std::mutex commandMutex;
std::unique_ptr<Request> latestCommand;
bool commandScheduled = false;
struct Playback {
    uint64_t gen = 0;
    std::string audio, path;
    int sceneRole = 0;
    uint64_t token = 0;
    HWND target = nullptr;
    UINT imageWidth = 0, imageHeight = 0;
    std::vector<BYTE> pixels;
    float gain = 0, rampFrom = 0, rampTo = 0;
    ULONGLONG rampStarted = 0;
    double rampSeconds = 0;
    bool looping = false, loopSeeking = false, loopPending = false, topologyReady = false, startRequested = false, nativeStarted = false;
    bool video = false, playing = false, hasAudio = false;
    double duration = 0;
    Com<IMFMediaSource> source;
    Com<IMFMediaSession> session;
    Com<IMFVideoDisplayControl> display;
    Com<IMFAudioStreamVolume> streamVolume;
    std::vector<float> silence;
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
std::deque<Request> pending;
std::array<std::atomic<uint64_t>, 4> sceneTokens{};
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
std::string stopCurrent() {
    black();
    std::string error;
    if (active) {
        // Session-wide mute affects subsequent renderers in the process's
        // default audio session. Silence only this retiring stream instead.
        if (active->streamVolume && !active->silence.empty()) {
            HRESULT hr = active->streamVolume->SetAllVolumes((UINT32)active->silence.size(), active->silence.data());
            if (FAILED(hr)) error = failure(hr, "Silence audio stream");
        }
        if (active->session) {
            HRESULT hr = active->session->Stop();
            if (FAILED(hr) && hr != MF_E_INVALIDREQUEST && active->playing && error.empty())
                error = failure(hr, "Stop media session");
        }
        retire(std::move(active));
    }
    return error;
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
    syncStageCursor();
    SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED);
    return true;
}
void disableStage() {
    stopCurrent(); hideStageWindow();
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
void decodeImage(Playback &p, const std::string &path) {
    Com<IWICImagingFactory> factory;
    check(CoCreateInstance(CLSID_WICImagingFactory, nullptr, CLSCTX_INPROC_SERVER,
                          __uuidof(IWICImagingFactory), (void**)factory.out()), "Create native image decoder");
    Com<IWICBitmapDecoder> decoder; Com<IWICBitmapFrameDecode> frame;
    check(factory->CreateDecoderFromFilename(wide(path).c_str(), nullptr, GENERIC_READ,
                                             WICDecodeMetadataCacheOnDemand, decoder.out()), "Open image");
    check(decoder->GetFrame(0, frame.out()), "Read image frame");
    UINT width = 0, height = 0;
    check(frame->GetSize(&width, &height), "Read image dimensions");
    if (!width || !height || width > 32768 || height > 32768 || (uint64_t)width*height > 100000000)
        throw std::string("Image dimensions exceed the supported limit");
    double scale = std::min(1.0, 4096.0 / std::max(width, height));
    p.imageWidth = std::max(1U, (UINT)std::lround(width*scale));
    p.imageHeight = std::max(1U, (UINT)std::lround(height*scale));
    Com<IWICBitmapScaler> scaler;
    check(factory->CreateBitmapScaler(scaler.out()), "Create image scaler");
    check(scaler->Initialize(frame.p, p.imageWidth, p.imageHeight, WICBitmapInterpolationModeFant), "Scale image");
    Com<IWICFormatConverter> converter;
    check(factory->CreateFormatConverter(converter.out()), "Create image pixel converter");
    check(converter->Initialize(scaler.p, GUID_WICPixelFormat32bppPBGRA, WICBitmapDitherTypeNone,
                               nullptr, 0, WICBitmapPaletteTypeCustom), "Convert image pixels");
    p.pixels.resize((size_t)p.imageWidth*p.imageHeight*4);
    check(converter->CopyPixels(nullptr, p.imageWidth*4, (UINT)p.pixels.size(), p.pixels.data()), "Decode image pixels");
}
bool requestCurrent(const Request &r) {
    return r.sceneRole ? r.token == sceneTokens[r.sceneRole].load() : r.gen == generation.load();
}
std::unique_ptr<Playback> prepare(const Request &r) {
    auto p = std::make_unique<Playback>(); p->gen = r.gen; p->audio = r.audio;
    p->path = r.path; p->sceneRole = r.sceneRole; p->token = r.token; p->target = r.target;
    try {
        if (r.image) { decodeImage(*p, r.path); return p; }
        openLocalMedia(r.path, p->source.out());
        if (!requestCurrent(r)) return p;
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
            if (major == MFMediaType_Audio && !audio && !r.muteTrack) {
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
        p->hasAudio = audio;
        if (!requestCurrent(r)) return p;
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
          workerCV.wait(lock, [] { return quitting.load() || !pending.empty(); });
          if (quitting.load()) return;
          r = pending.front(); pending.pop_front(); }
        if (!requestCurrent(r)) continue;
        auto p = prepare(r);
        if (quitting.load() || !requestCurrent(r)) { retire(std::move(p)); continue; }
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

// Scene playback retains independent foreground sound, stage visuals, and a
// looping background. Only this UI thread touches live renderer state.
struct Scene {
    uint64_t revision = 0, gen = 0, foregroundID = 0;
    std::string foregroundPath, foregroundKind, imagePath, backgroundPath, backgroundKind, audio, display;
    bool foregroundAudio = false, backgroundAudio = false, enabled = false, hardStop = false;
    double fade = 0;
};
std::atomic<uint64_t> sceneRevision{0};
std::mutex sceneMutex;
std::unique_ptr<Scene> latestScene;
Scene scene;
bool sceneMode = false;
uint64_t nextSceneToken = 0, stoppedGeneration = 0;
uint64_t failedImageToken = 0, failedBackgroundToken = 0, completedForegroundToken = 0;
std::string sceneFatalError;
uint64_t sceneFatalGeneration = 0;
std::unique_ptr<Playback> sceneForeground, sceneIncoming, sceneBackground, sceneImage, sceneRetiring, sceneVisualRetiring;

// The EVR continues to decode and play both videos during a transition. Only
// the short crossfade is composited in software; normal playback goes straight
// to the native video window. Three bounded RGB surfaces cap the scratch space
// at 24 MiB even on a large projector. Rapid cue changes retain the last mixed
// frame instead of building an unbounded chain of video renderers.
struct VisualFrame {
    UINT width = 0, height = 0;
    LONGLONG timestamp = 0;
    std::vector<BYTE> pixels;
    bool empty() const { return pixels.empty(); }
    void clear() { width = height = 0; std::vector<BYTE>().swap(pixels); }
};
struct VisualTransition {
    uint64_t shown = 0, from = 0, to = 0;
    ULONGLONG requested = 0, started = 0, lastFrame = 0;
    double seconds = 0;
    bool active = false, frozen = false;
    VisualFrame source, target, mixed;
} visualTransition;
Playback *visualPlayback(uint64_t token) {
    if (!token) return nullptr;
    for (Playback *p : {sceneForeground.get(), sceneIncoming.get(), sceneBackground.get(),
                        sceneImage.get(), sceneRetiring.get(), sceneVisualRetiring.get()})
        if (p && p->token == token) return p;
    return nullptr;
}
bool retainedVisual(const Playback *p) {
    return p && stageEnabled && scene.fade > 0 && (p->token == visualTransition.shown ||
        (visualTransition.active && !visualTransition.frozen && p->token == visualTransition.from));
}
void cancelVisualTransition() {
    visualTransition = VisualTransition{};
    if (transitionWindow) ShowWindow(transitionWindow, SW_HIDE);
}
void frameSize(UINT &width, UINT &height) {
    RECT bounds{}; GetClientRect(stageWindow, &bounds);
    double scale = std::min({1.0, 1920.0/std::max(1L, bounds.right), 1080.0/std::max(1L, bounds.bottom)});
    width = std::max(1U, (UINT)std::lround(bounds.right*scale));
    height = std::max(1U, (UINT)std::lround(bounds.bottom*scale));
}
bool captureVisual(Playback *p, VisualFrame &frame) {
    UINT width, height; frameSize(width, height);
    frame.width = width; frame.height = height;
    frame.pixels.resize((size_t)width*height*4);
    frame.timestamp = 0;
    if (!p) { std::fill(frame.pixels.begin(), frame.pixels.end(), 0); return true; }
    const BYTE *pixels = p->pixels.data();
    LONG sourceWidth = (LONG)p->imageWidth, sourceHeight = -(LONG)p->imageHeight;
    BYTE *videoPixels = nullptr;
    SIZE nativeSize{}, aspectSize{};
    if (p->video) {
        if (!p->display || FAILED(p->display->GetNativeVideoSize(&nativeSize, &aspectSize)) ||
            nativeSize.cx <= 0 || nativeSize.cy <= 0 || nativeSize.cx > 8192 || nativeSize.cy > 8192 ||
            (uint64_t)nativeSize.cx*nativeSize.cy > 33554432) return false;
        BITMAPINFOHEADER info{}; info.biSize = sizeof(info);
        DWORD bytes = 0; LONGLONG timestamp = 0;
        HRESULT hr = p->display->GetCurrentImage(&info, &videoPixels, &bytes, &timestamp);
        if (FAILED(hr) || !videoPixels || info.biWidth <= 0 || info.biHeight == LONG_MIN || !info.biHeight ||
            info.biBitCount != 32 || info.biCompression != BI_RGB || info.biWidth > 8192 ||
            std::abs(info.biHeight) > 8192 || (uint64_t)info.biWidth*std::abs(info.biHeight)*4 > bytes) {
            CoTaskMemFree(videoPixels); return false;
        }
        pixels = videoPixels; sourceWidth = info.biWidth; sourceHeight = info.biHeight; frame.timestamp = timestamp;
    }
    if (!sourceWidth || !sourceHeight || !pixels) { CoTaskMemFree(videoPixels); return false; }
    UINT rows = (UINT)std::abs(sourceHeight);
    // EVR readback is the source frame, not its letterboxed destination. Honor
    // the source's pixel aspect ratio when fitting it to the stage.
    double ratio = p->video && aspectSize.cx > 0 && aspectSize.cy > 0 ?
        (double)aspectSize.cx/aspectSize.cy : (double)sourceWidth/rows;
    UINT drawWidth = width, drawHeight = height;
    if (ratio > (double)width/height) drawHeight = std::max(1U, (UINT)std::lround(width/ratio));
    else drawWidth = std::max(1U, (UINT)std::lround(height*ratio));
    UINT left = (width-drawWidth)/2, top = (height-drawHeight)/2;
    std::fill(frame.pixels.begin(), frame.pixels.end(), 0);
    for (UINT y=0; y<drawHeight; ++y) {
        UINT sy = (UINT)((uint64_t)y*rows/drawHeight);
        if (sourceHeight > 0) sy = rows-1-sy;
        const BYTE *source = pixels+(size_t)sy*sourceWidth*4;
        BYTE *destination = frame.pixels.data()+((size_t)(y+top)*width+left)*4;
        for (UINT x=0; x<drawWidth; ++x) {
            UINT sx = (UINT)((uint64_t)x*sourceWidth/drawWidth);
            memcpy(destination+x*4, source+sx*4, 4);
        }
    }
    CoTaskMemFree(videoPixels); return true;
}
void drawVisualFrame(HDC dc, HWND window, const VisualFrame &frame) {
    if (frame.empty()) return;
    RECT bounds; GetClientRect(window, &bounds);
    BITMAPINFO info{}; info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    info.bmiHeader.biWidth = (LONG)frame.width; info.bmiHeader.biHeight = -(LONG)frame.height;
    info.bmiHeader.biPlanes = 1; info.bmiHeader.biBitCount = 32; info.bmiHeader.biCompression = BI_RGB;
    SetStretchBltMode(dc, HALFTONE); SetBrushOrgEx(dc, 0, 0, nullptr);
    StretchDIBits(dc, 0, 0, bounds.right, bounds.bottom, 0, 0, frame.width, frame.height,
                  frame.pixels.data(), &info, DIB_RGB_COLORS, SRCCOPY);
}

void emitScene(uint64_t g, const char *kind, const std::string &message = "", double pos = 0, double duration = 0) {
    std::ostringstream s;
    s << "{\"generation\":" << g << ",\"sceneRevision\":" << scene.revision
      << ",\"kind\":" << quote(kind) << ",\"message\":" << quote(message)
      << ",\"position\":" << pos << ",\"duration\":" << duration
      << ",\"stageEnabled\":" << (stageEnabled ? "true" : "false") << "}";
    std::lock_guard<std::mutex> lock(eventMutex);
    if (eventQueue.size() >= 256) eventQueue.pop_front();
    eventQueue.push_back(s.str());
}
bool sceneCurrent() { return scene.revision == sceneRevision.load() && !quitting.load(); }
bool volume(Playback &p, float gain) {
    gain = std::clamp(gain, 0.0f, 1.0f);
    if (p.streamVolume && !p.silence.empty()) {
        std::fill(p.silence.begin(), p.silence.end(), gain);
        if (FAILED(p.streamVolume->SetAllVolumes((UINT32)p.silence.size(), p.silence.data()))) return false;
    }
    p.gain = gain;
    return true;
}
void halt(std::unique_ptr<Playback> &p) {
    if (!p) return;
    volume(*p, 0);
    if (p->session) p->session->Stop();
    if (p->target) ShowWindow(p->target, SW_HIDE);
    retire(std::move(p));
}
void ramp(Playback &p, float target, double seconds) {
    if (p.rampTo == target && p.rampSeconds > 0) return;
    p.rampFrom = p.gain; p.rampTo = target;
    p.rampSeconds = seconds;
    p.rampStarted = GetTickCount64();
    if (!p.hasAudio || seconds <= 0 || std::abs(target-p.gain) < 0.0001f) {
        p.rampSeconds = 0;
        if (!volume(p, target)) { sceneFatalError = "Cannot update native audio gain"; sceneFatalGeneration = scene.gen; }
    }
}
bool advanceRamp(Playback &p) {
    if (p.rampSeconds <= 0) return true;
    double t = std::min(1.0, (GetTickCount64()-p.rampStarted)/(p.rampSeconds*1000.0));
    bool ok = volume(p, p.rampFrom + (p.rampTo-p.rampFrom)*(float)t);
    if (t >= 1) p.rampSeconds = 0;
    return ok;
}
bool audible(const Playback *p) { return p && p->playing && p->hasAudio && p->gain > 0.0001f; }
bool anySceneAudio() {
    return audible(sceneForeground.get()) || audible(sceneBackground.get()) || audible(sceneRetiring.get()) || audible(sceneVisualRetiring.get());
}
void freezeVisualTransition() {
    if (visualTransition.active && !visualTransition.mixed.empty()) {
        ULONGLONG now = GetTickCount64();
        double remaining = visualTransition.started ?
            std::max(0.0, visualTransition.seconds-(now-visualTransition.started)/1000.0) : visualTransition.seconds;
        visualTransition.source = visualTransition.mixed;
        visualTransition.frozen = true; visualTransition.from = 0;
        // A pending next decoder may take time. Rebase the remaining fade on
        // exactly the displayed pixels instead of applying its old progress twice.
        visualTransition.seconds = std::max(.001, remaining);
        visualTransition.started = visualTransition.requested = now; visualTransition.lastFrame = 0;
    } else if (Playback *p = visualPlayback(visualTransition.shown)) {
        if (captureVisual(p, visualTransition.source)) visualTransition.frozen = true;
    }
}
void retireWithFade(std::unique_ptr<Playback> &p, double fade) {
    if (!p) return;
    if (retainedVisual(p.get())) {
        // At most one no-longer-selected visual survives. If interrupted,
        // continue from the displayed composite before releasing that renderer.
        if (sceneVisualRetiring) { freezeVisualTransition(); halt(sceneVisualRetiring); }
        sceneVisualRetiring = std::move(p);
        ramp(*sceneVisualRetiring, 0, fade);
    } else if (audible(p.get()) && fade > 0) {
        if (p->target) ShowWindow(p->target, SW_HIDE);
        halt(sceneRetiring);
        sceneRetiring = std::move(p); ramp(*sceneRetiring, 0, fade);
    } else halt(p);
}
Playback *requestedVisual(bool &pendingVisual) {
    pendingVisual = false;
    if (!scene.imagePath.empty()) {
        if (sceneImage && sceneImage->path == scene.imagePath && !sceneImage->pixels.empty()) return sceneImage.get();
        if (failedImageToken != sceneTokens[3].load()) { pendingVisual = true; return nullptr; }
    }
    if (!scene.foregroundPath.empty() && scene.foregroundKind == "video") {
        if (sceneForeground && sceneForeground->gen == scene.foregroundID && sceneForeground->video && sceneForeground->playing)
            return sceneForeground.get();
        if (completedForegroundToken != sceneTokens[1].load()) { pendingVisual = true; return nullptr; }
    }
    if (!scene.backgroundPath.empty()) {
        if (sceneBackground && sceneBackground->path == scene.backgroundPath &&
            (!sceneBackground->pixels.empty() || (sceneBackground->video && sceneBackground->playing))) return sceneBackground.get();
        pendingVisual = failedBackgroundToken != sceneTokens[2].load();
    }
    return nullptr;
}
void displayVisualWindows() {
    Playback *source = stageEnabled && sceneCurrent() && visualTransition.active ? visualPlayback(visualTransition.from) : nullptr;
    Playback *target = stageEnabled && sceneCurrent() ? visualPlayback(visualTransition.shown) : nullptr;
    auto visible = [source, target](HWND window) {
        return (source && source->video && source->playing && source->target == window) ||
               (target && target->video && target->playing && target->target == window);
    };
    for (HWND window : sceneVideoWindows) if (!visible(window)) ShowWindow(window, SW_HIDE);
    for (HWND window : backgroundWindows) if (!visible(window)) ShowWindow(window, SW_HIDE);
    bool overlay = stageEnabled && sceneCurrent() && visualTransition.active && !visualTransition.mixed.empty();
    if (!overlay) ShowWindow(transitionWindow, SW_HIDE);
    if (!stageEnabled || !sceneCurrent()) return;
    auto show = [](Playback *p) {
        if (p && p->video && p->playing && p->target && !IsWindowVisible(p->target))
            ShowWindow(p->target, SW_SHOWNOACTIVATE);
    };
    show(source);
    if (!visualTransition.active || visualTransition.started || overlay) show(target);
    if (overlay) {
        SetWindowPos(transitionWindow, HWND_TOP, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE|SWP_SHOWWINDOW);
        RedrawWindow(transitionWindow, nullptr, nullptr, RDW_INVALIDATE|RDW_UPDATENOW);
    } else if (target && target->video && target->target) {
        SetWindowPos(target->target, HWND_TOP, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE);
    }
    RedrawWindow(stageWindow, nullptr, nullptr, RDW_INVALIDATE|RDW_UPDATENOW);
    syncStageCursor();
}
void finishVisualTransition() {
    visualTransition.active = false; visualTransition.frozen = false;
    visualTransition.from = visualTransition.to = 0;
    visualTransition.source.clear(); visualTransition.target.clear(); visualTransition.mixed.clear();
    displayVisualWindows();
}
void tickVisualTransition() {
    if (!visualTransition.active || !stageEnabled || !sceneCurrent()) return;
    ULONGLONG now = GetTickCount64();
    if (now-visualTransition.lastFrame < 33) return;
    visualTransition.lastFrame = now;
    bool sourceReady = visualTransition.frozen ? !visualTransition.source.empty() :
        ((!visualTransition.from || visualPlayback(visualTransition.from)) &&
         captureVisual(visualPlayback(visualTransition.from), visualTransition.source));
    bool targetReady = (!visualTransition.to || visualPlayback(visualTransition.to)) &&
        captureVisual(visualPlayback(visualTransition.to), visualTransition.target);
    if (!sourceReady || !targetReady) {
        if (sourceReady && visualTransition.mixed.empty()) visualTransition.mixed = visualTransition.source;
        // Some EVR/remote-display drivers cannot read back their surface. Leave
        // the outgoing image in place briefly, then use live native playback;
        // never leave a frozen overlay covering an otherwise healthy video.
        if (now-visualTransition.requested < 1500) return;
        finishVisualTransition();
        emitScene(scene.gen, "background-error", "Visual fade unavailable on this display; showing the selected visual immediately");
        return;
    }
    if (!visualTransition.started) visualTransition.started = now;
    double progress = std::min(1.0, (now-visualTransition.started)/(visualTransition.seconds*1000.0));
    if (progress >= 1) { finishVisualTransition(); return; }
    VisualFrame &output = visualTransition.mixed;
    output.width = visualTransition.target.width; output.height = visualTransition.target.height;
    if (visualTransition.source.width != output.width || visualTransition.source.height != output.height) {
        finishVisualTransition(); return; // Display changed; never reuse differently sized pixel storage.
    }
    output.pixels.resize(visualTransition.target.pixels.size());
    unsigned incoming = (unsigned)std::lround(progress*256), outgoing = 256-incoming;
    for (size_t i=0; i<output.pixels.size(); ++i)
        output.pixels[i] = (BYTE)((visualTransition.source.pixels[i]*outgoing + visualTransition.target.pixels[i]*incoming)>>8);
    displayVisualWindows();
}
void sceneVisuals() {
    if (!stageEnabled || !sceneCurrent()) { cancelVisualTransition(); displayVisualWindows(); return; }
    bool pendingVisual = false;
    Playback *target = requestedVisual(pendingVisual);
    uint64_t token = target ? target->token : 0;
    if (pendingVisual) { displayVisualWindows(); return; }
    if (token != visualTransition.shown) {
        bool interrupted = visualTransition.active;
        bool hadVisual = visualTransition.shown != 0 || (interrupted && !visualTransition.mixed.empty());
        if (interrupted) freezeVisualTransition();
        else { visualTransition.frozen = false; visualTransition.source.clear(); }
        visualTransition.from = visualTransition.frozen ? 0 : visualTransition.shown;
        visualTransition.shown = visualTransition.to = token;
        visualTransition.seconds = scene.fade;
        visualTransition.requested = GetTickCount64();
        visualTransition.started = visualTransition.lastFrame = 0;
        visualTransition.active = scene.fade > 0 && hadVisual;
        if (!visualTransition.active) finishVisualTransition();
    }
    tickVisualTransition(); displayVisualWindows();
}
Playback *visualImage() {
    if (!sceneMode || !stageEnabled) return nullptr;
    Playback *p = visualPlayback(visualTransition.shown);
    return p && !p->pixels.empty() ? p : nullptr;
}
void paintImage(HDC dc, HWND window) {
    if (window == transitionWindow) { drawVisualFrame(dc, window, visualTransition.mixed); return; }
    Playback *p = window == stageWindow ? visualImage() : nullptr;
    if (!p) return;
    RECT bounds; GetClientRect(window, &bounds);
    double scale = std::min((double)(bounds.right-bounds.left)/p->imageWidth,
                            (double)(bounds.bottom-bounds.top)/p->imageHeight);
    int width = (int)std::lround(p->imageWidth*scale), height = (int)std::lround(p->imageHeight*scale);
    BITMAPINFO info{}; info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    info.bmiHeader.biWidth = (LONG)p->imageWidth; info.bmiHeader.biHeight = -(LONG)p->imageHeight;
    info.bmiHeader.biPlanes = 1; info.bmiHeader.biBitCount = 32; info.bmiHeader.biCompression = BI_RGB;
    SetStretchBltMode(dc, HALFTONE); SetBrushOrgEx(dc, 0, 0, nullptr);
    StretchDIBits(dc, (bounds.right-width)/2, (bounds.bottom-height)/2, width, height,
                  0, 0, p->imageWidth, p->imageHeight, p->pixels.data(), &info, DIB_RGB_COLORS, SRCCOPY);
}
void backgroundGain(bool transition = true) {
    if (!sceneBackground || !sceneBackground->playing) return;
    bool foregroundSound = sceneForeground && sceneForeground->playing && sceneForeground->hasAudio;
    float target = stageEnabled && scene.backgroundAudio && !foregroundSound ? 1.0f : 0.0f;
    double duration = transition && (target == 0 || anySceneAudio()) ? scene.fade : 0;
    ramp(*sceneBackground, target, duration);
}
void clearScene(bool hide) {
    cancelVisualTransition();
    for (int role=1; role<=3; ++role) sceneTokens[role].store(++nextSceneToken);
    { std::lock_guard<std::mutex> lock(workerMutex);
      pending.erase(std::remove_if(pending.begin(), pending.end(), [](const Request &r) { return r.sceneRole != 0; }), pending.end()); }
    halt(sceneIncoming); halt(sceneForeground); halt(sceneBackground); halt(sceneImage); halt(sceneRetiring); halt(sceneVisualRetiring);
    stoppedGeneration = 0;
    if (hide) hideStageWindow();
}
bool queueScene(int role, const std::string &path, bool image, bool video, bool mute, uint64_t gen, HWND target) {
    { std::lock_guard<std::mutex> lock(cleanupMutex);
      if (retired.size() >= 8) return false; }
    Request r{Request::Play, gen, path, scene.audio, scene.display};
    r.sceneRole = role; r.token = ++nextSceneToken; r.image = image; r.video = video; r.muteTrack = mute; r.target = target;
    sceneTokens[role].store(r.token);
    { std::lock_guard<std::mutex> lock(workerMutex);
      pending.erase(std::remove_if(pending.begin(), pending.end(), [role](const Request &old) { return old.sceneRole == role; }), pending.end());
      pending.push_back(r); }
    workerCV.notify_one(); return true;
}
template<size_t N> HWND availableSceneWindow(HWND (&windows)[N]) {
    auto owned = [](HWND window) {
        for (Playback *p : {sceneForeground.get(), sceneIncoming.get(), sceneBackground.get(),
                            sceneRetiring.get(), sceneVisualRetiring.get()})
            if (p && p->video && p->target == window) return true;
        return false;
    };
    for (HWND window : windows) if (!owned(window)) return window;
    // Rapid commands can occupy the bounded pool with a hidden audio tail.
    // Silence/release that oldest tail before reusing its video target.
    if (sceneRetiring && sceneRetiring->video) {
        for (HWND window : windows) {
            if (sceneRetiring->target == window) { halt(sceneRetiring); return window; }
        }
    }
    return nullptr;
}
void resizeSceneRenderers() {
    RECT r; GetClientRect(stageWindow, &r);
    for (HWND window : sceneVideoWindows) SetWindowPos(window, nullptr, 0, 0, r.right, r.bottom, SWP_NOZORDER|SWP_NOACTIVATE);
    for (HWND window : backgroundWindows) SetWindowPos(window, nullptr, 0, 0, r.right, r.bottom, SWP_NOZORDER|SWP_NOACTIVATE);
    SetWindowPos(transitionWindow, nullptr, 0, 0, r.right, r.bottom, SWP_NOZORDER|SWP_NOACTIVATE);
    for (Playback *p : {sceneForeground.get(), sceneIncoming.get(), sceneBackground.get()})
        if (p && p->display) p->display->SetVideoPosition(nullptr, &r);
}
void processSceneCommand() {
    std::unique_ptr<Scene> next;
    { std::lock_guard<std::mutex> lock(sceneMutex); next = std::move(latestScene); }
    if (!next || next->revision != sceneRevision.load()) return;
    if (!sceneMode) { stopCurrent(); sceneMode = true; }
    Scene previous = scene; scene = std::move(*next);
    generation.store(scene.gen);
    if (scene.hardStop) {
        clearScene(true); emitScene(scene.gen, "stage"); emitScene(scene.gen, "stopped"); return;
    }
    if (scene.enabled) {
        if (!stageEnabled || stageDisplay != scene.display) {
            if (!enableStage(scene.display)) {
                clearScene(true); emitScene(scene.gen, "error", "Selected stage display is unavailable"); return;
            }
            resizeSceneRenderers();
        }
    } else if (stageEnabled) {
        hideStageWindow();
    }
    emitScene(scene.gen, "stage");
    if (scene.foregroundPath.empty() && scene.gen != previous.gen) stoppedGeneration = scene.gen;
    if (scene.foregroundID != previous.foregroundID || scene.foregroundPath != previous.foregroundPath || (scene.foregroundAudio && scene.audio != previous.audio)) {
        halt(sceneIncoming); sceneTokens[1].store(++nextSceneToken);
        if (scene.foregroundPath.empty()) {
            retireWithFade(sceneForeground, scene.fade); stoppedGeneration = scene.gen;
        } else {
            HWND target = availableSceneWindow(sceneVideoWindows);
            if ((!target && scene.foregroundKind == "video") ||
                !queueScene(1, scene.foregroundPath, false, scene.foregroundKind == "video", !scene.foregroundAudio, scene.foregroundID, target)) {
                clearScene(true); emitScene(scene.foregroundID, "error", "Native cleanup is busy; stop and retry"); return;
            }
        }
    }
    if (scene.imagePath != previous.imagePath) {
        retireWithFade(sceneImage, scene.fade); sceneTokens[3].store(++nextSceneToken);
        if (!scene.imagePath.empty() && !queueScene(3, scene.imagePath, true, false, true, scene.gen, nullptr)) {
            failedImageToken = sceneTokens[3].load();
            emitScene(scene.gen, "background-error", "Native image cleanup is busy; retry the image cue");
        }
    }
    bool backgroundChanged = scene.backgroundPath != previous.backgroundPath || scene.backgroundKind != previous.backgroundKind ||
        (scene.backgroundAudio && (scene.audio != previous.audio ||
          (sceneBackground && (sceneBackground->audio != scene.audio || !sceneBackground->hasAudio))));
    if (backgroundChanged || (!stageEnabled && sceneBackground)) {
        sceneTokens[2].store(++nextSceneToken);
        if (sceneBackground) retireWithFade(sceneBackground, scene.fade);
    }
    if (stageEnabled && !scene.backgroundPath.empty() && (backgroundChanged || !previous.enabled)) {
        HWND target = availableSceneWindow(backgroundWindows);
        if (target) backgroundWindow = target;
        if ((!target && scene.backgroundKind == "video") ||
            !queueScene(2, scene.backgroundPath, scene.backgroundKind == "image", scene.backgroundKind == "video", scene.audio.empty(), scene.gen, target)) {
            failedBackgroundToken = sceneTokens[2].load();
            emitScene(scene.gen, "background-error", "Native background cleanup is busy; retry the background");
        }
    }
    if (!stageEnabled) sceneTokens[2].store(++nextSceneToken);
    backgroundGain(); sceneVisuals();
}
void sceneReady(std::unique_ptr<Playback> p) {
    processSceneCommand();
    if (!sceneMode || quitting.load() || p->token != sceneTokens[p->sceneRole].load()) { retire(std::move(p)); return; }
    if (!p->error.empty()) {
        uint64_t failedGen = p->gen; int role = p->sceneRole; std::string error = p->error;
        if (role == 2) failedBackgroundToken = p->token;
        if (role == 3) failedImageToken = p->token;
        retire(std::move(p));
        if (role == 1) clearScene(true); else sceneVisuals();
        emitScene(role == 1 ? failedGen : scene.gen, role == 1 ? "error" : "background-error", error); return;
    }
    if (p->sceneRole == 1) { halt(sceneIncoming); sceneIncoming = std::move(p); }
    else if (p->sceneRole == 2) { retireWithFade(sceneBackground, scene.fade); p->looping = true; sceneBackground = std::move(p); }
    else { retireWithFade(sceneImage, scene.fade); sceneImage = std::move(p); }
    sceneVisuals();
}
bool startScenePlayback(Playback &p) {
    if (!sceneCurrent() || p.token != sceneTokens[p.sceneRole].load()) return false;
    PROPVARIANT start; PropVariantInit(&start); start.vt = VT_I8; start.hVal.QuadPart = 0;
    check(p.session->Start(&GUID_NULL, &start), "Start native scene playback");
    p.startRequested = true; return true;
}
bool configureScenePlayback(Playback &p) {
    if (p.video) {
        check(MFGetService(p.session.p, MR_VIDEO_RENDER_SERVICE, __uuidof(IMFVideoDisplayControl), (void**)p.display.out()), "Configure stage renderer");
        RECT rect; GetClientRect(p.target, &rect);
        check(p.display->SetAspectRatioMode(MFVideoARMode_PreservePicture), "Set video aspect ratio");
        p.display->SetBorderColor(RGB(0,0,0)); p.display->SetVideoPosition(nullptr, &rect);
    }
    if (p.hasAudio) {
        check(MFGetService(p.session.p, MR_STREAM_VOLUME_SERVICE, __uuidof(IMFAudioStreamVolume), (void**)p.streamVolume.out()), "Acquire independent stream volume");
        UINT32 channels = 0; check(p.streamVolume->GetChannelCount(&channels), "Read audio channel count");
        if (!channels || channels > 64) throw std::string("Unsupported audio channel count");
        p.silence.assign(channels, 0.0f);
        check(p.streamVolume->SetAllVolumes(channels, p.silence.data()), "Initialize silent incoming stream");
    }
    Com<IMFClock> clock;
    if (SUCCEEDED(p.session->GetClock(clock.out()))) clock->QueryInterface(__uuidof(IMFPresentationClock), (void**)p.clock.out());
    p.topologyReady = true;
    return startScenePlayback(p);
}
void activateScenePlayback(Playback &p, bool incoming) {
    if (p.playing || !p.nativeStarted || !sceneCurrent() || p.token != sceneTokens[p.sceneRole].load()) return;
    p.playing = true;
    bool wasAudible = anySceneAudio();
    if (incoming) {
        retireWithFade(sceneForeground, scene.fade);
        ramp(p, p.hasAudio ? 1.0f : 0.0f, wasAudible ? scene.fade : 0);
        if (sceneBackground) ramp(*sceneBackground, p.hasAudio ? 0.0f : (stageEnabled && scene.backgroundAudio ? 1.0f : 0.0f), wasAudible ? scene.fade : 0);
        emitScene(p.gen, "playing", "", 0, p.duration);
    } else if (p.sceneRole == 2) {
        bool foregroundSound = sceneForeground && sceneForeground->playing && sceneForeground->hasAudio;
        ramp(p, stageEnabled && scene.backgroundAudio && !foregroundSound ? 1.0f : 0.0f, wasAudible ? scene.fade : 0);
        sceneVisuals();
    }
}
void restartSceneLoop(Playback &p) {
    if (!p.loopPending || !sceneCurrent() || !stageEnabled || p.token != sceneTokens[2].load()) return;
    PROPVARIANT start; PropVariantInit(&start); start.vt = VT_I8; start.hVal.QuadPart = 0;
    check(p.session->Start(&GUID_NULL, &start), "Loop background video");
    p.loopPending = false; p.loopSeeking = true;
}
// Returns true when a session has reached its end or failed and must retire.
bool pumpScene(Playback &p, bool incoming) {
    if (!p.session || !sceneCurrent()) return false;
    try {
        if (p.topologyReady && !p.startRequested && !startScenePlayback(p)) return false;
        activateScenePlayback(p, incoming);
        restartSceneLoop(p);
        for (int i=0; i<16; ++i) {
            Com<IMFMediaEvent> event;
            HRESULT hr = p.session->GetEvent(MF_EVENT_FLAG_NO_WAIT, event.out());
            if (hr == MF_E_NO_EVENTS_AVAILABLE) break;
            check(hr, "Read native scene event");
            MediaEventType type; HRESULT status;
            check(event->GetType(&type), "Read native event type"); check(event->GetStatus(&status), "Read native event result");
            check(status, "Native scene playback failed");
            if (type == MESessionTopologyStatus && MFGetAttributeUINT32(event.p, MF_EVENT_TOPOLOGY_STATUS, 0) == MF_TOPOSTATUS_READY) {
                if (!configureScenePlayback(p)) return false;
            } else if (type == MESessionStarted) {
                p.nativeStarted = true;
                if (p.loopSeeking) { p.loopSeeking = false; continue; }
                activateScenePlayback(p, incoming);
            } else if (type == MESessionEnded) {
                if (p.looping && stageEnabled && p.token == sceneTokens[2].load()) {
                    // Wait for the terminal session event, not both end events,
                    // so one loop causes exactly one seek and retains its gain.
                    p.loopPending = true;
                    restartSceneLoop(p);
                } else {
                    if (p.sceneRole == 1) emitScene(p.gen, "ended");
                    return true;
                }
            }
        }
    } catch (const std::string &error) {
        if (p.sceneRole == 1) {
            if (p.gen == scene.foregroundID) { sceneFatalError = error; sceneFatalGeneration = p.gen; }
        } else emitScene(scene.gen, "background-error", error);
        return true;
    }
    return false;
}
// A retiring background remains animated through a long fade even after its
// role token has been replaced. Drain only its own session events; never emit
// PLAY/ENDED or revive it once the visual transition has released it.
void pumpRetiringVisual(Playback &p) {
    if (!p.session || !p.video || !sceneCurrent() || !retainedVisual(&p)) return;
    for (int i=0; i<8; ++i) {
        Com<IMFMediaEvent> event;
        HRESULT hr = p.session->GetEvent(MF_EVENT_FLAG_NO_WAIT, event.out());
        if (hr == MF_E_NO_EVENTS_AVAILABLE) break;
        MediaEventType type = MEUnknown; HRESULT status = S_OK;
        bool ok = SUCCEEDED(hr) && SUCCEEDED(event->GetType(&type)) && SUCCEEDED(event->GetStatus(&status)) && SUCCEEDED(status);
        if (ok && type == MESessionEnded && p.looping && stageEnabled && retainedVisual(&p)) {
            PROPVARIANT start; PropVariantInit(&start); start.vt = VT_I8; start.hVal.QuadPart = 0;
            ok = SUCCEEDED(p.session->Start(&GUID_NULL, &start));
        }
        if (!ok) {
            // Keep only the last outgoing frame for the remainder of this fade.
            // The new renderer continues normally and the failed one is muted.
            if (visualTransition.active && visualTransition.from == p.token && !visualTransition.source.empty()) {
                visualTransition.frozen = true; visualTransition.from = 0;
            }
            volume(p, 0); p.rampSeconds = 0;
            break;
        }
    }
}
void tickScene() {
    processSceneCommand();
    if (!sceneMode || !sceneCurrent()) return;
    if (!sceneFatalError.empty()) { clearScene(true); emitScene(sceneFatalGeneration, "error", sceneFatalError); sceneFatalError.clear(); return; }
    if (sceneIncoming && pumpScene(*sceneIncoming, true)) halt(sceneIncoming);
    if (!sceneFatalError.empty()) { clearScene(true); emitScene(sceneFatalGeneration, "error", sceneFatalError); sceneFatalError.clear(); return; }
    if (sceneIncoming && sceneIncoming->playing) {
        halt(sceneForeground); sceneForeground = std::move(sceneIncoming); sceneVisuals();
    }
    if (sceneForeground && pumpScene(*sceneForeground, false)) {
        completedForegroundToken = sceneForeground->token;
        retireWithFade(sceneForeground, scene.fade);
        if (!sceneFatalError.empty()) { clearScene(true); emitScene(sceneFatalGeneration, "error", sceneFatalError); sceneFatalError.clear(); return; }
        backgroundGain(); sceneVisuals();
    }
    if (sceneBackground && pumpScene(*sceneBackground, false)) {
        failedBackgroundToken = sceneBackground->token;
        retireWithFade(sceneBackground, scene.fade); sceneVisuals();
    }
    if (sceneVisualRetiring) pumpRetiringVisual(*sceneVisualRetiring);
    for (Playback *p : {sceneForeground.get(), sceneBackground.get(), sceneRetiring.get(), sceneVisualRetiring.get()}) {
        if (p && !advanceRamp(*p)) {
            clearScene(true); emitScene(scene.gen, "error", "Cannot update native audio fade"); return;
        }
    }
    tickVisualTransition();
    if (sceneRetiring && sceneRetiring->rampSeconds <= 0) halt(sceneRetiring);
    if (sceneVisualRetiring && !retainedVisual(sceneVisualRetiring.get()) && sceneVisualRetiring->rampSeconds <= 0)
        halt(sceneVisualRetiring);
    if (stoppedGeneration && !sceneForeground && !sceneIncoming && !sceneRetiring && !sceneVisualRetiring) {
        emitScene(stoppedGeneration, "stopped"); stoppedGeneration = 0;
    }
    static ULONGLONG lastProgress = 0;
    if (sceneForeground && sceneForeground->playing && sceneForeground->clock && GetTickCount64()-lastProgress >= 250) {
        MFTIME time = 0;
        if (SUCCEEDED(sceneForeground->clock->GetTime(&time)))
            emitScene(sceneForeground->gen, "progress", "", time/10000000.0, sceneForeground->duration);
        lastProgress = GetTickCount64();
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
            if (active->hasAudio) {
                hr = MFGetService(active->session.p, MR_STREAM_VOLUME_SERVICE, __uuidof(IMFAudioStreamVolume), (void**)active->streamVolume.out());
                if (FAILED(hr)) { stopCurrent(); emit(g, "error", failure(hr, "Acquire audio stream silence control")); return; }
                UINT32 channels = 0;
                hr = active->streamVolume->GetChannelCount(&channels);
                if (FAILED(hr) || channels == 0 || channels > 64) {
                    stopCurrent(); emit(g, "error", "Cannot prepare bounded per-stream audio silence control"); return;
                }
                // Prepare/check the stream control before any samples play;
                // STOP then needs no allocation. Leave the user's session and
                // endpoint mixer mute/volume settings unchanged.
                active->silence.assign(channels, 1.0f);
                hr = active->streamVolume->SetAllVolumes(channels, active->silence.data());
                std::fill(active->silence.begin(), active->silence.end(), 0.0f);
                if (FAILED(hr)) { stopCurrent(); emit(g, "error", failure(hr, "Initialize audio stream volume")); return; }
            }
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
    if (sceneMode) {
        processSceneCommand();
        auto displays = monitors();
        bool lostDisplay = stageEnabled && std::none_of(displays.begin(), displays.end(), [](const Monitor &m) { return m.id == stageDisplay; });
        bool lostAudio = false;
        std::vector<std::string> requiredAudio;
        for (Playback *p : {sceneForeground.get(), sceneIncoming.get(), sceneRetiring.get(), sceneVisualRetiring.get()})
            if (p && p->hasAudio && !p->audio.empty()) requiredAudio.push_back(p->audio);
        if (scene.backgroundAudio && stageEnabled && sceneBackground && sceneBackground->hasAudio && !sceneBackground->audio.empty())
            requiredAudio.push_back(sceneBackground->audio);
        if (!requiredAudio.empty()) {
            try {
                auto devices = audioDevices();
                for (const std::string &endpoint : requiredAudio)
                    if (std::none_of(devices.begin(), devices.end(), [&](const Audio &a) { return a.id == endpoint; })) lostAudio = true;
            } catch (...) { lostAudio = true; }
        }
        if (lostDisplay || lostAudio) {
            clearScene(true);
            emitScene(scene.gen, "device-lost", lostDisplay ? "Stage display disconnected; select and enable it again" : "Audio output disconnected; playback stopped");
        }
        emitScene(scene.gen, "devices"); return;
    }
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
    if (msg == sceneMessage) { processSceneCommand(); return 0; }
    if (msg == commandMessage) {
        std::unique_ptr<Request> r;
        { std::lock_guard<std::mutex> lock(commandMutex); r = std::move(latestCommand); commandScheduled = false; }
        if (!r) return 0;
        if (sceneMode) { clearScene(true); sceneMode = false; }
        if (r->gen != generation.load()) return 0;
        std::string stopError = stopCurrent();
        if (!stopError.empty()) {
            if (r->kind == Request::Stage && !r->enabled) disableStage();
            emit(r->gen, "error", stopError); return 0;
        }
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
        { std::lock_guard<std::mutex> lock(workerMutex); pending.clear(); pending.push_back(*r); }
        workerCV.notify_one(); return 0;
    }
    if (msg == readyMessage) {
        std::unique_ptr<Playback> p((Playback*)lp);
        if (p->sceneRole) { sceneReady(std::move(p)); return 0; }
        if (p->gen != generation.load() || quitting.load()) { retire(std::move(p)); return 0; }
        if (!p->error.empty()) { emit(p->gen, "error", p->error); retire(std::move(p)); return 0; }
        active = std::move(p); return 0;
    }
    if (msg == WM_TIMER) {
        // A fallback for a failed wake PostMessage; control admission is a
        // single latest-command slot, never an unbounded OS message backlog.
        SendMessageW(controlWindow, commandMessage, 0, 0);
        processSceneCommand();
        if (sceneMode) tickScene(); else tick();
        // Stage changes, capture and renderer/WebView callbacks can change the
        // pointer without another WM_SETCURSOR. Reconcile its actual owner.
        syncStageCursor(); return 0;
    }
    if (msg == deviceMessage || msg == WM_DISPLAYCHANGE || msg == WM_DEVICECHANGE) { checkDevices(); return 0; }
    if (msg == WM_KEYDOWN && wp == VK_ESCAPE) {
        if (sceneMode) { processSceneCommand(); clearScene(true); emitScene(scene.gen, "escape"); return 0; }
        uint64_t g = generation.fetch_add(1); disableStage(); emit(g, "escape"); return 0;
    }
    if (msg == WM_CLOSE && hwnd != controlWindow) { if (sceneMode) { clearScene(true); emitScene(scene.gen, "escape"); return 0; } uint64_t g = generation.fetch_add(1); disableStage(); emit(g, "escape"); return 0; }
    if (msg == WM_SETCURSOR && stageSurface(hwnd) && stageEnabled) { selectStageCursor(); return TRUE; }
    if (msg == WM_ERASEBKGND && hwnd == transitionWindow && !visualTransition.mixed.empty()) return TRUE;
    if (msg == WM_ERASEBKGND) { RECT r; GetClientRect(hwnd,&r); FillRect((HDC)wp,&r,(HBRUSH)GetStockObject(BLACK_BRUSH)); return TRUE; }
    if (msg == WM_PAINT) {
        PAINTSTRUCT ps; HDC dc = BeginPaint(hwnd, &ps);
        if (hwnd != transitionWindow || visualTransition.mixed.empty()) FillRect(dc, &ps.rcPaint, (HBRUSH)GetStockObject(BLACK_BRUSH));
        paintImage(dc, hwnd); EndPaint(hwnd, &ps);
        if (hwnd == videoWindow && active && active->display && active->gen == generation.load()) active->display->RepaintVideo();
        if (sceneMode) for (Playback *p : {sceneForeground.get(), sceneBackground.get(), sceneVisualRetiring.get()})
            if (p && p->target == hwnd && p->display) p->display->RepaintVideo();
        return 0;
    }
    if (msg == quitMessage) { clearScene(true); stopCurrent(); disableStage(); PostQuitMessage(0); return 0; }
    return DefWindowProcW(hwnd, msg, wp, lp);
}
void submit(Request *r) {
    bool wake = false;
    { std::lock_guard<std::mutex> lock(commandMutex);
      generation.store(r->gen); latestCommand.reset(r);
      if (!commandScheduled) { commandScheduled = true; wake = true; } }
    if (wake) PostMessageW(controlWindow, commandMessage, 0, 0);
}
} // namespace

extern "C" char *ss_init() {
    HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    if (FAILED(hr)) return copy(failure(hr, "Initialize COM MTA"));
    hr = MFStartup(MF_VERSION, MFSTARTUP_FULL);
    if (FAILED(hr)) { CoUninitialize(); return copy(failure(hr, "Media Foundation unavailable; check Windows multimedia components")); }
    bool registered = false;
    auto initializationFailure = [&](const std::string &message) {
        if (stageWindow) DestroyWindow(stageWindow);
        if (controlWindow) DestroyWindow(controlWindow);
        stageWindow = controlWindow = videoWindow = backgroundWindow = transitionWindow = nullptr;
        for (HWND &window : sceneVideoWindows) window = nullptr;
        for (HWND &window : backgroundWindows) window = nullptr;
        if (registered) UnregisterClassW(L"SmartStageNative", GetModuleHandleW(nullptr));
        if (stageCursor) { DestroyCursor(stageCursor); stageCursor = nullptr; }
        MFShutdown(); CoUninitialize();
        return copy(message);
    };
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    stageCursor = createStageCursor();
    if (!stageCursor) return initializationFailure(failure(HRESULT_FROM_WIN32(GetLastError()), "Create transparent stage cursor"));
    WNDCLASSW cls{}; cls.lpfnWndProc = windowProc; cls.hInstance = GetModuleHandleW(nullptr);
    cls.lpszClassName = L"SmartStageNative"; cls.hbrBackground = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (!RegisterClassW(&cls)) return initializationFailure("Cannot register native stage window");
    registered = true;
    // Hidden top-level control window receives broadcast display-change events.
    controlWindow = CreateWindowExW(0, cls.lpszClassName, L"Smart Stage", 0, 0,0,0,0, nullptr,nullptr,cls.hInstance,nullptr);
    stageWindow = CreateWindowExW(WS_EX_TOPMOST | WS_EX_TOOLWINDOW, cls.lpszClassName, L"", WS_POPUP | WS_CLIPCHILDREN, 0,0,1,1,nullptr,nullptr,cls.hInstance,nullptr);
    videoWindow = CreateWindowExW(0, cls.lpszClassName, L"", WS_CHILD | WS_CLIPSIBLINGS, 0,0,1,1,stageWindow,nullptr,cls.hInstance,nullptr);
    for (HWND &window : backgroundWindows)
        window = CreateWindowExW(0, cls.lpszClassName, L"", WS_CHILD | WS_CLIPSIBLINGS, 0,0,1,1,stageWindow,nullptr,cls.hInstance,nullptr);
    backgroundWindow = backgroundWindows[0];
    for (HWND &window : sceneVideoWindows)
        window = CreateWindowExW(0, cls.lpszClassName, L"", WS_CHILD | WS_CLIPSIBLINGS, 0,0,1,1,stageWindow,nullptr,cls.hInstance,nullptr);
    transitionWindow = CreateWindowExW(0, cls.lpszClassName, L"", WS_CHILD | WS_CLIPSIBLINGS, 0,0,1,1,stageWindow,nullptr,cls.hInstance,nullptr);
    if (!controlWindow || !stageWindow || !videoWindow || !backgroundWindow || !transitionWindow || !sceneVideoWindows[0] || !sceneVideoWindows[1] || !sceneVideoWindows[2] || !backgroundWindows[1])
        return initializationFailure("Cannot create native stage windows in this desktop session");
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
    }
    { std::lock_guard<std::mutex> lock(commandMutex); latestCommand.reset(); }
    cleanupQuitting.store(true); cleanupCV.notify_all(); if (cleaner.joinable()) cleaner.join();
    ss_windows_desktop_shutdown();
    hideStageWindow();
    DestroyWindow(stageWindow); DestroyWindow(controlWindow);
    if (GetCursor() == stageCursor) SetCursor(LoadCursorW(nullptr, IDC_ARROW));
    if (stageCursor) { DestroyCursor(stageCursor); stageCursor = nullptr; }
    if (notificationEnumerator.p) { notificationEnumerator.p->Release(); notificationEnumerator.p = nullptr; }
    MFShutdown(); CoUninitialize();
}
extern "C" void ss_quit() { quitting.store(true); generation.fetch_add(1); PostMessageW(controlWindow, quitMessage, 0, 0); }
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
        // WIC validates actual pixels; a filename extension is never accepted as
        // proof that a file is a decodable image.
        try {
            Playback image; decodeImage(image, path);
            return copy("{\"kind\":\"image\",\"hasAudio\":false,\"hasVideo\":false,\"duration\":0}");
        } catch (const std::string &) { /* Let Media Foundation inspect audio/video. */ }
        Com<IMFMediaSource> source; Com<IMFSourceReader> reader; Com<IMFAttributes> attrs;
        check(MFCreateAttributes(attrs.out(), 1), "Create reader attributes");
        check(attrs->SetUINT32(MF_SOURCE_READER_ENABLE_VIDEO_PROCESSING, TRUE), "Enable native video decoding");
        openLocalMedia(path, source.out());
        HRESULT readerResult = MFCreateSourceReaderFromMediaSource(source.p, attrs.p, reader.out());
        // On success the reader owns shutdown. If construction fails, there
        // is no reader to shut down the source we created.
        if (FAILED(readerResult)) source->Shutdown();
        check(readerResult, "Inspect local media");
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

extern "C" void ss_scene(const ss_scene_request *r) {
    if (!r) return;
    auto s = std::make_unique<Scene>();
    s->revision = r->revision; s->gen = r->generation; s->foregroundID = r->foreground_id;
    auto text = [](const char *p) { return p ? std::string(p) : std::string(); };
    s->foregroundPath = text(r->foreground_path); s->foregroundKind = text(r->foreground_kind);
    s->imagePath = text(r->image_path); s->backgroundPath = text(r->background_path); s->backgroundKind = text(r->background_kind);
    s->audio = text(r->audio); s->display = text(r->display);
    s->foregroundAudio = r->foreground_has_audio != 0; s->backgroundAudio = r->background_audio != 0;
    s->enabled = r->stage_enabled != 0; s->hardStop = r->hard_stop != 0;
    s->fade = std::isfinite(r->fade_seconds) ? std::clamp(r->fade_seconds, 0.0, 30.0) : 0;
    bool wake = false;
    { std::lock_guard<std::mutex> lock(sceneMutex);
      if (r->revision <= sceneRevision.load()) return;
      sceneRevision.store(r->revision); wake = !latestScene; latestScene = std::move(s); }
    if (wake) PostMessageW(controlWindow, sceneMessage, 0, 0);
}
