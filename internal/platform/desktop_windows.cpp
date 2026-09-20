//go:build windows && cgo

// The UI owns a separate STA. Media Foundation retains its original MTA.
// No native callback enters Go: imports, reopen and quit requests are polled.
#include "bridge.h"
#include <windows.h>
#include <windowsx.h>
#include <shellapi.h>
#include <shobjidl.h>
#include <shlobj.h>
#include <sddl.h>
#include <bcrypt.h>
#include <dcomp.h>
#include <io.h>
#include "webview2/WebView2.h"
#include "webview2/uuid_mingw.h"
#include <atomic>
#include <condition_variable>
#include <deque>
#include <filesystem>
#include <functional>
#include <map>
#include <mutex>
#include <sstream>
#include <string>
#include <thread>
#include <vector>
#if defined(_M_ARM64) || defined(__aarch64__)
#include "webview2/loader_arm64.inc"
#else
#include "webview2/loader_amd64.inc"
#endif

namespace desktop {
template<class T> struct Ptr {
    T* p = nullptr;
    Ptr() = default;
    Ptr(const Ptr&) = delete;
    Ptr& operator=(const Ptr&) = delete;
    Ptr(Ptr&& other) noexcept: p(other.p) { other.p = nullptr; }
    ~Ptr() { reset(); }
    void reset(T* value = nullptr) { if (p) p->Release(); p = value; }
    T** out() { reset(); return &p; }
    T* operator->() const { return p; }
    explicit operator bool() const { return p != nullptr; }
};
std::wstring wide(const char* s) {
    if (!s) return {};
    int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s, -1, nullptr, 0);
    if (n <= 0) return {};
    std::wstring value(n, 0);
    MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s, -1, value.data(), n);
    value.pop_back(); return value;
}
std::string utf8(const wchar_t* s) {
    if (!s) return {};
    int n = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, s, -1, nullptr, 0, nullptr, nullptr);
    if (n <= 0) return {};
    std::string value(n, 0);
    WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, s, -1, value.data(), n, nullptr, nullptr);
    value.pop_back(); return value;
}
std::string quote(const std::string& value) {
    const char* hex = "0123456789abcdef";
    std::string out = "\"";
    for (unsigned char c : value) {
        if (c == '"' || c == '\\') { out += '\\'; out += c; }
        else if (c < 32) { out += "\\u00"; out += hex[c >> 4]; out += hex[c & 15]; }
        else out += c;
    }
    return out + '"';
}
void check(HRESULT hr, const char* action) {
    if (FAILED(hr)) { std::ostringstream s; s << action << " (0x" << std::hex << uint32_t(hr) << ")"; throw s.str(); }
}
constexpr UINT wakeMessage = WM_APP+81, trayMessage = WM_APP+82, shutdownMessage = WM_APP+83;
constexpr UINT openID = 101, chooseID = 102, quitID = 103, reloadID = 104, logsID = 105, browserID = 106;
std::atomic<HWND> control{nullptr};
HWND window = nullptr, errorLabel = nullptr, retryButton = nullptr;
std::wstring identity = L"SmartStageAdmin-default", adminURL, origin, logPath;
std::wstring directory, loaderPath;
std::wstring errorText = L"Starting Smart Stage…";
std::atomic<bool> ready{false}, stopping{false}, showPending{false}, adminRequested{false}, quitRequested{false}, chooserScheduled{false};
std::atomic<bool> emergencyRequested{false};
std::thread uiThread;
std::once_flag startOnce;
std::mutex initMutex, tasksMutex, filesMutex;
std::condition_variable initCV;
bool initialized = false, uiQuit = false, creating = false, trayAdded = false, mouseTracking = false;
bool chooserActive = false, chooserShowing = false;
std::deque<std::function<void()>> tasks;
struct FileRequest { uint64_t id; std::vector<std::string> paths; };
std::deque<FileRequest> files;
std::map<uint64_t,size_t> pendingFiles;
uint64_t nextFileID = 1;
size_t pendingCount = 0;
Ptr<ICoreWebView2Environment> environment;
Ptr<ICoreWebView2Controller> controller;
Ptr<ICoreWebView2CompositionController> composition;
Ptr<ICoreWebView2> webview;
Ptr<IDCompositionDevice> dcomp;
Ptr<IDCompositionTarget> target;
Ptr<IDCompositionVisual> visual;
Ptr<IFileOpenDialog> chooser;
HMODULE loader = nullptr;
HANDLE loaderFile = INVALID_HANDLE_VALUE;
HANDLE browserProcess = nullptr;
NOTIFYICONDATAW tray{};
UINT taskbarCreated = 0;
unsigned trayAttempts = 0;
DWORD trayAddError = 0;

void showError(const std::string& message) {
    errorText = wide(message.c_str());
    if(controller)controller->put_IsVisible(FALSE);
    if (errorLabel) { SetWindowTextW(errorLabel, errorText.c_str()); ShowWindow(errorLabel, SW_SHOW); ShowWindow(retryButton, SW_SHOW); }
    fprintf(stderr, "Native Admin: %s\n", message.c_str());
}
template<class Interface, class... Args> class Handler final : public Interface {
    std::atomic<ULONG> refs{1};
    std::function<HRESULT(Args...)> fn;
public:
    explicit Handler(std::function<HRESULT(Args...)> f): fn(std::move(f)) {}
    HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void** out) override {
        if (!out) return E_POINTER;
        *out = nullptr;
        if (iid == __uuidof(IUnknown) || iid == __uuidof(Interface)) { *out = static_cast<Interface*>(this); AddRef(); return S_OK; }
        return E_NOINTERFACE;
    }
    ULONG STDMETHODCALLTYPE AddRef() override { return ++refs; }
    ULONG STDMETHODCALLTYPE Release() override { ULONG n = --refs; if (!n) delete this; return n; }
    HRESULT STDMETHODCALLTYPE Invoke(Args... args) override {
        if (stopping.load()) return S_OK;
        try { return fn(args...); }
        catch (const std::string& error) { showError(error); return E_FAIL; }
        catch (...) { showError("An unexpected error occurred in the Admin window."); return E_FAIL; }
    }
};
template<class Interface, class... Args, class F> Ptr<Interface> callback(F fn) {
    Ptr<Interface> p; p.p = new Handler<Interface, Args...>(fn); return p;
}

