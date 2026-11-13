// Test-only source resolution regression probe. This exercises the production
// opener without creating an audio renderer or requiring an output device.
#include "../internal/platform/bridge_windows.cpp"
#include <cstdio>

namespace {
std::string currentCase, currentStrictHRESULT;

struct Source {
    Com<IMFMediaSource> value;
    ~Source() { if (value) value->Shutdown(); }
};

void require(bool condition, const char *message) {
    if (!condition) throw std::string(message);
}

std::string hexResult(HRESULT hr) {
    std::ostringstream out;
    out << "0x" << std::hex << static_cast<uint32_t>(hr);
    return out.str();
}

HRESULT strictOpen(const std::string &path) {
    Com<IMFSourceResolver> resolver;
    check(MFCreateSourceResolver(resolver.out()), "Create strict control resolver");
    Com<IUnknown> object;
    MF_OBJECT_TYPE type = MF_OBJECT_INVALID;
    HRESULT hr = resolver->CreateObjectFromURL(wide(path).c_str(),
        MF_RESOLUTION_MEDIASOURCE | MF_RESOLUTION_READ, nullptr, &type, object.out());
    if (SUCCEEDED(hr)) {
        require(type == MF_OBJECT_MEDIASOURCE, "Strict resolver returned the wrong object type");
        Source source;
        check(object->QueryInterface(__uuidof(IMFMediaSource), (void**)source.value.out()),
              "Get strict control source");
    }
    return hr;
}

std::string inspect(const std::string &path) {
    char *raw = ss_inspect(path.c_str());
    require(raw != nullptr, "Inspection returned no result");
    std::string result(raw);
    ss_free(raw);
    return result;
}

std::string accepted(const char *name, const std::string &path, bool mismatch, bool expectedVideo = false) {
    currentCase = name;
    HRESULT strict = strictOpen(path);
    currentStrictHRESULT = hexResult(strict);
    if (mismatch) {
        require(strict == MF_E_UNSUPPORTED_BYTESTREAM_TYPE,
                "Renamed audio did not reproduce the strict resolver's unsupported byte-stream error");
    } else {
        check(strict, "Strict resolver rejected a correctly named fixture");
    }
    bool hasAudio = false, hasVideo = false;
    UINT64 duration = 0;
    {
        Source source;
        openLocalMedia(path, source.value.out());
        require(static_cast<bool>(source.value), "Production opener returned no source");
        Com<IMFPresentationDescriptor> presentation;
        check(source.value->CreatePresentationDescriptor(presentation.out()), "Read opened source descriptor");
        DWORD count = 0;
        check(presentation->GetStreamDescriptorCount(&count), "Count opened source tracks");
        for (DWORD index = 0; index < count; ++index) {
            BOOL selected = FALSE;
            Com<IMFStreamDescriptor> stream;
            Com<IMFMediaTypeHandler> handler;
            GUID major{};
            check(presentation->GetStreamDescriptorByIndex(index, &selected, stream.out()), "Read opened source track");
            check(stream->GetMediaTypeHandler(handler.out()), "Read opened source media type");
            check(handler->GetMajorType(&major), "Read opened source major type");
            hasAudio = hasAudio || major == MFMediaType_Audio;
            hasVideo = hasVideo || major == MFMediaType_Video;
        }
        check(presentation->GetUINT64(MF_PD_DURATION, &duration), "Read opened source duration");
        require(hasAudio, "Opened fixture has no audio stream");
        require(hasVideo == expectedVideo, "Opened fixture has unexpected video stream presence");
        require(duration > 25000000 && duration < 35000000, "Opened fixture has an unexpected duration");
    }
    // Use a distinct source instance and the actual production decoder probe.
    // Source readers and media sessions must never consume the same source.
    std::string inspection = inspect(path);
    require(inspection.find("\"error\"") == std::string::npos, "Opened fixture failed production decoding inspection");
    std::ostringstream result;
    result << "{\"fixture\":" << quote(name) << ",\"strictHRESULT\":" << quote(hexResult(strict))
           << ",\"strictUnsupportedByteStreamObserved\":" << (strict == MF_E_UNSUPPORTED_BYTESTREAM_TYPE ? "true" : "false")
           << ",\"productionOpenSucceeded\":true,\"sourceHasAudio\":true,\"sourceDuration\":"
           << duration / 10000000.0 << ",\"sourceHasVideo\":" << (hasVideo ? "true" : "false")
           << ",\"inspection\":" << inspection << "}";
    return result.str();
}

std::string repeatedInspection(const std::string &video, const std::string &audio) {
    constexpr unsigned iterations = 16;
    std::ostringstream result;
    result << "{\"status\":\"passed\",\"iterations\":" << iterations << ",\"results\":[";
    for (unsigned iteration = 0; iteration < iterations; ++iteration) {
        currentCase = "repeated-mp4-audio-video-" + std::to_string(iteration + 1);
        currentStrictHRESULT.clear();
        const std::string videoInspection = inspect(video);
        if (videoInspection.find("\"error\"") != std::string::npos)
            throw std::string("Repeated mixed audio/video inspection failed without retry: ") + videoInspection;
        currentCase = "repeated-audio-" + std::to_string(iteration + 1);
        const std::string audioInspection = inspect(audio);
        if (audioInspection.find("\"error\"") != std::string::npos)
            throw std::string("Repeated audio inspection failed without retry: ") + audioInspection;
        if (iteration) result << ',';
        result << "{\"iteration\":" << iteration + 1 << ",\"video\":" << videoInspection
               << ",\"audio\":" << audioInspection << '}';
    }
    result << "]}";
    return result.str();
}

std::string rejected(const char *name, const std::string &path) {
    currentCase = name;
    currentStrictHRESULT.clear();
    std::string error;
    try {
        Source source;
        openLocalMedia(path, source.value.out());
    } catch (const std::string &message) { error = message; }
    require(!error.empty(), "Production opener unexpectedly accepted invalid input");
    std::string inspection = inspect(path);
    require(inspection.find("\"error\"") != std::string::npos,
            "Production inspection unexpectedly accepted invalid input");
    return "{\"fixture\":" + quote(name) + ",\"productionOpenRejected\":true,\"error\":"
        + quote(error) + ",\"inspection\":" + inspection + "}";
}
}

