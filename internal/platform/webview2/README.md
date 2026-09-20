# Pinned Microsoft WebView2 loader

SDK version: **1.0.4191.47**. The unmodified SDK header and the official x64/ARM64 loader DLL bytes are vendored from Microsoft's NuGet package. The case-compatible `EventToken.h` shim supports Linux cross-compilation. No runtime loader download occurs. The loader is extracted to a new random per-user directory with a restrictive ACL, opened without write/delete sharing, and loaded by its full path using system-only dependency search. The browser runtime is Microsoft Evergreen WebView2, independently installed and maintained by Microsoft.

Package: https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/1.0.4191.47/microsoft.web.webview2.1.0.4191.47.nupkg

SHA-256 package: `f492bbf547d0da329553b6727435b677579b1e9f91cc9e4a1ad029366d5f23d0`

- amd64 original loader DLL SHA-256: `c66e4a92fdc7a216118e43b7a5024ea2200e8c43f9310bf20d96a0084f82c5bc`
- arm64 original loader DLL SHA-256: `5a5cf33bdce364d72cbb56d59e24e5cf77ee38976d4f8b392a151e0013215703`

See [LICENSE.txt](LICENSE.txt). The single executable embeds the loader, not the separately serviced Evergreen browser runtime. The static library in this SDK requires the MSVC C++ ABI; our LLVM-MinGW build uses the official DLL loader instead.

Primary implementation references:
- https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/threading-model
- https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution
- https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2compositioncontroller
- https://github.com/MicrosoftEdge/WebView2Samples/tree/main/SampleApps/WebView2APISample
