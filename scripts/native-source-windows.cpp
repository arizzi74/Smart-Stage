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

std::string accepted(const char *name, const std::string &path, bool mismatch) {
    currentCase = name;
    HRESULT strict = strictOpen(path);
    currentStrictHRESULT = hexResult(strict);
    if (mismatch) {
        require(strict == MF_E_UNSUPPORTED_BYTESTREAM_TYPE,
                "Renamed WAV did not reproduce the strict resolver's unsupported byte-stream error");
    } else {
        check(strict, "Strict resolver rejected a correctly named fixture");
    }
    bool hasAudio = false;
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
        }
        check(presentation->GetUINT64(MF_PD_DURATION, &duration), "Read opened source duration");
        require(hasAudio, "Opened fixture has no audio stream");
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
           << duration / 10000000.0 << ",\"inspection\":" << inspection << "}";
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
    if (argc != 6) return 2;
    Apartment apartment;
    std::vector<std::string> records;
    std::string error;
    bool started = false;
    try {
        check(apartment.hr, "Initialize source probe COM");
        check(MFStartup(MF_VERSION, MFSTARTUP_FULL), "Initialize source probe Media Foundation");
        started = true;
        records.push_back(accepted("mp3", utf8(argv[1]), false));
        records.push_back(accepted("wav-unicode-path", utf8(argv[2]), false));
        records.push_back(accepted("wav-renamed-mp3", utf8(argv[3]), true));
        records.push_back(rejected("damaged", utf8(argv[4])));
        records.push_back(rejected("missing", utf8(argv[5])));
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
    if (!error.empty()) result << ",\"error\":" << quote(error)
        << ",\"failedFixture\":" << quote(currentCase)
        << ",\"failedFixtureStrictHRESULT\":" << quote(currentStrictHRESULT);
    result << '}';
    puts(result.str().c_str());
    if (!error.empty()) fprintf(stderr, "%s\n", result.str().c_str());
    return error.empty() ? 0 : 1;
}
