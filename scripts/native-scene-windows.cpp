// Test-only driver includes the production bridge to read renderer state on its
// owning UI thread. No diagnostic endpoints or hooks ship in Smart Stage.
#include "../internal/platform/bridge_windows.cpp"
#include <cstdio>

namespace {
ss_scene_request desired{};
std::string probeAudio, probeDisplay, probeWaveA, probeWaveB, probeVideo, probeImage;
unsigned phase = 0;
ULONGLONG phaseStart = 0;
uint64_t probeRevision = 0, retainedToken = 0;
double lastBackgroundTime = 0, retainedTime = 0;
bool soundAvailable = true;
bool passed = false, observedBackgroundCrossfade = false, observedCueCrossfade = false;
bool observedStopCrossfade = false, sawBackgroundError = false;
std::string probeFailure;

void require(bool condition, const char *message) { if (!condition) throw std::string(message); }
void next(unsigned value) { phase = value; phaseStart = GetTickCount64(); }
void apply() { desired.revision = ++probeRevision; ss_scene(&desired); }
double position(Playback *p) {
    MFTIME value = 0;
    if (!p || !p->clock || FAILED(p->clock->GetTime(&value))) return -1;
    return value/10000000.0;
}
void checkGain(Playback *p) {
    require(p && p->streamVolume && !p->silence.empty(), "Missing actual per-stream volume service");
    std::vector<float> actual(p->silence.size());
    require(SUCCEEDED(p->streamVolume->GetAllVolumes((UINT32)actual.size(), actual.data())), "Cannot read actual native gain");
    for (float gain : actual) require(std::abs(gain-p->gain) < .02f, "Native stream gain differs from the configured fade");
}
void consumeEvents() {
    char *raw;
    while ((raw = ss_poll())) {
        std::string event(raw); ss_free(raw);
        if (event.find("\"kind\":\"background-error\"") != std::string::npos) {
            require(phase == 12, "Unexpected background error"); sawBackgroundError = true;
        }
        if (event.find("\"kind\":\"error\"") != std::string::npos) throw std::string("Native scene error: ")+event;
        if (phase == 14 && event.find("\"kind\":\"playing\"") != std::string::npos)
            throw std::string("Canceled foreground started after hard STOP: ")+event;
    }
}
void CALLBACK probeTick(HWND, UINT, UINT_PTR timer, DWORD) {
    try {
        consumeEvents();
        ULONGLONG elapsed = GetTickCount64()-phaseStart;
        require(elapsed < 12000, ("Native scene probe timed out in phase "+std::to_string(phase)).c_str());
        switch (phase) {
        case 100:
            desired.generation = 1; desired.background_path = probeVideo.c_str(); desired.background_kind = "video";
            desired.audio = ""; desired.display = probeDisplay.c_str(); desired.background_audio = 0;
            desired.stage_enabled = 1; desired.fade_seconds = 1; apply(); next(101); break;
        case 101:
            if (sceneBackground && sceneBackground->playing) {
                require(stageEnabled && IsWindowVisible(backgroundWindow), "Muted background video is not visible");
                require(!sceneBackground->hasAudio, "Muted background unexpectedly requires an audio endpoint");
                retainedToken = sceneBackground->token; lastBackgroundTime = position(sceneBackground.get()); next(102);
            }
            break;
        case 102:
            require(sceneBackground && sceneBackground->token == retainedToken, "Loop replaced the muted background renderer");
            if (double now = position(sceneBackground.get()); now >= 0 && now + .3 < lastBackgroundTime) {
                desired.image_path = probeImage.c_str(); apply(); next(103);
            } else lastBackgroundTime = now;
            break;
        case 103:
            if (sceneImage && visualImage() == sceneImage.get()) {
                require(stageEnabled && !IsWindowVisible(backgroundWindow), "Image overlay did not cover background video");
                desired.foreground_id = desired.generation = 10; desired.foreground_path = probeVideo.c_str();
                desired.foreground_kind = "video"; desired.foreground_has_audio = 0; desired.image_path = "";
                apply(); next(104);
            }
            break;
        case 104:
            if (elapsed < 300) apply();
            if (sceneForeground && sceneForeground->playing) {
                require(IsWindowVisible(sceneForeground->target), "Foreground video was not revealed");
                retainedToken = sceneForeground->token; retainedTime = position(sceneForeground.get());
                desired.stage_enabled = 0; apply(); next(105);
            }
            break;
        case 105:
            if (scene.revision == desired.revision && elapsed > 400) {
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Stage off did not hide foreground video");
                require(sceneForeground && sceneForeground->token == retainedToken, "Stage off replaced foreground video");
                require(position(sceneForeground.get()) > retainedTime+.2, "Stage off stopped the foreground timeline");
                desired.stage_enabled = 1; desired.image_path = probeImage.c_str(); apply(); next(106);
            }
            break;
        case 106:
            if (sceneImage && visualImage() == sceneImage.get()) {
                require(sceneForeground && sceneForeground->token == retainedToken, "Image overlay replaced foreground video");
                desired.foreground_id = 0; desired.generation = 11; desired.foreground_path = ""; desired.foreground_kind = "";
                desired.image_path = ""; desired.background_path = probeImage.c_str(); desired.background_kind = "image";
                apply(); next(107);
            }
            break;
        case 107:
            if (!sceneForeground && sceneBackground && visualImage() == sceneBackground.get()) {
                RECT r; GetClientRect(stageWindow, &r); HDC dc = GetDC(stageWindow);
                COLORREF pixel = GetPixel(dc, r.right/2, r.bottom/2); ReleaseDC(stageWindow, dc);
                require(GetRValue(pixel) > 200 && GetGValue(pixel) < 40 && GetBValue(pixel) < 40, "Background fixture image was not painted");
                SetCursor(LoadCursorW(nullptr, IDC_ARROW));
                SendMessageW(stageWindow, WM_SETCURSOR, (WPARAM)stageWindow, MAKELPARAM(HTCLIENT, WM_MOUSEMOVE));
                require(stageCursor && GetCursor() == stageCursor, "Stage message did not select the transparent cursor");
                SetCursor(LoadCursorW(nullptr, IDC_ARROW));
                SendMessageW(controlWindow, WM_SETCURSOR, (WPARAM)controlWindow, MAKELPARAM(HTCLIENT, WM_MOUSEMOVE));
                require(GetCursor() != stageCursor, "Control window inherited the stage cursor");
                desired.hard_stop = 1; desired.stage_enabled = 0; desired.generation = 12; apply(); next(108);
            }
            break;
        case 108:
            if (scene.revision == desired.revision && elapsed > 200) {
                require(!sceneForeground && !sceneIncoming && !sceneBackground && !sceneRetiring && !sceneImage,
                        "Hard STOP retained scene playback");
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Hard STOP left stage visible");
                passed = true; KillTimer(nullptr, timer); ss_quit();
            }
            break;
        case 0:
            desired.generation = 1; desired.background_path = probeVideo.c_str(); desired.background_kind = "video";
            desired.audio = probeAudio.c_str(); desired.display = probeDisplay.c_str(); desired.background_audio = 1;
            desired.stage_enabled = 1; desired.fade_seconds = 1;
            apply(); next(1); break;
        case 1:
            if (sceneBackground && sceneBackground->playing && sceneBackground->gain > .99f) {
                require(stageEnabled && IsWindowVisible(backgroundWindow), "Background video is not visible");
                checkGain(sceneBackground.get()); retainedToken = sceneBackground->token;
                lastBackgroundTime = position(sceneBackground.get()); next(2);
            }
            break;
        case 2:
            require(sceneBackground && sceneBackground->token == retainedToken, "Loop replaced the background renderer");
            if (double now = position(sceneBackground.get()); now >= 0 && now + .3 < lastBackgroundTime) {
                require(sceneBackground->gain > .99f, "Background loop lost its soundtrack gain");
                desired.generation = desired.foreground_id = 10; desired.foreground_path = probeWaveA.c_str();
                desired.foreground_kind = "audio"; desired.foreground_has_audio = 1;
                apply(); next(3);
            } else lastBackgroundTime = now;
            break;
        case 3:
            // A changing image/background/stage scene revision must never lose
            // a still-preparing foreground whose identity is unchanged.
            if (elapsed < 600) apply();
            if (sceneForeground && sceneForeground->gen == 10 && sceneForeground->playing) {
                checkGain(sceneForeground.get()); checkGain(sceneBackground.get());
                if (sceneForeground->gain > .05f && sceneForeground->gain < .95f && sceneBackground->gain > .05f)
                    observedBackgroundCrossfade = true;
                if (sceneForeground->gain > .99f && sceneBackground->gain < .01f) {
                    require(observedBackgroundCrossfade, "Did not observe background-to-cue crossfade");
                    retainedToken = sceneForeground->token; retainedTime = position(sceneForeground.get());
                    desired.stage_enabled = 0; apply(); next(4);
                }
            }
            break;
        case 4:
            if (scene.revision == desired.revision && elapsed > 400) {
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Stage off did not hide the window");
                require(sceneForeground && sceneForeground->token == retainedToken, "Stage off replaced foreground audio");
                require(position(sceneForeground.get()) > retainedTime+.2, "Foreground timeline stopped with stage off");
                require(sceneForeground->gain > .99f, "Stage off muted foreground sound");
                desired.stage_enabled = 1; desired.image_path = probeImage.c_str(); apply(); next(5);
            }
            break;
        case 5:
            if (sceneImage && visualImage() == sceneImage.get() && sceneBackground && sceneBackground->playing) {
                require(sceneForeground && sceneForeground->token == retainedToken, "Image cue replaced foreground music");
                require(sceneForeground->gain > .99f, "Image cue muted foreground music");
                require(stageEnabled && IsWindowVisible(stageWindow), "Image overlay did not restore stage");
                desired.generation = desired.foreground_id = 11; desired.foreground_path = probeWaveB.c_str();
                desired.image_path = ""; apply(); next(6);
            }
            break;
        case 6:
            if (sceneForeground && sceneForeground->gen == 11 && sceneForeground->playing) {
                checkGain(sceneForeground.get());
                if (sceneRetiring && sceneRetiring->gen == 10 && sceneRetiring->gain > .05f &&
                    sceneForeground->gain > .05f && sceneForeground->gain < .95f) {
                    checkGain(sceneRetiring.get()); observedCueCrossfade = true;
                }
                if (sceneForeground->gain > .99f && !sceneRetiring) {
                    require(observedCueCrossfade, "Did not observe cue-to-cue crossfade");
                    require(!sceneImage && IsWindowVisible(backgroundWindow), "Audio cue did not restore background visual");
                    desired.generation = 12; desired.foreground_id = 0; desired.foreground_path = "";
                    desired.foreground_kind = ""; desired.foreground_has_audio = 0;
                    apply(); next(7);
                }
            }
            break;
        case 7:
            if (sceneRetiring && sceneBackground && sceneRetiring->gain > .05f && sceneBackground->gain > .05f && sceneBackground->gain < .95f) {
                checkGain(sceneRetiring.get()); checkGain(sceneBackground.get()); observedStopCrossfade = true;
            }
            if (scene.revision == desired.revision && !sceneForeground && !sceneRetiring && sceneBackground && sceneBackground->gain > .99f) {
                require(observedStopCrossfade, "STOP did not crossfade back to background soundtrack");
                desired.stage_enabled = 0; apply(); next(8);
            }
            break;
        case 8:
            if (scene.revision == desired.revision && !sceneBackground && !sceneRetiring) {
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Background-only stage off did not complete");
                desired.background_path = probeImage.c_str(); desired.background_kind = "image";
                desired.background_audio = 0; desired.stage_enabled = 1; apply(); next(9);
            }
            break;
        case 9:
            if (sceneBackground && !sceneBackground->pixels.empty() && visualImage() == sceneBackground.get()) {
                require(IsWindowVisible(stageWindow), "Background image window is hidden");
                RECT r; GetClientRect(stageWindow, &r); HDC dc = GetDC(stageWindow);
                COLORREF pixel = GetPixel(dc, r.right/2, r.bottom/2); ReleaseDC(stageWindow, dc);
                require(GetRValue(pixel) > 200 && GetGValue(pixel) < 40 && GetBValue(pixel) < 40, "Stage image was not painted with the fixture pixels");
                SetCursor(LoadCursorW(nullptr, IDC_ARROW));
                SendMessageW(stageWindow, WM_SETCURSOR, (WPARAM)stageWindow, MAKELPARAM(HTCLIENT, WM_MOUSEMOVE));
                require(stageCursor && GetCursor() == stageCursor, "Stage message did not select the transparent cursor");
                SetCursor(LoadCursorW(nullptr, IDC_ARROW));
                SendMessageW(controlWindow, WM_SETCURSOR, (WPARAM)controlWindow, MAKELPARAM(HTCLIENT, WM_MOUSEMOVE));
                require(GetCursor() != stageCursor, "Control window inherited the stage cursor");
                desired.generation = desired.foreground_id = 20; desired.foreground_path = probeWaveA.c_str();
                desired.foreground_kind = "audio"; desired.foreground_has_audio = 1;
                apply(); next(10);
            }
            break;
        case 10:
            if (sceneForeground && sceneForeground->gen == 20 && sceneForeground->playing) {
                // Nothing was audible before this start, so the first observed
                // native Started event must already have full stream gain.
                require(sceneForeground->gain > .99f, "Playback from silence incorrectly faded in");
                retainedToken = sceneForeground->token; retainedTime = position(sceneForeground.get());
                desired.background_path = "C:\\smartstage-probe-background-does-not-exist.png";
                apply(); next(12);
            }
            break;
        case 12:
            if (sawBackgroundError && elapsed > 400) {
                require(sceneForeground && sceneForeground->token == retainedToken && sceneForeground->gain > .99f,
                        "Background failure interrupted foreground music");
                require(position(sceneForeground.get()) > retainedTime+.2, "Background failure stopped foreground timeline");
                desired.generation = desired.foreground_id = 30; desired.foreground_path = probeWaveB.c_str();
                apply(); next(13);
            }
            break;
        case 13:
            // Queue a new foreground, then supersede it before its decoders can
            // become an audible/revealed current session.
            desired.generation = 31; desired.foreground_id = 0; desired.foreground_path = "";
            desired.foreground_kind = ""; desired.foreground_has_audio = 0;
            desired.hard_stop = 1; desired.stage_enabled = 0; apply();
            processSceneCommand(); consumeEvents(); next(14); break;
        case 14:
            if (elapsed > 500) {
                require(!sceneForeground && !sceneIncoming && !sceneBackground && !sceneRetiring && !sceneImage,
                        "Hard STOP retained scene playback");
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Hard STOP left stage visible");
                passed = true; KillTimer(nullptr, timer); ss_quit();
            }
            break;
        }
    } catch (const std::string &error) {
        probeFailure = error; KillTimer(nullptr, timer); ss_quit();
    }
}
}
int main(int argc, char **argv) {
    if (argc != 7) return 2;
    probeAudio = argv[1]; probeDisplay = argv[2]; probeWaveA = argv[3]; probeWaveB = argv[4]; probeVideo = argv[5]; probeImage = argv[6];
    char *error = ss_init();
    if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
    char *inspection = ss_inspect(probeImage.c_str());
    bool validImage = inspection && strstr(inspection, "\"kind\":\"image\"");
    ss_free(inspection);
    if (!validImage) { fprintf(stderr, "WIC image inspection failed\n"); ss_quit(); ss_run(); return 4; }
    soundAvailable = probeAudio != "-";
    if (!soundAvailable) phase = 100;
    phaseStart = GetTickCount64(); SetTimer(nullptr, 0, 20, probeTick); ss_run();
    if (!passed) { fprintf(stderr, "%s\n", probeFailure.c_str()); return 5; }
    if (!soundAvailable) {
        puts("{\"status\":\"passed\",\"nativeStreamGainsReadBack\":false,\"audioFadesVerified\":false,\"audioUnavailableReason\":\"No active runner audio endpoint\",\"backgroundLoop\":true,\"stableForegroundAcrossSceneRevisions\":true,\"stageOffPreservesTimeline\":true,\"imagePreservesForeground\":true,\"imagePixelsRendered\":true,\"stageCursorMessageHandling\":true,\"cursorObservationScope\":\"Synthetic WM_SETCURSOR and current-thread GetCursor only\",\"hardStopClearsScene\":true,\"physicalOutputsVerified\":false}");
        return 0;
    }
    puts("{\"status\":\"passed\",\"nativeStreamGainsReadBack\":true,\"backgroundLoop\":true,\"backgroundToCueCrossfade\":true,\"cueToCueCrossfade\":true,\"stopToBackgroundCrossfade\":true,\"stableForegroundAcrossSceneRevisions\":true,\"stageOffPreservesTimeline\":true,\"imagePreservesMusic\":true,\"imagePixelsRendered\":true,\"stageCursorMessageHandling\":true,\"cursorObservationScope\":\"Synthetic WM_SETCURSOR and current-thread GetCursor only\",\"startFromSilenceImmediate\":true,\"backgroundFailurePreservesMusic\":true,\"hardStopCancelsIncoming\":true,\"physicalOutputsVerified\":false}");
    return 0;
}