bool validAdmin(const std::wstring& value) {
    const std::wstring prefix = L"http://127.0.0.1:";
    if (value.compare(0, prefix.size(), prefix)) return false;
    auto end = value.find(L'/', prefix.size());
    if (end == std::wstring::npos || value.substr(end) != L"/admin" || end - prefix.size() > 5) return false;
    unsigned port = 0;
    for (size_t i=prefix.size(); i<end; ++i) { if (value[i] < L'0' || value[i] > L'9') return false; port = port*10+unsigned(value[i]-L'0'); }
    return port > 0 && port <= 65535;
}
bool sameAdmin(std::wstring value) { auto hash = value.find(L'#'); if (hash != std::wstring::npos) value.resize(hash); return value == adminURL; }
bool localResource(const std::wstring& value) {
    if (origin.empty() || value.compare(0, origin.size()+1, origin+L"/")) return false;
    auto path = value.substr(origin.size());
    auto end = path.find_first_of(L"?#"); if (end != std::wstring::npos) path.resize(end);
    if (path.find(L'%') != std::wstring::npos || path.find(L'\\') != std::wstring::npos || path.find(L"..") != std::wstring::npos) return false;
    return path == L"/admin" || path == L"/favicon.ico" || path == L"/licenses.txt" || path.compare(0,8,L"/assets/") == 0 || path.compare(0,5,L"/api/") == 0;
}
bool externalURL(const std::wstring& value) {
    auto start = value.compare(0,8,L"https://") == 0 ? 8u : value.compare(0,7,L"http://") == 0 ? 7u : 0u;
    if (!start || value.size() > 8192 || value.find_first_of(L"\r\n\t\\") != std::wstring::npos) return false;
    auto end = value.find_first_of(L"/?#", start); auto host = value.substr(start, end-start);
    return !host.empty() && host.find(L'@') == std::wstring::npos;
}
bool onAdmin() { LPWSTR source = nullptr; if (!webview || FAILED(webview->get_Source(&source))) return false; bool valid = sameAdmin(source ? source : L""); CoTaskMemFree(source); return valid; }
void openExternal(const wchar_t* url, BOOL user) {
    if (user && url && onAdmin() && externalURL(url)) ShellExecuteW(window, L"open", url, nullptr, nullptr, SW_SHOWNORMAL);
}
void showWindow();
void createWebView();
void shutdownUI();
void resize() {
    RECT bounds{}; GetClientRect(window, &bounds);
    if (controller) controller->put_Bounds(bounds);
    if (errorLabel) MoveWindow(errorLabel, 20, 22, std::max(100L,bounds.right-160), 72, TRUE);
    if (retryButton) MoveWindow(retryButton, std::max(20L,bounds.right-130), 28, 112, 30, TRUE);
}
bool queueFiles(std::vector<std::string> paths) {
    if (paths.empty() || stopping.load()) return false;
    if (paths.size() > 500) { showError("Add no more than 500 files at once."); return false; }
    size_t bytes = 0;
    for (const auto& path : paths) {
        bytes += path.size();
        bool drive = path.size() > 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/');
        bool unc = path.size() > 2 && path[0] == '\\' && path[1] == '\\';
        if ((!drive && !unc) || path.size() > 131072 || bytes > 4*1024*1024) { showError("Choose original files with valid absolute Windows paths."); return false; }
    }
    { std::lock_guard<std::mutex> lock(filesMutex);
      if (pendingFiles.size() >= 8 || pendingCount + paths.size() > 500) { showError("Wait for the files already being added, then try again."); return false; }
      auto id = nextFileID++; pendingFiles[id] = paths.size(); pendingCount += paths.size(); files.push_back({id,std::move(paths)}); }
    adminRequested.store(true); showWindow();
    return true;
}
class DropTarget final : public IDropTarget {
    std::atomic<ULONG> refs{1}; bool acceptable = false;
    static FORMATETC format() { FORMATETC f{}; f.cfFormat=CF_HDROP; f.dwAspect=DVASPECT_CONTENT; f.lindex=-1; f.tymed=TYMED_HGLOBAL; return f; }
public:
    HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void** out) override { if (!out) return E_POINTER; *out=nullptr; if (iid==__uuidof(IUnknown)||iid==__uuidof(IDropTarget)) { *out=this; AddRef(); return S_OK; } return E_NOINTERFACE; }
    ULONG STDMETHODCALLTYPE AddRef() override { return ++refs; }
    ULONG STDMETHODCALLTYPE Release() override { auto n=--refs; if(!n)delete this; return n; }
    HRESULT STDMETHODCALLTYPE DragEnter(IDataObject* object, DWORD, POINTL, DWORD* effect) override { auto f=format(); acceptable=object && object->QueryGetData(&f)==S_OK && ready.load() && !stopping.load(); *effect=acceptable ? (*effect & DROPEFFECT_COPY) : DROPEFFECT_NONE; return S_OK; }
    HRESULT STDMETHODCALLTYPE DragOver(DWORD, POINTL, DWORD* effect) override { *effect=acceptable ? (*effect & DROPEFFECT_COPY) : DROPEFFECT_NONE; return S_OK; }
    HRESULT STDMETHODCALLTYPE DragLeave() override { acceptable=false; return S_OK; }
    HRESULT STDMETHODCALLTYPE Drop(IDataObject* object, DWORD, POINTL, DWORD* effect) override {
        *effect=DROPEFFECT_NONE; acceptable=false;
        if (!object || !ready.load() || stopping.load()) return S_OK;
        auto f=format(); STGMEDIUM data{};
        if (FAILED(object->GetData(&f,&data))) return S_OK;
        std::vector<std::string> paths;
        auto drop=(HDROP)data.hGlobal; UINT count=DragQueryFileW(drop,0xffffffff,nullptr,0);
        if (count <= 500) for(UINT i=0;i<count;++i) { UINT n=DragQueryFileW(drop,i,nullptr,0); if(n>32767) { paths.clear();break; } std::wstring path(n+1,0); DragQueryFileW(drop,i,path.data(),n+1); paths.push_back(utf8(path.c_str())); }
        ReleaseStgMedium(&data);
        try { if (count > 500) showError("Add no more than 500 files at once."); else if (queueFiles(std::move(paths))) *effect=DROPEFFECT_COPY; } catch (...) { showError("The selected files could not be added."); }
        return S_OK;
    }
};
Ptr<IDropTarget> dropTarget;

