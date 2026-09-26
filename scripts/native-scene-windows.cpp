// Test-only driver includes the production bridge to read renderer state on its
// owning UI thread. No diagnostic endpoints or hooks ship in Smart Stage.
#include "../internal/platform/bridge_windows.cpp"
#include <cstdio>

namespace {
ss_scene_request desired{};
std::string probeAudio, probeDisplay, probeWaveA, probeWaveB, probeVideo, probeImage, probeGreen;
unsigned phase = 0;
ULONGLONG phaseStart = 0;
uint64_t probeRevision = 0, retainedToken = 0;
double lastBackgroundTime = 0, retainedTime = 0;
bool soundAvailable = true;
bool passed = false, observedBackgroundCrossfade = false, observedCueCrossfade = false;
bool observedStopCrossfade = false, sawBackgroundError = false;
bool imageBlendPixels = false, imageBlackPixels = false;
bool movingTargetVideo = false, movingSourceVideo = false, movingBothVideos = false;
double visualClockA = -1, visualClockB = -1;
LONGLONG visualFrameA = 0, visualFrameB = 0;
ULONGLONG visualClockStart = 0;
COLORREF centerPixel(HWND window) {
    RECT r; GetClientRect(window, &r); HDC dc = GetDC(window);
    COLORREF value = GetPixel(dc, r.right/2, r.bottom/2); ReleaseDC(window, dc); return value;
}
void startVisualChecks();
std::string probeFailure;

void require(bool condition, const char *message) { if (!condition) throw std::string(message); }
void next(unsigned value) { phase = value; phaseStart = GetTickCount64(); }
void startVisualChecks() { desired = {}; next(200); }
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
void checkRendererTargets() {
    std::vector<HWND> targets;
    for (Playback *p : {sceneForeground.get(), sceneIncoming.get(), sceneBackground.get(), sceneRetiring.get(), sceneVisualRetiring.get()}) {
        if (!p || !p->video || !p->target) continue;
        require(std::find(targets.begin(), targets.end(), p->target) == targets.end(), "Two live video renderers own the same HWND");
        targets.push_back(p->target);
    }
}
void checkTargetPoolAdmission() {
    // Reserve real renderer windows as a hidden audio tail would. In particular,
    // the background pool must not reuse its first HWND while that tail owns it.
    sceneRetiring = std::make_unique<Playback>(); sceneRetiring->video = true; sceneRetiring->target = backgroundWindows[0];
    require(availableSceneWindow(backgroundWindows) == backgroundWindows[1], "Background admission reused a hidden audio-tail HWND");
    halt(sceneRetiring);
    sceneForeground = std::make_unique<Playback>(); sceneForeground->video = true; sceneForeground->target = sceneVideoWindows[0];
    sceneVisualRetiring = std::make_unique<Playback>(); sceneVisualRetiring->video = true; sceneVisualRetiring->target = sceneVideoWindows[1];
    sceneRetiring = std::make_unique<Playback>(); sceneRetiring->video = true; sceneRetiring->target = sceneVideoWindows[2];
    require(availableSceneWindow(sceneVideoWindows) == sceneVideoWindows[2] && !sceneRetiring,
            "Full target pool did not release its oldest hidden audio tail before reuse");
    halt(sceneForeground); halt(sceneVisualRetiring);
}
void consumeEvents() {
    char *raw;
    while ((raw = ss_poll())) {
        std::string event(raw); ss_free(raw);
        if (event.find("\"kind\":\"background-error\"") != std::string::npos) {
            require(phase == 12 || phase == 218 || phase == 220, "Unexpected background error"); sawBackgroundError = true;
        }
        if (event.find("\"kind\":\"error\"") != std::string::npos) throw std::string("Native scene error: ")+event;
        if (phase == 14 && event.find("\"kind\":\"playing\"") != std::string::npos)
            throw std::string("Canceled foreground started after hard STOP: ")+event;
    }
}
void CALLBACK probeTick(HWND, UINT, UINT_PTR timer, DWORD) {
    try {
        consumeEvents(); checkRendererTargets();
        ULONGLONG elapsed = GetTickCount64()-phaseStart;
        require(elapsed < 12000, ("Native scene probe timed out in phase "+std::to_string(phase)).c_str());
        switch (phase) {
        case 200:
            desired.generation = 100; desired.audio = ""; desired.display = probeDisplay.c_str();
            desired.background_path = probeImage.c_str(); desired.background_kind = "image";
            desired.stage_enabled = 1; desired.fade_seconds = 1.2; apply(); next(201); break;
        case 201:
            if (sceneBackground && visualImage() == sceneBackground.get()) {
                require(!visualTransition.active, "First visual from black should appear immediately");
                require(GetRValue(centerPixel(stageWindow)) > 220, "Initial red image is not painted");
                desired.image_path = probeGreen.c_str(); apply(); next(202);
            }
            break;
        case 202:
            if (visualTransition.active && IsWindowVisible(transitionWindow)) {
                COLORREF pixel = centerPixel(transitionWindow);
                if (GetRValue(pixel) > 40 && GetRValue(pixel) < 215 && GetGValue(pixel) > 40 && GetGValue(pixel) < 215)
                    imageBlendPixels = true;
            }
            if (sceneImage && !visualTransition.active && sceneImage->path == probeGreen) {
                require(imageBlendPixels, "Did not observe actual red/green crossfade pixels");
                require(GetGValue(centerPixel(stageWindow)) > 220, "Image crossfade did not settle on green");
                desired.image_path = ""; desired.background_path = probeVideo.c_str(); desired.background_kind = "video";
                apply(); visualClockStart = 0; next(203);
            }
            break;
        case 203:
            if (visualTransition.active && visualTransition.started && sceneBackground && sceneBackground->video) {
                if (!visualClockStart) { visualClockStart = GetTickCount64(); visualClockA = position(sceneBackground.get()); visualFrameA = visualTransition.target.timestamp; }
                else if (GetTickCount64()-visualClockStart > 220)
                    movingTargetVideo = movingTargetVideo || (std::abs(position(sceneBackground.get())-visualClockA) > .1 &&
                        std::abs(visualTransition.target.timestamp-visualFrameA) > 1000000);
            }
            if (sceneBackground && sceneBackground->video && sceneBackground->playing && !visualTransition.active) {
                require(movingTargetVideo, "Incoming video did not keep moving during image/video crossfade");
                require(!sceneVisualRetiring, "Completed image/video fade retained an outgoing renderer");
                desired.foreground_id = desired.generation = 101; desired.foreground_path = probeVideo.c_str();
                desired.foreground_kind = "video"; desired.foreground_has_audio = 0;
                apply(); visualClockStart = 0; next(204);
            }
            break;
        case 204:
            if (visualTransition.active && visualTransition.started && sceneForeground && sceneBackground) {
                if (!visualClockStart) {
                    visualClockStart = GetTickCount64(); visualClockA = position(sceneBackground.get()); visualClockB = position(sceneForeground.get());
                    visualFrameA = visualTransition.source.timestamp; visualFrameB = visualTransition.target.timestamp;
                } else if (GetTickCount64()-visualClockStart > 220) {
                    // The background loops, so a wrapped clock also demonstrates movement.
                    movingBothVideos = movingBothVideos || (std::abs(position(sceneBackground.get())-visualClockA) > .1 && position(sceneForeground.get()) > visualClockB+.1 &&
                        std::abs(visualTransition.source.timestamp-visualFrameA) > 1000000 && visualTransition.target.timestamp > visualFrameB+1000000);
                }
            }
            if (sceneForeground && sceneForeground->playing && !visualTransition.active) {
                require(movingBothVideos, "Both video timelines did not advance during crossfade");
                desired.image_path = probeGreen.c_str(); desired.foreground_path = desired.foreground_kind = "";
                desired.foreground_id = 0; desired.generation = 102; apply(); visualClockStart = 0; next(205);
            }
            break;
        case 205:
            if (visualTransition.active && visualTransition.started && sceneVisualRetiring && sceneVisualRetiring->video) {
                if (!visualClockStart) { visualClockStart = GetTickCount64(); visualClockA = position(sceneVisualRetiring.get()); visualFrameA = visualTransition.source.timestamp; }
                else if (GetTickCount64()-visualClockStart > 220)
                    movingSourceVideo = movingSourceVideo || (position(sceneVisualRetiring.get()) > visualClockA+.1 &&
                        visualTransition.source.timestamp > visualFrameA+1000000);
            }
            if (sceneImage && sceneImage->path == probeGreen && !visualTransition.active) {
                require(movingSourceVideo, "Outgoing video did not keep moving during video/image crossfade");
                require(!sceneVisualRetiring, "Finished video/image crossfade retained its outgoing video");
                desired.foreground_path = desired.foreground_kind = desired.background_path = desired.background_kind = desired.image_path = "";
                desired.foreground_id = 0; desired.generation = 102; apply(); next(206);
            }
            break;
        case 206:
            if (visualTransition.active && IsWindowVisible(transitionWindow)) {
                COLORREF pixel = centerPixel(transitionWindow);
                if (GetGValue(pixel) > 40 && GetGValue(pixel) < 215 && GetRValue(pixel) < 20 && GetBValue(pixel) < 20)
                    imageBlackPixels = true;
            }
            if (scene.revision == desired.revision && !visualTransition.active && !sceneVisualRetiring) {
                require(imageBlackPixels, "Did not observe image fading to black");
                require(centerPixel(stageWindow) == RGB(0,0,0), "Fade out did not settle on black");
                desired.background_path = probeImage.c_str(); desired.background_kind = "image";
                desired.fade_seconds = 0; apply(); next(207);
            }
            break;
        case 207:
            if (sceneBackground && visualImage() == sceneBackground.get()) {
                require(!visualTransition.active, "Disabled fade animated first image");
                desired.image_path = probeGreen.c_str(); apply(); next(208);
            }
            break;
        case 208:
            if (sceneImage && visualImage() == sceneImage.get()) {
                require(!visualTransition.active && !IsWindowVisible(transitionWindow), "Disabled fade animated image replacement");
                require(GetGValue(centerPixel(stageWindow)) > 220, "Disabled fade did not show new image immediately");
                desired.image_path = ""; desired.fade_seconds = 2; apply(); next(209);
            }
            break;
        case 209:
            if (visualTransition.active && visualTransition.started) {
                desired.image_path = probeGreen.c_str(); apply(); next(210);
            }
            break;
        case 210:
            if (scene.revision == desired.revision && visualTransition.active && visualTransition.frozen) {
                require(!visualTransition.source.empty() && !visualTransition.mixed.empty(), "Rapid replacement lost displayed composite");
                desired.stage_enabled = 0; apply(); processSceneCommand();
                require(!visualTransition.active && !IsWindowVisible(transitionWindow) && !IsWindowVisible(stageWindow),
                        "Stage off did not immediately cancel visual fade");
                desired.stage_enabled = 1; desired.image_path = ""; apply(); next(211);
            }
            break;
        case 211:
            if (sceneBackground && !visualTransition.active && visualImage() == sceneBackground.get()) {
                desired.image_path = probeGreen.c_str(); apply(); next(212);
            }
            break;
        case 212:
            if (visualTransition.active && visualTransition.started) {
                SendMessageW(stageWindow, WM_KEYDOWN, VK_ESCAPE, 0);
                require(!visualTransition.active && !sceneVisualRetiring && !IsWindowVisible(stageWindow),
                        "Escape retained visual fade or stage window");
                desired.stage_enabled = 0; apply(); processSceneCommand();
                desired.stage_enabled = 1; desired.image_path = ""; desired.generation = 103; apply(); next(213);
            }
            break;
        case 213:
            if (sceneBackground && !visualTransition.active && visualImage() == sceneBackground.get()) {
                desired.image_path = probeGreen.c_str(); apply(); next(214);
            }
            break;
        case 214:
            if (visualTransition.active && visualTransition.started) {
                desired.hard_stop = 1; desired.stage_enabled = 0; desired.generation = 104; apply(); processSceneCommand();
                require(!visualTransition.active && visualTransition.mixed.empty() && !sceneVisualRetiring && !IsWindowVisible(stageWindow),
                        "Hard STOP retained visual fade resources");
                next(215);
            }
            break;
        case 215:
            if (elapsed > 300) {
                require(!visualTransition.active && !sceneVisualRetiring && !IsWindowVisible(stageWindow),
                        "A stale native callback resurrected a canceled transition");
                desired.hard_stop = 0; desired.stage_enabled = 1;
                desired.image_path = ""; desired.background_path = probeImage.c_str(); desired.background_kind = "image";
                desired.fade_seconds = .6; apply(); next(217);
            }
            break;
        case 217:
            if (sceneBackground && !visualTransition.active && visualImage() == sceneBackground.get()) {
                sawBackgroundError = false;
                desired.background_path = "C:\\smartstage-probe-background-does-not-exist.png";
                apply(); next(218);
            }
            break;
        case 218:
            if (sawBackgroundError && !visualTransition.active && !sceneVisualRetiring) {
                require(visualTransition.shown == 0 && centerPixel(stageWindow) == RGB(0,0,0),
                        "Failed background left the old visual selected indefinitely");
                desired.background_path = probeImage.c_str(); desired.image_path = probeGreen.c_str(); apply(); next(219);
            }
            break;
        case 219:
            if (sceneImage && sceneBackground && !visualTransition.active && visualImage() == sceneImage.get()) {
                sawBackgroundError = false;
                desired.image_path = "C:\\smartstage-probe-image-does-not-exist.png";
                apply(); next(220);
            }
            break;
        case 220:
            if (sawBackgroundError && !visualTransition.active && !sceneVisualRetiring) {
                require(visualImage() == sceneBackground.get() && GetRValue(centerPixel(stageWindow)) > 220,
                        "Failed image did not return to the saved background");
                passed = true; KillTimer(nullptr, timer); ss_quit();
            }
            break;
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
            if (sceneImage && visualImage() == sceneImage.get() && !visualTransition.active) {
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
            if (!sceneForeground && sceneBackground && visualImage() == sceneBackground.get() && !visualTransition.active) {
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
                require(!sceneForeground && !sceneIncoming && !sceneBackground && !sceneRetiring && !sceneVisualRetiring && !sceneImage,
                        "Hard STOP retained scene playback");
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Hard STOP left stage visible");
                startVisualChecks();
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
                require(!sceneForeground && !sceneIncoming && !sceneBackground && !sceneRetiring && !sceneVisualRetiring && !sceneImage,
                        "Hard STOP retained scene playback");
                require(!stageEnabled && !IsWindowVisible(stageWindow), "Hard STOP left stage visible");
                startVisualChecks();
            }
            break;
        }
    } catch (const std::string &error) {
        probeFailure = error; KillTimer(nullptr, timer); ss_quit();
    }
}
}
int main(int argc, char **argv) {
    if (argc != 8) return 2;
    probeAudio = argv[1]; probeDisplay = argv[2]; probeWaveA = argv[3]; probeWaveB = argv[4]; probeVideo = argv[5]; probeImage = argv[6]; probeGreen = argv[7];
    char *error = ss_init();
    if (error) { fprintf(stderr, "%s\n", error); ss_free(error); return 3; }
    try { checkTargetPoolAdmission(); } catch (const std::string &failure) {
        fprintf(stderr, "%s\n", failure.c_str()); ss_quit(); ss_run(); return 6;
    }
    char *inspection = ss_inspect(probeImage.c_str());
    bool validImage = inspection && strstr(inspection, "\"kind\":\"image\"");
    ss_free(inspection);
    if (!validImage) { fprintf(stderr, "WIC image inspection failed\n"); ss_quit(); ss_run(); return 4; }
    soundAvailable = probeAudio != "-";
    if (!soundAvailable) phase = 100;
    phaseStart = GetTickCount64(); SetTimer(nullptr, 0, 20, probeTick); ss_run();
    if (!passed) { fprintf(stderr, "%s\n", probeFailure.c_str()); return 5; }
    if (!soundAvailable) {
        puts("{\"status\":\"passed\",\"nativeStreamGainsReadBack\":false,\"audioFadesVerified\":false,\"audioUnavailableReason\":\"No active runner audio endpoint\",\"backgroundLoop\":true,\"stableForegroundAcrossSceneRevisions\":true,\"stageOffPreservesTimeline\":true,\"imagePreservesForeground\":true,\"imagePixelsRendered\":true,\"stageCursorMessageHandling\":true,\"cursorObservationScope\":\"Synthetic WM_SETCURSOR and current-thread GetCursor only\",\"hardStopClearsScene\":true,\"visualCrossfadePixelsRendered\":true,\"imageFadeToBlackPixelsRendered\":true,\"videoTimelinesAdvanceDuringFade\":true,\"liveVideoFrameReadbackAdvances\":true,\"exclusiveRendererTargetOwnership\":true,\"zeroDurationVisualSwitchImmediate\":true,\"firstVisualImmediate\":true,\"rapidVisualReplacementBounded\":true,\"stageOffEscapeHardStopCancelVisualFade\":true,\"failedVisualReturnsToBackgroundOrBlack\":true,\"physicalOutputsVerified\":false}");
        return 0;
    }
    puts("{\"status\":\"passed\",\"nativeStreamGainsReadBack\":true,\"backgroundLoop\":true,\"backgroundToCueCrossfade\":true,\"cueToCueCrossfade\":true,\"stopToBackgroundCrossfade\":true,\"stableForegroundAcrossSceneRevisions\":true,\"stageOffPreservesTimeline\":true,\"imagePreservesMusic\":true,\"imagePixelsRendered\":true,\"stageCursorMessageHandling\":true,\"cursorObservationScope\":\"Synthetic WM_SETCURSOR and current-thread GetCursor only\",\"startFromSilenceImmediate\":true,\"backgroundFailurePreservesMusic\":true,\"hardStopCancelsIncoming\":true,\"visualCrossfadePixelsRendered\":true,\"imageFadeToBlackPixelsRendered\":true,\"videoTimelinesAdvanceDuringFade\":true,\"liveVideoFrameReadbackAdvances\":true,\"exclusiveRendererTargetOwnership\":true,\"zeroDurationVisualSwitchImmediate\":true,\"firstVisualImmediate\":true,\"rapidVisualReplacementBounded\":true,\"stageOffEscapeHardStopCancelVisualFade\":true,\"failedVisualReturnsToBackgroundOrBlack\":true,\"physicalOutputsVerified\":false}");
    return 0;
}