int wmain(int argc, wchar_t **argv) {
    if (argc != 9) return 2;
    Apartment apartment;
    std::vector<std::string> records;
    std::string error, repeated;
    bool started = false;
    try {
        check(apartment.hr, "Initialize source probe COM");
        check(MFStartup(MF_VERSION, MFSTARTUP_FULL), "Initialize source probe Media Foundation");
        started = true;
        records.push_back(accepted("mp3", utf8(argv[1]), false));
        records.push_back(accepted("wav-unicode-path", utf8(argv[2]), false));
        records.push_back(accepted("wav-renamed-mp3", utf8(argv[3]), true));
        records.push_back(accepted("m4a", utf8(argv[4]), false));
        records.push_back(accepted("m4a-renamed-mp3", utf8(argv[5]), true));
        records.push_back(accepted("mp4-audio-video", utf8(argv[8]), false, true));
        records.push_back(rejected("damaged", utf8(argv[6])));
        records.push_back(rejected("missing", utf8(argv[7])));
        repeated = repeatedInspection(utf8(argv[8]), utf8(argv[2]));
    } catch (const std::string &message) { error = message; }
    if (started) MFShutdown();
    std::ostringstream result;
    result << "{\"status\":" << quote(error.empty() ? "passed" : "failed")
           << ",\"requiresAudioEndpoint\":false,\"physicalPlaybackVerified\":false,\"cases\":[";
    for (size_t index = 0; index < records.size(); ++index) {
        if (index) result << ',';
        result << records[index];
    }
    result << ']';
    if (!repeated.empty()) result << ",\"repeatedInspection\":" << repeated;
    if (!error.empty()) result << ",\"error\":" << quote(error)
        << ",\"failedFixture\":" << quote(currentCase)
        << ",\"failedFixtureStrictHRESULT\":" << quote(currentStrictHRESULT);
    result << '}';
    puts(result.str().c_str());
    if (!error.empty()) fprintf(stderr, "%s\n", result.str().c_str());
    return error.empty() ? 0 : 1;
}
