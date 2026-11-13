// Own-process observation of the production Media Foundation bridge. The
// fixture reads actual sessions, clocks and per-stream gains; it does not ship.
#include "../internal/platform/bridge_windows.cpp"
#include <cstdio>
#include <set>
#include <tuple>

namespace {
ss_scene_request wanted{};
std::vector<ss_audio_layer> layers;
std::string audioID, displayID, firstPath, secondPath, videoPath, missingPath;
uint64_t revision = 0, videoToken = 0, firstToken = 0, secondToken = 0, retiredToken = 0, stopRevision = 0;
IMFMediaSession *videoSession = nullptr, *firstSession = nullptr, *secondSession = nullptr;
Com<IMFMediaSession> retiredSession;
unsigned phase = 0;
ULONGLONG phaseStarted = 0, started = 0, preservedFadeStart = 0;
double videoBefore = 0, firstBefore = 0, secondBefore = 0;
LONGLONG videoFrameBefore = 0;
bool passed = false, replacementOverlap = false, stopFade = false, mixedDuringFade = false;
std::string probeFailure;
std::vector<std::string> observations;
std::set<std::tuple<uint64_t,uint64_t,std::string>> events;

void require(bool condition, const char *message) { if (!condition) throw std::string(message); }
void observed(const char *key) { observations.push_back(key); }
void next(unsigned value) { phase = value; phaseStarted = GetTickCount64(); }
Playback *layer(uint64_t id) {
    auto found = sceneAudioLayers.find(id);
    return found == sceneAudioLayers.end() ? nullptr : found->second.player.get();
}
Playback *tail(uint64_t token) {
    for (auto &p : sceneLayerRetiring) if (p->token == token) return p.get();
    return nullptr;
}
ss_audio_layer voice(uint64_t id, const std::string &path, uint64_t transport) {
    ss_audio_layer source{};
    source.id = id; source.path = path.c_str(); source.audio = audioID.c_str();
    source.transport_revision = transport; source.volume = 1;
    return source;
}
void apply(bool hard = false) {
    wanted.revision = ++revision; wanted.generation = 1000+revision;
    wanted.hard_stop = hard;
    auto snapshot = layers;
    wanted.audio_layers = snapshot.empty() ? nullptr : snapshot.data();
    wanted.audio_layer_count = (uint32_t)snapshot.size();
    ss_scene(&wanted);
    // Native ownership must include the caller-owned array, not only strings.
    if (!snapshot.empty()) memset(snapshot.data(), 0, snapshot.size()*sizeof(ss_audio_layer));
    wanted.audio_layers = nullptr;
}
void foreground(uint64_t id, const std::string &path, bool video, uint64_t transport) {
    wanted.foreground_id = id; wanted.foreground_path = path.c_str();
    wanted.foreground_kind = video ? "video" : "audio"; wanted.foreground_has_audio = 1;
    wanted.foreground_muted = wanted.foreground_paused = 0;
    wanted.transport_revision = transport; wanted.seek_revision = 0; wanted.seek_seconds = 0;
}
void clearForeground() {
    wanted.foreground_id = 0; wanted.foreground_path = wanted.foreground_kind = "";
    wanted.foreground_has_audio = wanted.foreground_muted = wanted.foreground_paused = 0;
    wanted.transport_revision = wanted.seek_revision = 0;
}
bool playing(Playback *p) {
    return p && p->playing && !p->paused && !p->transportMuted && p->gain > .99f &&
        p->transportOperation == Playback::TransportOperation::None;
}
bool quietBackground() { return !sceneBackground || sceneBackground->gain < .001f || sceneBackground->transportMuted; }
float nativeGain(Playback *p) {
    require(p && p->streamVolume && !p->silence.empty(), "Missing native independent stream-volume service");
    std::vector<float> channels(p->silence.size());
    require(SUCCEEDED(p->streamVolume->GetAllVolumes((UINT32)channels.size(),channels.data())), "Cannot read actual native stream gain");
    for (float gain : channels) require(std::abs(gain-channels.front()) < .001f, "Native channel gains differ");
    return channels.front();
}
bool shutdown(IMFMediaSession *session) {
    if (!session) return false;
    Com<IMFClock> clock;
    return session->GetClock(clock.out()) == MF_E_SHUTDOWN;
}
bool event(uint64_t id, uint64_t transport, const char *kind) { return events.count({id,transport,kind}) != 0; }
uint64_t number(const std::string &text, const char *key) {
    std::string marker = std::string("\"")+key+"\":";
    auto offset = text.find(marker);
    return offset == std::string::npos ? 0 : std::stoull(text.substr(offset+marker.size()));
}
void consume() {
    while (char *raw = ss_poll()) {
        std::string text(raw); ss_free(raw);
        auto offset = text.find("\"kind\":\"");
        require(offset != std::string::npos, "Malformed native event");
        offset += 8; auto end = text.find('"',offset);
        std::string kind = text.substr(offset,end-offset);
        uint64_t id = number(text,"generation"), transport = number(text,"transportRevision");
        events.emplace(id,transport,kind);
        if ((kind == "error" && id != 53 && id != 54) || kind == "background-error" || kind == "device-lost")
            throw std::string("Unexpected native layer event: ")+text;
        if (stopRevision && number(text,"sceneRevision") >= stopRevision &&
            (kind == "playing" || kind == "paused" || kind == "progress" || kind == "ended"))
            throw std::string("Canceled source emitted a late event: ")+text;
    }
}
void CALLBACK probe(HWND, UINT, UINT_PTR timer, DWORD) {
    if (passed || quitting.load()) return;
    try {
        consume();
        ULONGLONG elapsed = GetTickCount64()-phaseStarted;
        require(GetTickCount64()-started < 85000 && elapsed < 14000,
                ("Native layer probe timed out in phase "+std::to_string(phase)).c_str());
        switch (phase) {
        case 0:
            if (playing(sceneForeground.get()) && !visualTransition.active && scenePosition(*sceneForeground) > .1) {
                VisualFrame frame;
                require(captureVisual(sceneForeground.get(),frame), "Cannot read actual foreground video frame");
                videoToken = sceneForeground->token; videoSession = sceneForeground->session.p;
                videoBefore = scenePosition(*sceneForeground); videoFrameBefore = frame.timestamp;
                layers.push_back(voice(20,firstPath,41)); apply(); next(1);
            }
            break;
        case 1:
            if (playing(layer(20))) {
                VisualFrame frame;
                require(sceneForeground && sceneForeground->token == videoToken && sceneForeground->session.p == videoSession &&
                        playing(sceneForeground.get()) && scenePosition(*sceneForeground) > videoBefore &&
                        visualTransition.shown == videoToken && !visualTransition.active && quietBackground(),
                        "MixAudio restarted video or replaced its soundtrack");
                require(captureVisual(sceneForeground.get(),frame) && frame.timestamp > videoFrameBefore,
                        "MixAudio stopped the actual video frame timeline");
                require(nativeGain(sceneForeground.get()) > .99f && nativeGain(layer(20)) > .99f, "MixAudio did not keep both actual streams audible");
                observed("mixAudioRetainsVideoPlayerFrameTimelineAndSoundtrack");
                // Rewind the three-second fixture through the real transport,
                // leaving time for a separate KeepVideo transition observation.
                wanted.seek_revision = 1; wanted.seek_seconds = 0; wanted.transport_revision = 2;
                apply(); next(21);
            }
            break;
        case 21:
            if (sceneForeground && sceneForeground->transportRevision == 2 &&
                sceneForeground->transportOperation == Playback::TransportOperation::None && scenePosition(*sceneForeground) > .1) {
                retiredToken = layer(20)->token;
                require(SUCCEEDED(layer(20)->session->QueryInterface(__uuidof(IMFMediaSession),(void**)retiredSession.out())), "Cannot retain retired-session observation");
                videoBefore = scenePosition(*sceneForeground);
                wanted.foreground_muted = 1; layers = {voice(30,secondPath,51)};
                apply(); processSceneCommand();
                require(tail(retiredToken) && tail(retiredToken)->awaitingAudioHandoff && nativeGain(tail(retiredToken)) > .99f &&
                        nativeGain(sceneForeground.get()) > .99f, "Preparing replacement cut outgoing audio before native readiness");
                observed("replacementPreparationPreservesOutgoingAudioUntilReady"); next(2);
            }
            break;
        case 2:
            if (layer(30) && tail(retiredToken) && layer(30)->gain > .05f && layer(30)->gain < .95f && tail(retiredToken)->gain > .05f) {
                replacementOverlap = true;
                require(nativeGain(layer(30)) > .04f && nativeGain(tail(retiredToken)) > .04f, "Replacement ramp did not overlap actual stream gains");
                if (!mixedDuringFade) {
                    mixedDuringFade = true; preservedFadeStart = layer(30)->rampStarted;
                    layers.push_back(voice(32,firstPath,62)); apply();
                }
            }
            if (playing(layer(30)) && playing(layer(32)) && sceneLayerRetiring.empty()) {
                require(replacementOverlap && mixedDuringFade && layer(30)->rampStarted == preservedFadeStart,
                        "Replacement crossfade missing, or a Mix addition reset its fade");
                require(sceneForeground && sceneForeground->token == videoToken && sceneForeground->session.p == videoSession &&
                        scenePosition(*sceneForeground) > videoBefore && nativeGain(sceneForeground.get()) < .001f &&
                        visualTransition.shown == videoToken && IsWindowVisible(sceneForeground->target) && quietBackground(),
                        "KeepVideo lost source identity, playhead, frame or sticky soundtrack mute");
                observed("keepVideoRetainsExactPlayerFrameAndPlayheadWhileReplacingSound");
                observed("mixAdditionPreservesExistingAudioFadeEnvelopes");
                // Hold this short video's existing frame for the later, longer
                // independent audio tests, using the real foreground transport.
                wanted.foreground_paused = 1; wanted.transport_revision = 3;
                layers = {voice(30,secondPath,51),voice(31,firstPath,61)};
                apply(); next(3);
            }
            break;
        case 3:
            if (playing(layer(30)) && playing(layer(31)) && sceneForeground && sceneForeground->paused && sceneLayerRetiring.empty() && shutdown(retiredSession.p)) {
                observed("audioLayerReplacementCrossfadesAndReleasesRetiredDecoder");
                firstToken = layer(30)->token; secondToken = layer(31)->token;
                firstSession = layer(30)->session.p; secondSession = layer(31)->session.p;
                layers[0].paused = 1; layers[0].transport_revision = 52; apply(); next(4);
            }
            break;
        case 4:
            if (event(30,52,"paused")) {
                require(layer(30) && layer(30)->paused && layer(30)->transportMuted, "Pause acknowledgement preceded native layer pause");
                firstBefore = scenePosition(*layer(30)); secondBefore = scenePosition(*layer(31)); next(5);
            }
            break;
        case 5:
            if (elapsed > 220) {
                require(layer(30) && layer(31) && layer(30)->token == firstToken && layer(31)->token == secondToken &&
                        layer(30)->session.p == firstSession && layer(31)->session.p == secondSession &&
                        std::abs(scenePosition(*layer(30))-firstBefore) < .04 && nativeGain(layer(30)) < .001f &&
                        playing(layer(31)) && scenePosition(*layer(31)) > secondBefore+.08 &&
                        nativeGain(sceneForeground.get()) < .001f && quietBackground(), "Independent layer pause moved or muted another source");
                observed("audioLayerPauseFreezesOnlyItsOwnDecoderAndClock");
                observed("pausedReplacementCannotRestoreVideoOrBackgroundSound");
                layers[0].paused = 0; layers[0].transport_revision = 53; apply(); next(6);
            }
            break;
        case 6:
            if (event(30,53,"playing") && playing(layer(30)) && scenePosition(*layer(30)) > firstBefore+.08) {
                require(layer(30)->session.p == firstSession && layer(31)->session.p == secondSession && playing(layer(31)), "Resume replaced or interrupted a decoder");
                observed("audioLayerResumeRetainsPlayerAndUsesItsOwnTransportRevision");
                wanted.master_volume = .4; layers[0].volume = .5; layers[1].volume = .75; apply(); next(7);
            }
            break;
        case 7:
            if (scene.revision == wanted.revision && elapsed > 100) {
                require(layer(30)->session.p == firstSession && layer(31)->session.p == secondSession &&
                        std::abs(nativeGain(layer(30))-.2f) < .002f && std::abs(nativeGain(layer(31))-.3f) < .002f &&
                        layer(30)->gain > .999f && layer(31)->gain > .999f && nativeGain(sceneForeground.get()) < .001f,
                        "Master and track volumes changed identity or mis-scaled actual gains");
                observed("masterAndTrackVolumeScaleEveryAudioLayerWithoutRestart");
                firstBefore = scenePosition(*layer(30)); secondBefore = scenePosition(*layer(31));
                wanted.stage_enabled = 0; apply(); next(8);
            }
            break;
        case 8:
            if (scene.revision == wanted.revision && elapsed > 200) {
                require(!stageEnabled && !IsWindowVisible(stageWindow) && layer(30)->session.p == firstSession && layer(31)->session.p == secondSession &&
                        scenePosition(*layer(30)) > firstBefore+.08 && scenePosition(*layer(31)) > secondBefore+.08 && quietBackground(),
                        "Stage off interrupted an audio layer");
                wanted.stage_enabled = 1; apply(); next(9);
            }
            break;
        case 9:
            if (stageEnabled && scene.revision == wanted.revision && elapsed > 150) {
                require(IsWindowVisible(stageWindow) && sceneForeground && sceneForeground->token == videoToken &&
                        sceneForeground->session.p == videoSession && sceneForeground->paused && nativeGain(sceneForeground.get()) < .001f &&
                        std::abs(nativeGain(layer(30))-.2f) < .002f && std::abs(nativeGain(layer(31))-.3f) < .002f && quietBackground(),
                        "Stage on changed source transport, gain or kept video frame");
                observed("stageChangesPreserveAllAudioLayersAndKeptVideo");
                wanted.master_volume = 1; layers[0].volume = layers[1].volume = 1;
                layers[0].transport_revision = 54; layers[0].seek_revision = 1; layers[0].seek_seconds = layer(30)->duration-.25;
                apply(); next(10);
            }
            break;
        case 10:
            if (event(30,54,"ended")) {
                require(!layer(30) && layer(31) && layer(31)->session.p == secondSession && playing(layer(31)) && quietBackground(),
                        "Layer natural end stopped another source or restored background sound");
                observed("audioLayerEndReportsOwnIdentityWithoutStoppingOtherSources"); apply(); next(11);
            }
            break;
        case 11:
            if (elapsed > 180) {
                require(!layer(30) && layer(31) && layer(31)->token == secondToken && playing(layer(31)) && quietBackground(),
                        "A late scene resurrected an ended source");
                observed("lateSceneCannotResurrectEndedAudioLayer"); retiredToken = layer(31)->token;
                require(SUCCEEDED(layer(31)->session->QueryInterface(__uuidof(IMFMediaSession),(void**)retiredSession.out())), "Cannot retain stop-session observation");
                layers.clear(); clearForeground(); apply(); next(12);
            }
            break;
        case 12:
            if (auto *p = tail(retiredToken)) {
                if (p->gain > .05f && p->gain < .95f && nativeGain(p) > .04f) stopFade = true;
            }
            if (sceneAudioLayers.empty() && sceneLayerRetiring.empty() && !sceneForeground && !sceneIncoming && shutdown(retiredSession.p)) {
                require(stopFade && quietBackground(), "STOP did not fade and release audio layers");
                observed("stopFadesAndReleasesAllAudioLayers"); wanted.audio_fade_seconds = .15;
                foreground(50,firstPath,false,70); layers = {voice(51,secondPath,71),voice(52,firstPath,72)};
                apply(); next(13);
            }
            break;
        case 13:
            if (playing(sceneForeground.get()) && playing(layer(51)) && playing(layer(52))) {
                firstSession = layer(51)->session.p; secondSession = layer(52)->session.p;
                firstBefore = scenePosition(*layer(51)); secondBefore = scenePosition(*layer(52));
                wanted.foreground_paused = 1; wanted.transport_revision = 73; apply(); next(14);
            }
            break;
        case 14:
            if (event(50,73,"paused") && elapsed > 200) {
                require(sceneForeground && sceneForeground->paused && nativeGain(sceneForeground.get()) < .001f &&
                        playing(layer(51)) && playing(layer(52)) && scenePosition(*layer(51)) > firstBefore+.08 &&
                        scenePosition(*layer(52)) > secondBefore+.08 && quietBackground(), "Pausing original audio silenced mixed sources");
                observed("mixAudioRetainsOriginalAudioAndIndependentPauseControls");
                layers.push_back(voice(53,missingPath,74)); apply(); next(15);
            }
            break;
        case 15:
            if (event(53,74,"error")) {
                require(!layer(53) && layer(51)->session.p == firstSession && layer(52)->session.p == secondSession &&
                        playing(layer(51)) && playing(layer(52)) && quietBackground(), "Layer error interrupted unrelated sources");
                observed("audioLayerErrorReportsOwnIdentityAndPreservesOtherSources");
                layers.back().paused = 1; layers.back().transport_revision = 76;
                apply(); next(24);
            }
            break;
        case 24:
            if (event(53,76,"error")) {
                require(!layer(53) && playing(layer(51)) && playing(layer(52)), "Failed layer transport acknowledgement disturbed other sources");
                layers.pop_back(); foreground(54,missingPath,false,75); apply(); next(16);
            }
            break;
        case 16:
            if (event(54,75,"error")) {
                require(!sceneForeground && !sceneIncoming && layer(51)->session.p == firstSession && layer(52)->session.p == secondSession &&
                        playing(layer(51)) && playing(layer(52)), "Primary source error tore down mixed audio layers");
                observed("foregroundErrorPreservesIndependentMixedLayers");
                wanted.foreground_paused = 1; wanted.transport_revision = 77; apply(); next(25);
            }
            break;
        case 25:
            if (event(54,77,"error")) {
                require(!sceneForeground && playing(layer(51)) && playing(layer(52)), "Failed primary transport acknowledgement disturbed other sources");
                observed("terminalErrorsAcknowledgeLatestSourceTransport");
                layers.clear(); foreground(60,firstPath,false,90); apply(); next(22);
            }
            break;
        case 22:
            if (playing(sceneForeground.get()) && sceneLayerRetiring.empty()) {
                retiredToken = sceneForeground->token;
                require(SUCCEEDED(sceneForeground->session->QueryInterface(__uuidof(IMFMediaSession),(void**)retiredSession.out())), "Cannot observe primary-audio handoff");
                wanted.audio_fade_seconds = 0; clearForeground(); layers = {voice(61,secondPath,91)};
                apply(); processSceneCommand();
                require(tail(retiredToken) && tail(retiredToken)->awaitingAudioHandoff && nativeGain(tail(retiredToken)) > .99f,
                        "Primary audio was cut before its replacement layer became native-ready with fading disabled");
                next(23);
            }
            break;
        case 23:
            if (playing(layer(61)) && sceneLayerRetiring.empty() && shutdown(retiredSession.p)) {
                observed("primaryAudioToLayerWaitsForNativeReadiness");
                wanted.audio_fade_seconds = .15; clearForeground(); wanted.background_audio = 1;
                layers = {voice(70,firstPath,81),voice(71,secondPath,82)}; apply(); next(17);
            }
            break;
        case 17:
            if (playing(sceneBackground.get()) && playing(layer(70)) && playing(layer(71)) && sceneLayerRetiring.empty()) {
                firstSession = layer(70)->session.p; secondSession = layer(71)->session.p;
                secondBefore = scenePosition(*layer(71));
                layers[0].paused = 1; layers[0].transport_revision = 83; apply(); next(18);
            }
            break;
        case 18:
            if (event(70,83,"paused") && elapsed > 220) {
                require(playing(sceneBackground.get()) && nativeGain(sceneBackground.get()) > .99f &&
                        layer(70)->session.p == firstSession && nativeGain(layer(70)) < .001f &&
                        layer(71)->session.p == secondSession && playing(layer(71)) && scenePosition(*layer(71)) > secondBefore+.08,
                        "MixAudio did not preserve eligible background sound during independent pause");
                observed("mixAudioPreservesEligibleBackgroundSoundtrack");
                layers[1].transport_revision = 84; layers[1].seek_revision = 1; layers[1].seek_seconds = 8;
                apply(); processSceneCommand(); tickScene();
                require(layer(71) && layer(71)->transportOperation == Playback::TransportOperation::Start,
                        "Hard STOP test did not reach a real pending native seek");
                layers.clear(); clearForeground(); wanted.stage_enabled = 0; apply(true);
                stopRevision = wanted.revision; processSceneCommand(); next(19);
            }
            break;
        case 19:
            if (elapsed > 400) {
                require(sceneAudioLayers.empty() && sceneLayerRetiring.empty() && !sceneForeground && !sceneIncoming && !sceneBackground &&
                        !sceneRetiring && !sceneVisualRetiring && !stageEnabled && !IsWindowVisible(stageWindow), "Hard STOP retained a source or stage");
                { std::lock_guard<std::mutex> lock(workerMutex); require(audioLayerTokens.empty(), "Hard STOP retained a valid layer token"); }
                observed("hardStopDuringLayerSeekDisposesAllSourcesAndRejectsLateEvents");
                passed = true; KillTimer(nullptr,timer); ss_quit();
            }
            break;
        }
    } catch (const std::string &message) {
        probeFailure = message; KillTimer(nullptr,timer); ss_quit();
    }
}
}
int main(int argc, char **argv) {
    if (argc != 6) return 2;
    audioID = argv[1]; displayID = argv[2]; firstPath = argv[3]; secondPath = argv[4]; videoPath = argv[5];
    if (audioID == "-") {
        puts("{\"status\":\"skipped\",\"reason\":\"No active runner audio endpoint; native stream mixing was not exercised\",\"observations\":[],\"physicalAudioVerified\":false}");
        return 0;
    }
    missingPath = firstPath+".missing";
    if (char *message = ss_init()) { fprintf(stderr,"%s\n",message); ss_free(message); return 3; }
    wanted.audio = audioID.c_str(); wanted.display = displayID.c_str(); wanted.stage_enabled = 1;
    wanted.master_volume = wanted.foreground_volume = wanted.background_volume = 1;
    wanted.background_path = videoPath.c_str(); wanted.background_kind = "video"; wanted.background_audio = 0;
    wanted.audio_fade_seconds = .35; wanted.visual_fade_seconds = .1;
    foreground(10,videoPath,true,1); apply();
    started = phaseStarted = GetTickCount64(); SetTimer(nullptr,0,20,probe); ss_run();
    if (!passed) { fprintf(stderr,"Native layer phase %u: %s\n",phase,probeFailure.c_str()); return 1; }
    std::ostringstream report;
    report << "{\"status\":\"passed\",\"observations\":[";
    for (size_t i=0; i<observations.size(); ++i) {
        if (i) report << ',';
        report << '{' << quote(observations[i]) << ":true}";
    }
    report << "],\"method\":\"Production Windows bridge; actual Media Foundation session identity, native clocks and video frame timestamps, stream gain readback, shutdown and source events. No physical audio measurement.\",\"physicalAudioVerified\":false}";
    puts(report.str().c_str());
    return 0;
}