void chooseMedia() {
    chooserScheduled.store(false);
    if (!ready.load() || stopping.load() || chooserActive) return;
    showWindow();
    chooserActive=true;
    struct FinishChooser {
        ~FinishChooser() {
            chooserShowing=false;chooserActive=false;chooser.reset();
            // Also runs after COM exceptions before or after Show. A pending
            // shutdown must never wait forever on a stale chooser pointer.
            if(stopping.load())shutdownUI();
        }
    } finish;
    check(CoCreateInstance(CLSID_FileOpenDialog,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(chooser.out())),"Open the Windows media chooser");
    FILEOPENDIALOGOPTIONS flags{}; check(chooser->GetOptions(&flags),"Read chooser options");
    check(chooser->SetOptions(flags|FOS_ALLOWMULTISELECT|FOS_FORCEFILESYSTEM|FOS_FILEMUSTEXIST|FOS_PATHMUSTEXIST|FOS_NOCHANGEDIR),"Configure the media chooser");
    COMDLG_FILTERSPEC filters[]={{L"Audio, video and images",L"*.mp3;*.wav;*.m4a;*.aac;*.flac;*.aiff;*.wma;*.mp4;*.m4v;*.mov;*.wmv;*.avi;*.mkv;*.jpg;*.jpeg;*.png;*.bmp;*.gif;*.tif;*.tiff;*.webp"},{L"All files",L"*.*"}};
    chooser->SetFileTypes(2,filters); chooser->SetTitle(L"Choose media for Smart Stage — files stay in their original folders"); chooser->SetOkButtonLabel(L"Add to Show");
    if(stopping.load())return;
    fprintf(stderr,"Opened native media chooser\n");
    chooserShowing=true;
    HRESULT result=chooser->Show(window);
    chooserShowing=false;
    if(stopping.load())fprintf(stderr,"Native media chooser returned during shutdown: 0x%lx\n",(unsigned long)result);
    if (SUCCEEDED(result) && !stopping.load()) {
        Ptr<IShellItemArray> items; check(chooser->GetResults(items.out()),"Read selected files"); DWORD count=0; items->GetCount(&count);
        std::vector<std::string> paths;
        if(count>500)showError("Add no more than 500 files at once.");
        else for(DWORD i=0;i<count;++i) { Ptr<IShellItem> item; LPWSTR path=nullptr; if(SUCCEEDED(items->GetItemAt(i,item.out())) && SUCCEEDED(item->GetDisplayName(SIGDN_FILESYSPATH,&path))) { paths.push_back(utf8(path)); CoTaskMemFree(path); } }
        queueFiles(std::move(paths));
    } else if (result==HRESULT_FROM_WIN32(ERROR_CANCELLED)) fprintf(stderr,"Cancelled native media chooser\n");
}
std::wstring randomDirectory() {
    PWSTR local=nullptr; check(SHGetKnownFolderPath(FOLDERID_LocalAppData,KF_FLAG_CREATE,nullptr,&local),"Find local application data");
    std::wstring base=std::wstring(local)+L"\\SmartStage"; CoTaskMemFree(local);
    HANDLE token=nullptr; if(!OpenProcessToken(GetCurrentProcess(),TOKEN_QUERY,&token)) throw std::string("Read current user identity");
    DWORD length=0; GetTokenInformation(token,TokenUser,nullptr,0,&length); std::vector<BYTE> buffer(length);
    BOOL ok=GetTokenInformation(token,TokenUser,buffer.data(),length,&length); CloseHandle(token); if(!ok)throw std::string("Read current user identity");
    LPWSTR sid=nullptr; if(!ConvertSidToStringSidW(((TOKEN_USER*)buffer.data())->User.Sid,&sid))throw std::string("Read current user SID");
    std::wstring acl=L"D:P(A;;FA;;;SY)(A;;FA;;;"+std::wstring(sid)+L")"; LocalFree(sid);
    PSECURITY_DESCRIPTOR descriptor=nullptr;
    if(!ConvertStringSecurityDescriptorToSecurityDescriptorW(acl.c_str(),SDDL_REVISION_1,&descriptor,nullptr))throw std::string("Protect WebView storage");
    SECURITY_ATTRIBUTES security{sizeof(security),descriptor,FALSE};
    CreateDirectoryW(base.c_str(),&security);
    unsigned char random[16]; auto status=BCryptGenRandom(nullptr,random,sizeof(random),BCRYPT_USE_SYSTEM_PREFERRED_RNG);
    if(status<0) { LocalFree(descriptor);throw std::string("Generate private WebView storage name"); }
    const wchar_t* hex=L"0123456789abcdef"; std::wstring name=base+L"\\session-";
    for(auto value:random) {name+=hex[value>>4]; name+=hex[value&15];}
    ok=CreateDirectoryW(name.c_str(),&security); LocalFree(descriptor);
    if(!ok)throw std::string("Create private WebView storage");
    return name;
}
using CreateEnvironment = HRESULT(STDMETHODCALLTYPE*)(PCWSTR,PCWSTR,ICoreWebView2EnvironmentOptions*,ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler*);
CreateEnvironment loadSDK() {
    if(loader)return reinterpret_cast<CreateEnvironment>(GetProcAddress(loader,"CreateCoreWebView2EnvironmentWithOptions"));
    directory=randomDirectory(); loaderPath=directory+L"\\WebView2Loader.dll";
    loaderFile=CreateFileW(loaderPath.c_str(),GENERIC_READ|GENERIC_WRITE,0,nullptr,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,nullptr);
    if(loaderFile==INVALID_HANDLE_VALUE)throw std::string("Create the embedded WebView2 loader");
    DWORD written=0;
    if(!WriteFile(loaderFile,loaderBytes,sizeof(loaderBytes),&written,nullptr) || written!=sizeof(loaderBytes) || !FlushFileBuffers(loaderFile))throw std::string("Write the embedded WebView2 loader");
    CloseHandle(loaderFile);
    loaderFile=CreateFileW(loaderPath.c_str(),GENERIC_READ,FILE_SHARE_READ,nullptr,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,nullptr);
    if(loaderFile==INVALID_HANDLE_VALUE)throw std::string("Protect the embedded WebView2 loader");
    std::vector<unsigned char> verified(sizeof(loaderBytes)); DWORD count=0; LARGE_INTEGER size{};
    if(!GetFileSizeEx(loaderFile,&size) || size.QuadPart!=sizeof(loaderBytes) || !ReadFile(loaderFile,verified.data(),DWORD(verified.size()),&count,nullptr) || count!=verified.size() || memcmp(verified.data(),loaderBytes,sizeof(loaderBytes)))throw std::string("Embedded WebView2 loader verification failed");
    // Verify the embedded bytes after reopening, then keep the read handle
    // open without write/delete sharing until the DLL unloads.
    loader=LoadLibraryExW(loaderPath.c_str(),nullptr,LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|LOAD_LIBRARY_SEARCH_SYSTEM32);
    if(!loader)throw std::string("Load the embedded Microsoft WebView2 loader");
    auto available=reinterpret_cast<HRESULT(STDMETHODCALLTYPE*)(PCWSTR,LPWSTR*)>(GetProcAddress(loader,"GetAvailableCoreWebView2BrowserVersionString"));
    LPWSTR version=nullptr; HRESULT result=available?available(nullptr,&version):E_NOINTERFACE; CoTaskMemFree(version);
    if(FAILED(result))throw std::string("Microsoft Edge WebView2 Runtime is missing. Quit Smart Stage, run the Smart Stage Windows installer, then reopen the app. The Smart Stage menu also offers Open Admin in Browser.");
    auto create=reinterpret_cast<CreateEnvironment>(GetProcAddress(loader,"CreateCoreWebView2EnvironmentWithOptions"));
    if(!create)throw std::string("The embedded WebView2 loader is incomplete");
    return create;
}
void configureWebView() {
    check(composition->QueryInterface(IID_PPV_ARGS(controller.out())),"Get Admin controller");
    check(controller->get_CoreWebView2(webview.out()),"Get Admin WebView");
    Ptr<ICoreWebView2Settings> settings; check(webview->get_Settings(settings.out()),"Read WebView settings");
    settings->put_AreDevToolsEnabled(FALSE); settings->put_AreDefaultContextMenusEnabled(FALSE); settings->put_IsStatusBarEnabled(FALSE);
    settings->put_IsWebMessageEnabled(FALSE); settings->put_AreHostObjectsAllowed(FALSE);
    Ptr<ICoreWebView2Settings2> settings2;
    check(settings->QueryInterface(IID_PPV_ARGS(settings2.out())),"Configure native Admin identity");
    LPWSTR agent=nullptr; check(settings2->get_UserAgent(&agent),"Read browser identity");
    std::wstring userAgent=std::wstring(agent?agent:L"")+L" SmartStageDesktop SmartStageWindowsDesktop"; CoTaskMemFree(agent);
    check(settings2->put_UserAgent(userAgent.c_str()),"Set native Admin identity");
    // The page has ordinary HTTP session/CSRF authentication. No filesystem
    // host objects, message bridge, injected privileged script, or file URLs.
    EventRegistrationToken token{};
    auto navigation=callback<ICoreWebView2NavigationStartingEventHandler,ICoreWebView2*,ICoreWebView2NavigationStartingEventArgs*>([](auto*,auto* args) {
        LPWSTR url=nullptr; args->get_Uri(&url); BOOL user=FALSE; args->get_IsUserInitiated(&user);
        if(!url || !sameAdmin(url)) { args->put_Cancel(TRUE); openExternal(url,user); }
        CoTaskMemFree(url); return S_OK;
    });
    check(webview->add_NavigationStarting(navigation.p,&token),"Protect Admin navigation");
    auto frame=callback<ICoreWebView2NavigationStartingEventHandler,ICoreWebView2*,ICoreWebView2NavigationStartingEventArgs*>([](auto*,auto* args) { args->put_Cancel(TRUE); return S_OK; });
    check(webview->add_FrameNavigationStarting(frame.p,&token),"Disable Admin frames");
    auto popup=callback<ICoreWebView2NewWindowRequestedEventHandler,ICoreWebView2*,ICoreWebView2NewWindowRequestedEventArgs*>([](auto*,auto* args) {
        args->put_Handled(TRUE); LPWSTR url=nullptr; BOOL user=FALSE; args->get_Uri(&url); args->get_IsUserInitiated(&user); openExternal(url,user); CoTaskMemFree(url); return S_OK;
    });
    check(webview->add_NewWindowRequested(popup.p,&token),"Handle explicit external links");
    auto permission=callback<ICoreWebView2PermissionRequestedEventHandler,ICoreWebView2*,ICoreWebView2PermissionRequestedEventArgs*>([](auto*,auto* args) { args->put_State(COREWEBVIEW2_PERMISSION_STATE_DENY); return S_OK; });
    check(webview->add_PermissionRequested(permission.p,&token),"Disable device permissions");
    check(webview->AddWebResourceRequestedFilter(L"*",COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL),"Restrict Admin resources");
    auto resource=callback<ICoreWebView2WebResourceRequestedEventHandler,ICoreWebView2*,ICoreWebView2WebResourceRequestedEventArgs*>([](auto*,auto* args) {
        Ptr<ICoreWebView2WebResourceRequest> request; check(args->get_Request(request.out()),"Inspect Admin resource"); LPWSTR uri=nullptr; request->get_Uri(&uri);
        bool allowed=uri && localResource(uri); CoTaskMemFree(uri);
        if(!allowed) { Ptr<ICoreWebView2WebResourceResponse> response; check(environment->CreateWebResourceResponse(nullptr,403,L"Forbidden",L"Content-Type: text/plain\r\n",response.out()),"Block external resource"); args->put_Response(response.p); }
        return S_OK;
    });
    check(webview->add_WebResourceRequested(resource.p,&token),"Protect Admin resources");
    Ptr<ICoreWebView2_4> webview4;
    if(SUCCEEDED(webview->QueryInterface(IID_PPV_ARGS(webview4.out())))) {
        auto download=callback<ICoreWebView2DownloadStartingEventHandler,ICoreWebView2*,ICoreWebView2DownloadStartingEventArgs*>([](auto*,auto* args) { args->put_Cancel(TRUE); args->put_Handled(TRUE); return S_OK; });
        check(webview4->add_DownloadStarting(download.p,&token),"Disable WebView downloads");
    }
    auto completed=callback<ICoreWebView2NavigationCompletedEventHandler,ICoreWebView2*,ICoreWebView2NavigationCompletedEventArgs*>([](auto*,auto* args) {
        BOOL success=FALSE; args->get_IsSuccess(&success);
        COREWEBVIEW2_WEB_ERROR_STATUS status{}; args->get_WebErrorStatus(&status);
        if(!success && status==COREWEBVIEW2_WEB_ERROR_STATUS_OPERATION_CANCELED)return S_OK;
        if(success && onAdmin()) { ShowWindow(errorLabel,SW_HIDE); ShowWindow(retryButton,SW_HIDE); fprintf(stderr,"Loaded native Admin page\n"); }
        else showError("Admin could not connect. Check that Smart Stage has finished starting, then reload.");
        return S_OK;
    });
    check(webview->add_NavigationCompleted(completed.p,&token),"Observe Admin navigation");
    auto failed=callback<ICoreWebView2ProcessFailedEventHandler,ICoreWebView2*,ICoreWebView2ProcessFailedEventArgs*>([](auto*,auto*) { showError("The Admin page stopped responding. Reload to reconnect; playback is still running."); return S_OK; });
    check(webview->add_ProcessFailed(failed.p,&token),"Observe Admin process");
    auto cursor=callback<ICoreWebView2CursorChangedEventHandler,ICoreWebView2CompositionController*,IUnknown*>([](auto* sender,auto*) { HCURSOR value=nullptr; if(SUCCEEDED(sender->get_Cursor(&value)))SetCursor(value); return S_OK; });
    check(composition->add_CursorChanged(cursor.p,&token),"Track Admin cursor");
    auto accelerator=callback<ICoreWebView2AcceleratorKeyPressedEventHandler,ICoreWebView2Controller*,ICoreWebView2AcceleratorKeyPressedEventArgs*>([](auto*,auto* args) {
        COREWEBVIEW2_KEY_EVENT_KIND kind{}; UINT key=0; args->get_KeyEventKind(&kind); args->get_VirtualKey(&key);
        if(kind==COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN && key==VK_ESCAPE) {
            COREWEBVIEW2_PHYSICAL_KEY_STATUS status{};args->get_PhysicalKeyStatus(&status);
            if(!status.WasKeyDown)emergencyRequested.store(true);
            args->put_Handled(TRUE);return S_OK;
        }
        if((kind==COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN || kind==COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN) && (GetKeyState(VK_CONTROL)&0x8000)) {
            if(key=='O') { args->put_Handled(TRUE); PostMessageW(control.load(),WM_COMMAND,chooseID,0); }
            if(key=='Q') { args->put_Handled(TRUE); PostMessageW(control.load(),WM_COMMAND,quitID,0); }
        }
        return S_OK;
    });
    check(controller->add_AcceleratorKeyPressed(accelerator.p,&token),"Configure Admin keyboard shortcuts");
    check(DCompositionCreateDevice(nullptr,IID_PPV_ARGS(dcomp.out())),"Create Admin composition device");
    check(dcomp->CreateTargetForHwnd(window,TRUE,target.out()),"Create Admin composition target");
    check(dcomp->CreateVisual(visual.out()),"Create Admin visual");
    check(target->SetRoot(visual.p),"Attach Admin visual");
    check(composition->put_RootVisualTarget(visual.p),"Attach WebView to Admin window");
    check(dcomp->Commit(),"Display Admin WebView");
    resize(); check(controller->put_IsVisible(IsWindowVisible(window)),"Show Admin WebView");
    check(webview->Navigate(adminURL.c_str()),"Load local Admin page");
    if(IsWindowVisible(window))controller->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);
}
void createWebView() {
    if(creating || webview || !window || stopping.load())return;
    creating=true;
    try {
        auto create=loadSDK();
        auto created=callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler,HRESULT,ICoreWebView2Environment*>([](HRESULT hr,auto* value) {
            creating=false; check(hr,"Create WebView2 environment"); environment.reset(value); value->AddRef();
            Ptr<ICoreWebView2Environment3> env3; check(environment->QueryInterface(IID_PPV_ARGS(env3.out())),"Create composition-capable WebView");
            creating=true;
            auto controllerCreated=callback<ICoreWebView2CreateCoreWebView2CompositionControllerCompletedHandler,HRESULT,ICoreWebView2CompositionController*>([](HRESULT result,auto* value) {
                creating=false; check(result,"Create native Admin WebView"); composition.reset(value); value->AddRef();
                try { configureWebView(); } catch (...) {
                    if(controller)controller->Close();
                    webview.reset();controller.reset();composition.reset();visual.reset();target.reset();dcomp.reset();
                    throw;
                }
                return S_OK;
            });
            HRESULT result=env3->CreateCoreWebView2CompositionController(window,controllerCreated.p); if(FAILED(result))creating=false;
            check(result,"Start Admin WebView controller"); return S_OK;
        });
        // A fresh per-process profile prevents browser extensions and stale
        // Admin sessions being shared with the user's browser or another app.
        auto userData=directory+L"\\Profile";
        HRESULT hr=create(nullptr,userData.c_str(),nullptr,created.p); if(FAILED(hr))creating=false;
        check(hr,"Start Microsoft WebView2");
    } catch (...) { creating=false; throw; }
}
void addTray() {
    if(trayAdded || stopping.load())return;
    tray={}; tray.cbSize=sizeof(tray); tray.hWnd=control.load(); tray.uID=1;
    tray.uFlags=NIF_MESSAGE|NIF_ICON|NIF_TIP; tray.uCallbackMessage=trayMessage;
    tray.hIcon=LoadIconW(GetModuleHandleW(nullptr),MAKEINTRESOURCEW(1));
    if(!tray.hIcon)tray.hIcon=LoadIconW(nullptr,IDI_APPLICATION);
    wcscpy_s(tray.szTip,L"Smart Stage — Open Admin / Quit");
    SetLastError(ERROR_SUCCESS);++trayAttempts;
    trayAdded=Shell_NotifyIconW(NIM_ADD,&tray)!=FALSE;
    trayAddError=trayAdded?ERROR_SUCCESS:GetLastError();
    if(trayAdded) { tray.uVersion=NOTIFYICON_VERSION_4; Shell_NotifyIconW(NIM_SETVERSION,&tray); KillTimer(control.load(),0x535); }
    else {
        HWND shell=FindWindowW(L"Shell_TrayWnd",nullptr);
        if(trayAttempts==1 || trayAttempts==10) {
            ICONINFO icon{};BOOL validIcon=tray.hIcon && GetIconInfo(tray.hIcon,&icon);
            if(icon.hbmColor)DeleteObject(icon.hbmColor);if(icon.hbmMask)DeleteObject(icon.hbmMask);
            DWORD shellPID=0,selfSession=DWORD(-1),shellSession=DWORD(-1);if(shell)GetWindowThreadProcessId(shell,&shellPID);
            ProcessIdToSessionId(GetCurrentProcessId(),&selfSession);if(shellPID)ProcessIdToSessionId(shellPID,&shellSession);
            fprintf(stderr,"Native Admin notification icon registration failed: attempt=%u shell=%p visible=%d error=%lu size=%u hwndOffset=%zu iconOffset=%zu control=%p validControl=%d icon=%p validIcon=%d selfPID=%lu selfSession=%lu shellPID=%lu shellSession=%lu\n",trayAttempts,(void*)shell,shell?int(IsWindowVisible(shell)):0,(unsigned long)trayAddError,(unsigned)tray.cbSize,offsetof(NOTIFYICONDATAW,hWnd),offsetof(NOTIFYICONDATAW,hIcon),(void*)tray.hWnd,int(IsWindow(tray.hWnd)),(void*)tray.hIcon,int(validIcon),(unsigned long)GetCurrentProcessId(),(unsigned long)selfSession,(unsigned long)shellPID,(unsigned long)shellSession);
        }
        if(trayAttempts<10)SetTimer(control.load(),0x535,500,nullptr);
        else KillTimer(control.load(),0x535);
    }
}
HMENU appMenu() {
    HMENU menu=CreatePopupMenu(); AppendMenuW(menu,MF_STRING,openID,L"Open Admin"); AppendMenuW(menu,MF_STRING,chooseID,L"Choose Media…\tCtrl+O");
    AppendMenuW(menu,MF_STRING,reloadID,L"Reload Admin"); AppendMenuW(menu,MF_STRING,browserID,L"Open Admin in Browser"); AppendMenuW(menu,MF_STRING,logsID,L"Open Log");
    AppendMenuW(menu,MF_SEPARATOR,0,nullptr); AppendMenuW(menu,MF_STRING,quitID,L"Quit Smart Stage\tCtrl+Q"); return menu;
}
void showWindow() {
    if(stopping.load())return;
    if(!validAdmin(adminURL)) { showPending.store(true);return; }
    showPending.store(false);
    if(!window) {
        RECT area{}; SystemParametersInfoW(SPI_GETWORKAREA,0,&area,0);
        int width=std::min(1120L,area.right-area.left-60),height=std::min(820L,area.bottom-area.top-60);
        window=CreateWindowExW(0,L"SmartStageAdmin-View",L"Smart Stage — Admin",WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN,
            area.left+(area.right-area.left-width)/2,area.top+(area.bottom-area.top-height)/2,width,height,nullptr,nullptr,GetModuleHandleW(nullptr),nullptr);
        if(!window)throw std::string("Create native Admin window");
        HMENU bar=CreateMenu(); AppendMenuW(bar,MF_POPUP,(UINT_PTR)appMenu(),L"Smart Stage"); SetMenu(window,bar);
        errorLabel=CreateWindowExW(0,L"STATIC",errorText.c_str(),WS_CHILD|WS_VISIBLE,20,22,width-160,72,window,nullptr,GetModuleHandleW(nullptr),nullptr);
        retryButton=CreateWindowExW(0,L"BUTTON",L"Reload Admin",WS_CHILD|WS_VISIBLE|WS_TABSTOP, width-130,28,112,30,window,(HMENU)(UINT_PTR)reloadID,GetModuleHandleW(nullptr),nullptr);
        SendMessageW(errorLabel,WM_SETFONT,(WPARAM)GetStockObject(DEFAULT_GUI_FONT),TRUE);
        SendMessageW(retryButton,WM_SETFONT,(WPARAM)GetStockObject(DEFAULT_GUI_FONT),TRUE);
        dropTarget.reset(new DropTarget()); check(RegisterDragDrop(window,dropTarget.p),"Enable Explorer file drops");
        fprintf(stderr,"Created native Admin window\n");
    }
    ShowWindow(window,IsIconic(window)?SW_RESTORE:SW_SHOW);
    // Installer/update helpers may supply SW_HIDE to avoid a console. Windows
    // applies that startup hint to the first ShowWindow call; an explicit
    // request for Admin must still display its dedicated window.
    if(!IsWindowVisible(window) || IsIconic(window))ShowWindow(window,SW_RESTORE);
    SetForegroundWindow(window);
    if(controller) { controller->put_IsVisible(TRUE); controller->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC); }
    else createWebView();
    fprintf(stderr,"Showed native Admin window\n");
}
void command(UINT id) {
    switch(id) {
    case openID: showWindow(); break;
    case chooseID: chooseMedia(); break;
    case quitID: if(!quitRequested.exchange(true))fprintf(stderr,"Quitting Smart Stage from the app menu\n"); break;
    case logsID: if(!logPath.empty())ShellExecuteW(window,L"open",logPath.c_str(),nullptr,nullptr,SW_SHOWNORMAL); break;
    case browserID: if(validAdmin(adminURL))ShellExecuteW(window,L"open",adminURL.c_str(),nullptr,nullptr,SW_SHOWNORMAL); break;
    case reloadID:
        showWindow();
        if(webview) { check(webview->Reload(),"Reload local Admin page"); }
        else createWebView();
        break;
    }
}
void drainTasks() {
    // Process one task per message. The file chooser enters a nested message
    // pump; moving the whole queue into a local batch would strand all later
    // tasks until that dialog closed, even while their wake messages ran.
    std::function<void()> fn;
    { std::lock_guard<std::mutex> lock(tasksMutex);
      if(stopping.load() || tasks.empty())return;
      fn=std::move(tasks.front());tasks.pop_front();
      if(!tasks.empty())PostMessageW(control.load(),wakeMessage,0,0); }
    try { fn(); } catch(const std::string& error) {showError(error);} catch(...) {showError("Native Admin operation failed.");}
}
void shutdownUI();
void cancelChooser() {
    if(!chooserShowing || !chooser)return;
    Ptr<IOleWindow> native;
    if(FAILED(chooser->QueryInterface(IID_PPV_ARGS(native.out()))))return;
    HWND dialog=nullptr;
    if(SUCCEEDED(native->GetWindow(&dialog)) && dialog && IsWindowVisible(dialog))
        // Let the dialog finish through its ordinary Cancel message. Calling
        // Close synchronously from its nested pump can reenter shell teardown.
        PostMessageW(dialog,WM_COMMAND,IDCANCEL,0);
}
LRESULT CALLBACK windowProc(HWND hwnd,UINT message,WPARAM wp,LPARAM lp) {
    try {
        if(message==shutdownMessage) {shutdownUI();return 0;}
        if(message==WM_TIMER && wp==0x535) {addTray();return 0;}
        if(message==WM_TIMER && wp==0x534 && stopping.load()) {cancelChooser();return 0;}
        if(message==wakeMessage) {drainTasks();return 0;}
        if(taskbarCreated && message==taskbarCreated && hwnd==control.load()) {trayAdded=false;trayAttempts=0;addTray();return 0;}
        if(message==trayMessage) {
            UINT event=LOWORD(lp);
            if(event==NIN_SELECT || event==NIN_KEYSELECT || event==WM_LBUTTONDBLCLK)showWindow();
            else if(event==WM_CONTEXTMENU || event==WM_RBUTTONUP) { POINT p{};GetCursorPos(&p);HMENU menu=appMenu();SetForegroundWindow(hwnd);UINT id=TrackPopupMenu(menu,TPM_RETURNCMD|TPM_RIGHTBUTTON,p.x,p.y,0,hwnd,nullptr);DestroyMenu(menu);if(id)command(id);PostMessageW(hwnd,WM_NULL,0,0); }
            return 0;
        }
        if(message==WM_COMMAND) { command(LOWORD(wp));return 0; }
        if(message==WM_KEYDOWN && hwnd==window && wp==VK_ESCAPE) {
            if(!(lp & (LPARAM(1)<<30)))emergencyRequested.store(true);
            return 0;
        }
        if(message==WM_CLOSE && hwnd==window) {
            // Retain a taskbar entry when the shell has rejected the tray icon,
            // so closing Admin never hides the user's only way back to the app.
            ShowWindow(window,trayAdded?SW_HIDE:SW_MINIMIZE);
            // A minimized window can be restored directly by the taskbar,
            // without showWindow(), so keep its WebView visible in that case.
            if(controller && trayAdded)controller->put_IsVisible(FALSE);
            fprintf(stderr,trayAdded?"Hid native Admin window\n":"Minimized native Admin window; notification icon unavailable\n");
            return 0;
        }
        if(message==WM_QUERYENDSESSION) { quitRequested.store(true);return TRUE; }
        if(message==WM_SIZE && hwnd==window) { resize();return 0; }
        if(message==WM_MOVE && controller && hwnd==window)controller->NotifyParentWindowPositionChanged();
        if(message==WM_DPICHANGED && hwnd==window) { auto* bounds=(RECT*)lp;SetWindowPos(hwnd,nullptr,bounds->left,bounds->top,bounds->right-bounds->left,bounds->bottom-bounds->top,SWP_NOZORDER|SWP_NOACTIVATE);resize();return 0; }
        if(message==WM_GETMINMAXINFO && hwnd==window) { auto* info=(MINMAXINFO*)lp;info->ptMinTrackSize={640,420};return 0; }
        if(message==WM_SETFOCUS && hwnd==window && controller) {controller->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);return 0;}
        if(message==WM_SETCURSOR && LOWORD(lp)==HTCLIENT && composition && hwnd==window) {HCURSOR cursor=nullptr;if(SUCCEEDED(composition->get_Cursor(&cursor))) {SetCursor(cursor);return TRUE;}}
        if(hwnd==window && composition && ((message>=WM_MOUSEFIRST && message<=WM_MOUSELAST)||message==WM_MOUSELEAVE)) {
            POINT point{GET_X_LPARAM(lp),GET_Y_LPARAM(lp)}; DWORD data=0;
            if(message==WM_MOUSEWHEEL || message==WM_MOUSEHWHEEL) {ScreenToClient(hwnd,&point);data=GET_WHEEL_DELTA_WPARAM(wp);}
            if(message==WM_XBUTTONDOWN || message==WM_XBUTTONUP || message==WM_XBUTTONDBLCLK)data=GET_XBUTTON_WPARAM(wp);
            if(message==WM_MOUSEMOVE && !mouseTracking) {TRACKMOUSEEVENT track{sizeof(track),TME_LEAVE,hwnd,0};TrackMouseEvent(&track);mouseTracking=true;}
            if(message==WM_MOUSELEAVE) {mouseTracking=false;point={0,0};}
            if(message==WM_LBUTTONDOWN || message==WM_RBUTTONDOWN || message==WM_MBUTTONDOWN || message==WM_XBUTTONDOWN) {SetCapture(hwnd);controller->MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);}
            if(message==WM_LBUTTONUP || message==WM_RBUTTONUP || message==WM_MBUTTONUP || message==WM_XBUTTONUP)if(GetCapture()==hwnd)ReleaseCapture();
            composition->SendMouseInput(static_cast<COREWEBVIEW2_MOUSE_EVENT_KIND>(message),static_cast<COREWEBVIEW2_MOUSE_EVENT_VIRTUAL_KEYS>(GET_KEYSTATE_WPARAM(wp)),data,point);return 0;
        }
    } catch(const std::string& error) {showError(error);} catch(...) {showError("Native Admin operation failed.");}
    return DefWindowProcW(hwnd,message,wp,lp);
}
void shutdownUI() {
    if(!stopping.load())fprintf(stderr,"Closing native Admin: chooserActive=%d chooserShowing=%d\n",int(chooserActive),int(chooserShowing));
    stopping.store(true);ready.store(false);
    if(chooserActive) {
        SetTimer(control.load(),0x534,50,nullptr);
        cancelChooser();
        return;
    }
    KillTimer(control.load(),0x534);
    KillTimer(control.load(),0x535);
    if(trayAdded) {Shell_NotifyIconW(NIM_DELETE,&tray);trayAdded=false;}
    if(window)RevokeDragDrop(window);
    UINT32 browserID=0;
    if(webview && SUCCEEDED(webview->get_BrowserProcessId(&browserID)) && browserID)
        browserProcess=OpenProcess(SYNCHRONIZE,FALSE,browserID);
    if(controller)controller->Close();
    fprintf(stderr,"Closed native Admin WebView\n");
    webview.reset();controller.reset();composition.reset();environment.reset();
    visual.reset();target.reset();dcomp.reset();dropTarget.reset();
    if(window) {DestroyWindow(window);window=nullptr;errorLabel=nullptr;retryButton=nullptr;}
    uiQuit=true;
}
void uiMain() {
    HRESULT apartment=OleInitialize(nullptr);
    if(SUCCEEDED(apartment)) {
        WNDCLASSEXW cls{};cls.cbSize=sizeof(cls);cls.lpfnWndProc=windowProc;cls.hInstance=GetModuleHandleW(nullptr);cls.hCursor=LoadCursorW(nullptr,IDC_ARROW);cls.hbrBackground=(HBRUSH)(COLOR_WINDOW+1);cls.hIcon=LoadIconW(cls.hInstance,MAKEINTRESOURCEW(1));cls.hIconSm=cls.hIcon;
        cls.lpszClassName=L"SmartStageAdmin-View";RegisterClassExW(&cls);
        cls.lpszClassName=identity.c_str();RegisterClassExW(&cls);
        control.store(CreateWindowExW(WS_EX_TOOLWINDOW,identity.c_str(),L"Smart Stage Control",0,0,0,0,0,nullptr,nullptr,cls.hInstance,nullptr));
        taskbarCreated=RegisterWindowMessageW(L"TaskbarCreated");
    }
    { std::lock_guard<std::mutex> lock(initMutex);initialized=true; } initCV.notify_all();
    if(!control.load()) {if(SUCCEEDED(apartment))OleUninitialize();return;}
    MSG message{};
    while(!uiQuit && GetMessageW(&message,nullptr,0,0)>0) {TranslateMessage(&message);DispatchMessageW(&message);}
    // Browser profile locks are released asynchronously. Pump this STA while
    // waiting only for this private profile's browser process, for at most 2s.
    if(browserProcess) {
        ULONGLONG deadline=GetTickCount64()+2000;
        while(GetTickCount64()<deadline) {
            DWORD status=MsgWaitForMultipleObjects(1,&browserProcess,FALSE,50,QS_ALLINPUT);
            if(status==WAIT_OBJECT_0 || status==WAIT_FAILED)break;
            while(PeekMessageW(&message,nullptr,0,0,PM_REMOVE)) {TranslateMessage(&message);DispatchMessageW(&message);}
        }
        CloseHandle(browserProcess);browserProcess=nullptr;
    }
    DestroyWindow(control.exchange(nullptr));
    if(loader) {FreeLibrary(loader);loader=nullptr;}
    if(loaderFile!=INVALID_HANDLE_VALUE) {CloseHandle(loaderFile);loaderFile=INVALID_HANDLE_VALUE;}
    if(!directory.empty()) {
        std::error_code error;ULONGLONG deadline=GetTickCount64()+1000;
        do {error.clear();std::filesystem::remove_all(directory,error);if(!error)break;Sleep(25);}while(GetTickCount64()<deadline);
        if(error)fprintf(stderr,"Native Admin private profile cleanup deferred: %s\n",error.message().c_str());
    }
    OleUninitialize();
}
bool post(std::function<void()> fn) {
    if(stopping.load())return false;
    std::call_once(startOnce,[]{uiThread=std::thread(uiMain);});
    {std::unique_lock<std::mutex> lock(initMutex);initCV.wait(lock,[]{return initialized;});}
    HWND handle=control.load();if(!handle)return false;
    {std::lock_guard<std::mutex> lock(tasksMutex);if(stopping.load() || tasks.size()>=64)return false;tasks.push_back(std::move(fn));}
    return PostMessageW(handle,wakeMessage,0,0)!=FALSE;
}
} // namespace desktop

extern "C" void ss_desktop_identity(const char* key) { desktop::identity=L"SmartStageAdmin-"+desktop::wide(key); }
extern "C" void ss_desktop_log_path(const char* path) {
    desktop::logPath=desktop::wide(path);
    // Explorer's GUI processes have no CRT stdout/stderr. Preserve real pipes
    // used by diagnostics/CI, but send native errors to the same app log as Go.
    for(FILE* stream : {stdout,stderr}) {
        int descriptor=_fileno(stream);
        auto handle=descriptor>=0 ? (HANDLE)_get_osfhandle(descriptor) : INVALID_HANDLE_VALUE;
        if((!handle || handle==INVALID_HANDLE_VALUE || GetFileType(handle)==FILE_TYPE_UNKNOWN) && !desktop::logPath.empty()) {
            if(_wfreopen(desktop::logPath.c_str(),L"a",stream))setvbuf(stream,nullptr,_IONBF,0);
        }
    }
}
extern "C" int ss_desktop_reopen(const char* key) {
    auto name=L"SmartStageAdmin-"+desktop::wide(key);HWND peer=FindWindowW(name.c_str(),nullptr);if(!peer)return 0;
    DWORD pid=0;GetWindowThreadProcessId(peer,&pid);if(!pid || pid==GetCurrentProcessId())return 0;
    AllowSetForegroundWindow(pid);return PostMessageW(peer,WM_COMMAND,desktop::openID,0)!=FALSE;
}
extern "C" int ss_desktop_poll_quit_request() {return desktop::quitRequested.exchange(false)?1:0;}
extern "C" int ss_desktop_poll_emergency_request() {return desktop::emergencyRequested.exchange(false)?1:0;}
extern "C" void ss_desktop_admin(const char* value) {
    auto url=desktop::wide(value);if(!desktop::validAdmin(url))return;
    desktop::post([url]{desktop::adminURL=url;desktop::origin=url.substr(0,url.size()-6);desktop::ready.store(true);desktop::addTray();if(desktop::showPending.load())desktop::showWindow();});
}
extern "C" int ss_desktop_has_admin_window() {return 1;}
extern "C" int ss_desktop_show_admin() {desktop::showPending.store(true);return desktop::post([]{desktop::showWindow();})?1:0;}
extern "C" int ss_desktop_poll_admin_request() {return desktop::adminRequested.exchange(false)?1:0;}
extern "C" int ss_desktop_can_choose_files() {return desktop::ready.load() && !desktop::stopping.load();}
extern "C" int ss_desktop_choose_files() {
    if(!ss_desktop_can_choose_files())return 0;
    if(desktop::chooserScheduled.exchange(true))return 1;
    if(!desktop::post([]{desktop::chooseMedia();})) {desktop::chooserScheduled.store(false);return 0;}return 1;
}
extern "C" int ss_desktop_activate_browser() {return ss_desktop_show_admin();}
extern "C" void ss_desktop_error(const char* message) {auto text=desktop::wide(message);MessageBoxW(nullptr,text.c_str(),L"Smart Stage",MB_OK|MB_ICONERROR);}
extern "C" char* ss_desktop_poll_files() {
    std::lock_guard<std::mutex> lock(desktop::filesMutex);if(desktop::files.empty())return nullptr;
    auto request=std::move(desktop::files.front());desktop::files.pop_front();
    std::string out="{\"id\":"+std::to_string(request.id)+",\"paths\":[";bool first=true;
    for(const auto& path:request.paths) {if(!first)out+=',';first=false;out+=desktop::quote(path);}out+="]}";
    auto* result=(char*)malloc(out.size()+1);if(result)memcpy(result,out.c_str(),out.size()+1);return result;
}
extern "C" int ss_desktop_files_pending() {std::lock_guard<std::mutex> lock(desktop::filesMutex);return desktop::pendingCount>0;}
extern "C" void ss_desktop_files_result(uint64_t id,const char* message) {
    {std::lock_guard<std::mutex> lock(desktop::filesMutex);auto found=desktop::pendingFiles.find(id);if(found==desktop::pendingFiles.end())return;desktop::pendingCount-=found->second;desktop::pendingFiles.erase(found);}
    std::string text=message?message:"";
    if(!text.empty())desktop::post([text]{desktop::showWindow();desktop::showError(text);});
}
extern "C" void ss_windows_desktop_shutdown() {
    if(!desktop::uiThread.joinable())return;
    // A separate message is never dropped by the bounded ordinary-task queue.
    // It also runs in IFileOpenDialog's nested pump, closes that dialog, then
    // the outer pump observes uiQuit without waiting for another message.
    HWND handle=desktop::control.load();
    if(handle)fprintf(stderr,"Requested native Admin shutdown: queued=%d\n",int(PostMessageW(handle,desktop::shutdownMessage,0,0)));
    desktop::uiThread.join();
    fprintf(stderr,"Native Admin shutdown completed\n");
}
