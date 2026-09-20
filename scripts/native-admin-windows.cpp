// Test-only own-process observer. Includes the production desktop implementation
// without adding a debug endpoint or privileged bridge to the shipped executable.
#include "../internal/platform/desktop_windows.cpp"
#include <chrono>
#include <future>
#include <iostream>

namespace probe {
std::map<std::string,std::string> report;
void require(bool condition,const char* message) {if(!condition)throw std::string(message);}
void passed(const char* name) {report[name]="true";std::cerr<<"Native Admin probe passed: "<<name<<"\n";}
template<class F> auto ui(F fn) -> decltype(fn()) {
    using T=decltype(fn()); auto promise=std::make_shared<std::promise<T>>();auto result=promise->get_future();
    require(desktop::post([fn,promise]{try {if constexpr(std::is_void_v<T>) {fn();promise->set_value();} else promise->set_value(fn());}catch(...) {promise->set_exception(std::current_exception());}}),"Could not queue native observation");
    require(result.wait_for(std::chrono::seconds(10))==std::future_status::ready,"Native UI did not respond");return result.get();
}
std::string js(const wchar_t* script) {
    auto promise=std::make_shared<std::promise<std::string>>();auto result=promise->get_future();
    require(desktop::post([script,promise]{
        if(!desktop::webview) {promise->set_value("null");return;}
        auto done=desktop::callback<ICoreWebView2ExecuteScriptCompletedHandler,HRESULT,LPCWSTR>([promise](HRESULT hr,LPCWSTR value) {promise->set_value(SUCCEEDED(hr)?desktop::utf8(value):"null");return S_OK;});
        HRESULT hr=desktop::webview->ExecuteScript(script,done.p);if(FAILED(hr))promise->set_value("null");
    }),"Could not queue WebView observation");
    require(result.wait_for(std::chrono::seconds(10))==std::future_status::ready,"WebView script did not finish");return result.get();
}
template<class F> void wait(F fn,const char* message) {auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(35);while(std::chrono::steady_clock::now()<deadline) {if(fn())return;std::this_thread::sleep_for(std::chrono::milliseconds(100));}throw std::string(message);}
bool chooserVisible() {
    return ui([]{
        if(!desktop::chooser)return false;
        desktop::Ptr<IOleWindow> native;
        if(FAILED(desktop::chooser->QueryInterface(IID_PPV_ARGS(native.out()))))return false;
        HWND dialog=nullptr;return SUCCEEDED(native->GetWindow(&dialog)) && dialog && IsWindowVisible(dialog);
    });
}
// Failure-only independent Win32 baselines distinguish an app failure from a
// shell that rejects every valid icon. Temporary icons/windows are removed.
// Return true if any independent registration or production readback works.
bool trayDiagnostics() {
    return ui([]{
        HWND shell=FindWindowW(L"Shell_TrayWnd",nullptr);DWORD shellPID=0;
        if(shell)GetWindowThreadProcessId(shell,&shellPID);
        auto process=[](DWORD pid,const char* label) {
            HANDLE handle=OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION,FALSE,pid),token=nullptr;
            DWORD session=DWORD(-1);ProcessIdToSessionId(pid,&session);
            std::cerr<<"Tray diagnostic process "<<label<<" pid="<<pid<<" session="<<session;
            if(handle && OpenProcessToken(handle,TOKEN_QUERY,&token)) {
                TOKEN_ELEVATION elevation{};DWORD needed=0;
                if(GetTokenInformation(token,TokenElevation,&elevation,sizeof(elevation),&needed))std::cerr<<" elevated="<<elevation.TokenIsElevated;
                GetTokenInformation(token,TokenIntegrityLevel,nullptr,0,&needed);std::vector<BYTE> buffer(needed);
                if(needed && GetTokenInformation(token,TokenIntegrityLevel,buffer.data(),needed,&needed)) {
                    auto* level=(TOKEN_MANDATORY_LABEL*)buffer.data();auto count=*GetSidSubAuthorityCount(level->Label.Sid);
                    if(count)std::cerr<<" integrity="<<*GetSidSubAuthority(level->Label.Sid,count-1);
                }
                CloseHandle(token);
            } else std::cerr<<" tokenError="<<GetLastError();
            if(handle)CloseHandle(handle);std::cerr<<"\n";
        };
        process(GetCurrentProcessId(),"probe");if(shellPID)process(shellPID,"explorer");
        for(auto object:{(HANDLE)GetProcessWindowStation(),(HANDLE)GetThreadDesktop(GetCurrentThreadId())}) {
            wchar_t name[256]{};DWORD needed=0;
            BOOL result=GetUserObjectInformationW(object,UOI_NAME,name,sizeof(name),&needed);
            std::cerr<<"Tray diagnostic desktop object="<<object<<" name="<<(result?desktop::utf8(name):"unknown")<<"\n";
        }
        for(HKEY hive:{HKEY_CURRENT_USER,HKEY_LOCAL_MACHINE})for(const auto* setting:{L"NoTrayItemsDisplay",L"NoSetTaskbar"}) {
            DWORD value=0,size=sizeof(value);LSTATUS result=RegGetValueW(hive,L"Software\\Microsoft\\Windows\\CurrentVersion\\Policies\\Explorer",setting,RRF_RT_REG_DWORD,nullptr,&value,&size);
            std::cerr<<"Tray diagnostic policy hive="<<(hive==HKEY_CURRENT_USER?"HKCU":"HKLM")<<" name="<<desktop::utf8(setting)<<" result="<<result<<" value="<<value<<"\n";
        }
        NOTIFYICONIDENTIFIER existing{};existing.cbSize=sizeof(existing);existing.hWnd=desktop::tray.hWnd;existing.uID=desktop::tray.uID;RECT rect{};
        HRESULT rectangle=Shell_NotifyIconGetRect(&existing,&rect);
        SetLastError(0);BOOL modify=Shell_NotifyIconW(NIM_MODIFY,&desktop::tray);DWORD modifyError=GetLastError();
        std::cerr<<"Tray diagnostic production readback rectHRESULT="<<(unsigned long)rectangle<<" modify="<<modify<<" modifyError="<<modifyError<<"\n";
        report["trayProductionReadback"]="{\"rectangleHRESULT\":"+std::to_string((unsigned long)rectangle)+",\"modifySucceeded\":"+(modify?"true":"false")+",\"lastError\":"+std::to_string(modifyError)+"}";
        HWND baseline=CreateWindowExW(0,L"STATIC",L"Smart Stage tray diagnostic",WS_OVERLAPPED,0,0,100,100,nullptr,nullptr,GetModuleHandleW(nullptr),nullptr);
        struct WindowCleanup {HWND handle;~WindowCleanup(){if(handle)DestroyWindow(handle);}} cleanup{baseline};
        std::cerr<<"Tray diagnostic baseline window="<<baseline<<" valid="<<IsWindow(baseline)<<" sizeW="<<sizeof(NOTIFYICONDATAW)<<" sizeA="<<sizeof(NOTIFYICONDATAA)<<"\n";
        require(baseline && IsWindow(baseline) && IsWindow(desktop::tray.hWnd),"Tray baseline has an invalid window; host capability could not be determined");
        HICON systemIcon=LoadIconW(nullptr,IDI_APPLICATION);ICONINFO information{};
        BOOL validIcon=systemIcon && GetIconInfo(systemIcon,&information);
        if(information.hbmColor)DeleteObject(information.hbmColor);if(information.hbmMask)DeleteObject(information.hbmMask);
        require(validIcon,"Tray baseline has an invalid system icon; host capability could not be determined");
        bool supported=modify || SUCCEEDED(rectangle);unsigned count=0;std::string results="[";
        auto record=[&](const char* label,BOOL added,DWORD error,UINT bytes,UINT flags) {
            if(count++)results+=',';
            results+="{\"name\":"+desktop::quote(label)+",\"added\":"+(added?"true":"false")+",\"lastError\":"+std::to_string(error)+",\"size\":"+std::to_string(bytes)+",\"flags\":"+std::to_string(flags)+",\"validWindow\":true,\"validSystemIcon\":true,\"windowVisible\":"+(IsWindowVisible(baseline)?"true":"false")+"}";
            supported=supported || added;
        };
        auto attempt=[&](const char* label,UINT bytes,UINT flags,bool guid) {
            NOTIFYICONDATAW item{};item.cbSize=bytes;item.hWnd=baseline;item.uID=71;item.uFlags=flags;
            item.uCallbackMessage=WM_APP+117;item.hIcon=systemIcon;wcscpy_s(item.szTip,L"Smart Stage tray diagnostic");
            if(guid) {require(SUCCEEDED(CoCreateGuid(&item.guidItem)),"Could not create tray baseline GUID");item.uFlags|=NIF_GUID;}
            SetLastError(0);BOOL added=Shell_NotifyIconW(NIM_ADD,&item);DWORD error=GetLastError();
            std::cerr<<"Tray diagnostic baseline "<<label<<" added="<<added<<" error="<<error<<" icon="<<item.hIcon<<" size="<<bytes<<" flags="<<item.uFlags<<"\n";
            record(label,added,error,bytes,item.uFlags);
            if(added)require(Shell_NotifyIconW(NIM_DELETE,&item)!=FALSE,"Could not remove temporary tray baseline icon");
        };
        attempt("unicode-minimal",sizeof(NOTIFYICONDATAW),NIF_ICON|NIF_TIP,false);
        attempt("unicode-callback",sizeof(NOTIFYICONDATAW),NIF_MESSAGE|NIF_ICON|NIF_TIP,false);
        attempt("unicode-guid",sizeof(NOTIFYICONDATAW),NIF_MESSAGE|NIF_ICON|NIF_TIP,true);
        attempt("unicode-v2",NOTIFYICONDATAW_V2_SIZE,NIF_MESSAGE|NIF_ICON|NIF_TIP,false);
        ShowWindow(baseline,SW_SHOWNOACTIVATE);
        require(IsWindowVisible(baseline),"Visible-window tray baseline could not be shown");
        attempt("unicode-visible",sizeof(NOTIFYICONDATAW),NIF_MESSAGE|NIF_ICON|NIF_TIP,false);
        NOTIFYICONDATAA narrow{};narrow.cbSize=sizeof(narrow);narrow.hWnd=baseline;narrow.uID=71;narrow.uFlags=NIF_ICON|NIF_TIP;
        narrow.hIcon=systemIcon;strcpy_s(narrow.szTip,"Smart Stage tray diagnostic");
        SetLastError(0);BOOL added=Shell_NotifyIconA(NIM_ADD,&narrow);DWORD error=GetLastError();
        std::cerr<<"Tray diagnostic baseline ansi-minimal added="<<added<<" error="<<error<<"\n";
        record("ansi-minimal",added,error,narrow.cbSize,narrow.uFlags);
        if(added)require(Shell_NotifyIconA(NIM_DELETE,&narrow)!=FALSE,"Could not remove temporary ANSI tray baseline icon");
        require(count==6,"Tray baseline evidence is incomplete");
        report["trayBaselineResults"]=results+"]";report["trayIndependentInputsValid"]="true";
        return supported;
    });
}
class Files final: public IDataObject {
    ULONG refs=1;std::vector<std::wstring> paths;bool text;
public:
    Files(std::vector<std::wstring> p,bool t=false):paths(std::move(p)),text(t){}
    HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid,void** out)override {if(!out)return E_POINTER;*out=nullptr;if(iid==__uuidof(IUnknown)||iid==__uuidof(IDataObject)) {*out=this;AddRef();return S_OK;}return E_NOINTERFACE;}
    ULONG STDMETHODCALLTYPE AddRef()override{return ++refs;}
    ULONG STDMETHODCALLTYPE Release()override{auto n=--refs;if(!n)delete this;return n;}
    HRESULT STDMETHODCALLTYPE QueryGetData(FORMATETC* f)override{return !text && f && f->cfFormat==CF_HDROP && (f->tymed&TYMED_HGLOBAL)?S_OK:DV_E_FORMATETC;}
    HRESULT STDMETHODCALLTYPE GetData(FORMATETC* f,STGMEDIUM* medium)override {
        if(QueryGetData(f)!=S_OK)return DV_E_FORMATETC;
        size_t chars=1;for(auto& path:paths)chars+=path.size()+1;
        HGLOBAL memory=GlobalAlloc(GMEM_MOVEABLE|GMEM_ZEROINIT,sizeof(DROPFILES)+chars*sizeof(wchar_t));if(!memory)return E_OUTOFMEMORY;
        auto* drop=(DROPFILES*)GlobalLock(memory);drop->pFiles=sizeof(DROPFILES);drop->fWide=TRUE;
        auto* cursor=(wchar_t*)((BYTE*)drop+sizeof(DROPFILES));for(auto& path:paths) {memcpy(cursor,path.c_str(),(path.size()+1)*sizeof(wchar_t));cursor+=path.size()+1;}
        GlobalUnlock(memory);medium->tymed=TYMED_HGLOBAL;medium->hGlobal=memory;medium->pUnkForRelease=nullptr;return S_OK;
    }
    HRESULT STDMETHODCALLTYPE GetDataHere(FORMATETC*,STGMEDIUM*)override{return E_NOTIMPL;}
    HRESULT STDMETHODCALLTYPE GetCanonicalFormatEtc(FORMATETC*,FORMATETC*)override{return E_NOTIMPL;}
    HRESULT STDMETHODCALLTYPE SetData(FORMATETC*,STGMEDIUM*,BOOL)override{return E_NOTIMPL;}
    HRESULT STDMETHODCALLTYPE EnumFormatEtc(DWORD,IEnumFORMATETC**)override{return E_NOTIMPL;}
    HRESULT STDMETHODCALLTYPE DAdvise(FORMATETC*,DWORD,IAdviseSink*,DWORD*)override{return OLE_E_ADVISENOTSUPPORTED;}
    HRESULT STDMETHODCALLTYPE DUnadvise(DWORD)override{return OLE_E_ADVISENOTSUPPORTED;}
    HRESULT STDMETHODCALLTYPE EnumDAdvise(IEnumSTATDATA**)override{return OLE_E_ADVISENOTSUPPORTED;}
};
std::string popRequest(size_t count) {
    char* raw=ss_desktop_poll_files();require(raw,"Native action did not queue files");std::string value=raw;free(raw);
    {std::lock_guard<std::mutex> lock(desktop::filesMutex);require(desktop::pendingCount==count,"Native pending file count is wrong");}
    uint64_t id=0;{std::lock_guard<std::mutex> lock(desktop::filesMutex);require(desktop::pendingFiles.size()==1,"Unexpected native request count");id=desktop::pendingFiles.begin()->first;}
    ss_desktop_files_result(id,"");require(!ss_desktop_files_pending(),"Native file acknowledgement was not applied");return value;
}
}
int main() {
    using namespace probe;
    // Match production ss_init before creating any native UI or WebView.
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    try {
        auto* addressWide=_wgetenv(L"SMARTSTAGE_PROBE_ADMIN_URL");auto* first=_wgetenv(L"SMARTSTAGE_PROBE_ORIGINAL_ONE");auto* second=_wgetenv(L"SMARTSTAGE_PROBE_ORIGINAL_TWO");
        require(addressWide&&first&&second,"Missing test-only probe environment");
        auto address=desktop::utf8(addressWide);
        ss_desktop_identity("native-probe");ss_desktop_admin(address.c_str());ui([]{});
        require(ss_desktop_show_admin(),"Native Admin show was rejected");
        wait([]{return js(L"typeof role!=='undefined' && role==='admin' && !localSessionBusy && typeof csrf!=='undefined' && csrf.length>0 && source?.readyState===EventSource.OPEN && state?.cues?.length===1 && document.getElementById('connection')?.textContent==='Connected to host'")=="true";},"Real Admin did not authenticate and connect its EventSource");
        passed("realAdminAssetsLoadedInWebView2");passed("localSessionAndCSRFInitialized");passed("authenticatedEventSourceConnected");
        require(js(L"navigator.userAgent.includes('SmartStageDesktop') && navigator.userAgent.includes('SmartStageWindowsDesktop') && document.getElementById('playlist').children.length>0 && !document.getElementById('choose-files').hidden && document.getElementById('media-path-settings').hidden")=="true","Desktop UI identity or chooser capability missing");
        passed("nativeUserAgentAndPlaylistRendered");
        HWND original=ui([]{return desktop::window;});auto* web=ui([]{return desktop::webview.p;});
        require(ui([]{return IsWindowVisible(desktop::window)!=FALSE && desktop::composition.p && desktop::controller.p;}),"Composition Admin window is not visible");passed("compositionWindowVisible");
        bool notificationArea=FindWindowW(L"Shell_TrayWnd",nullptr)!=nullptr;
        report["explorerNotificationAreaAvailable"]=notificationArea?"true":"false";
        try {wait([]{return ui([]{return desktop::trayAdded;});},"Smart Stage notification icon was not registered; see native shell diagnostics");}
        catch(...) {
            require(!trayDiagnostics(),"Smart Stage tray registration failed although an independent tray baseline or production readback succeeded");
            report["trayUnavailableReason"]=desktop::quote("The host shell rejected all six independent Win32 registrations with valid window and system icon; this run verifies the taskbar fallback instead of claiming tray registration.");
            std::cerr<<"Native Admin probe: host notification area unavailable; verifying minimized taskbar fallback\n";
        }
        report["trayIconRegistered"]=ui([]{return desktop::trayAdded;})?"true":"false";
        report["trayCapability"]=desktop::quote(report["trayIconRegistered"]=="true"?"available":"unavailable");
        js(L"window.__probeEpoch=state.stopEpoch;window.__probeDraft='preserved';document.getElementById('remote-connection-settings').open=true;document.getElementById('gateway-url').value='https://unsaved.example/smartstage';document.getElementById('gateway-url').dispatchEvent(new Event('input',{bubbles:true}));true");
        auto stopPosition=js(L"(()=>{const r=document.getElementById('stop').getBoundingClientRect();return [Math.round((r.left+r.width/2)*devicePixelRatio),Math.round((r.top+r.height/2)*devicePixelRatio)]})()");
        long stopX=0,stopY=0;require(sscanf(stopPosition.c_str(),"[%ld,%ld]",&stopX,&stopY)==2,"Could not locate rendered STOP button");
        ui([&]{SendMessageW(desktop::window,WM_LBUTTONDOWN,MK_LBUTTON,MAKELPARAM(stopX,stopY));SendMessageW(desktop::window,WM_LBUTTONUP,0,MAKELPARAM(stopX,stopY));});
        wait([]{return js(L"state.stopEpoch>window.__probeEpoch && online && source.readyState===EventSource.OPEN")=="true";},"Native mouse input did not activate real CSRF-protected STOP");passed("compositionMouseInputActivatedStop");passed("realCSRFProtectedStopAccepted");passed("liveStateAfterStopObserved");
        bool closedToTray=ui([]{bool available=desktop::trayAdded;SendMessageW(desktop::window,WM_CLOSE,0,0);return available;});
        if(closedToTray) {
            require(!IsWindowVisible(original),"Closing Admin with its notification icon did not hide the window");passed("closeHidWindowWithoutTerminating");
            report["closeBehavior"]=desktop::quote("hidden-to-tray");
        } else {
            require(IsWindowVisible(original) && IsIconic(original),"Closing Admin without a notification icon did not retain its minimized window");
            require(!(GetWindowLongPtrW(original,GWL_EXSTYLE)&WS_EX_TOOLWINDOW) && GetWindow(original,GW_OWNER)==nullptr,"Minimized Admin is not eligible for a taskbar entry");
            passed("closeMinimizedToTaskbarWithoutTerminating");report["closeBehavior"]=desktop::quote("minimized-to-taskbar");
            ui([]{SendMessageW(desktop::window,WM_SYSCOMMAND,SC_RESTORE,0);});
            require(IsWindowVisible(original) && !IsIconic(original),"Taskbar restore did not restore the existing Admin window");
            require(ui([]{BOOL visible=FALSE;return desktop::controller && SUCCEEDED(desktop::controller->get_IsVisible(&visible)) && visible;}),"Taskbar restore left Admin's WebView hidden");
            passed("taskbarRestoreKeptWebViewVisible");
        }
        ss_desktop_show_admin();ss_desktop_show_admin();wait([&]{return IsWindowVisible(original)!=FALSE && !IsIconic(original);},"Reopen did not restore Admin");
        require(ui([&]{return desktop::window==original&&desktop::webview.p==web;}),"Reopen replaced Admin or its WebView");
        require(js(L"window.__probeDraft==='preserved' && document.getElementById('gateway-url').value==='https://unsaved.example/smartstage' && document.getElementById('remote-connection-settings').open && online")=="true","Reopen lost unsaved UI or its connection");passed("reopenKeptSameWindowAndWebView");passed("unsavedUIAndLiveConnectionPreserved");
        ui([]{SendMessageW(desktop::window,WM_KEYDOWN,VK_ESCAPE,1);});
        require(ss_desktop_poll_emergency_request()==1 && ss_desktop_poll_emergency_request()==0,"Native Admin Escape did not queue exactly one emergency request");
        ui([]{SendMessageW(desktop::window,WM_KEYDOWN,VK_ESCAPE,(LPARAM(1)<<30)|1);});
        require(ss_desktop_poll_emergency_request()==0,"Held Escape repeated emergency requests");passed("nativeAdminEscapePolledWithoutJavaScript");
        require(!ss_desktop_files_pending(),"Native queue was not empty before drop");
        std::wstring firstPath=first,secondPath=second;
        ui([&]{
            RECT bounds{};GetClientRect(desktop::window,&bounds);POINT location{bounds.right/2,bounds.bottom/2};ClientToScreen(desktop::window,&location);
            require(WindowFromPoint(location)==desktop::window,"Drop point is intercepted by a browser child HWND");
            auto* object=new Files({firstPath,secondPath});DWORD effect=DROPEFFECT_COPY;POINTL point{location.x,location.y};
            require(SUCCEEDED(desktop::dropTarget->DragEnter(object,0,point,&effect))&&effect==DROPEFFECT_COPY,"Native drop enter was rejected");
            effect=DROPEFFECT_COPY;require(SUCCEEDED(desktop::dropTarget->Drop(object,0,point,&effect))&&effect==DROPEFFECT_COPY,"Native Explorer-format drop was rejected");object->Release();
        });
        passed("publicHitTestTargetsNativeAdminDropWindow");passed("nativeCFHDROPDeliveredToProductionDropTarget");report["dropRequest"]=popRequest(2);
        ui([]{auto* text=new Files({},true);DWORD effect=DROPEFFECT_COPY;desktop::dropTarget->DragEnter(text,0,{0,0},&effect);require(effect==DROPEFFECT_NONE,"Text was accepted as original file paths");desktop::dropTarget->Drop(text,0,{0,0},&effect);text->Release();});require(!ss_desktop_poll_files(),"Text drop queued a file");passed("textCannotSupplyNativeOriginalPaths");
        require(ss_desktop_choose_files(),"Native media chooser was rejected");
        wait(chooserVisible,"Native Windows file dialog did not become visible");
        ui([&]{desktop::check(desktop::chooser->SetFileName(secondPath.c_str()),"Select original file in native chooser");desktop::Ptr<IOleWindow> native;desktop::check(desktop::chooser->QueryInterface(IID_PPV_ARGS(native.out())),"Observe native file dialog");HWND dialog=nullptr;desktop::check(native->GetWindow(&dialog),"Find native chooser HWND");require(dialog&&IsWindowVisible(dialog),"Native file dialog is not visible");PostMessageW(dialog,WM_COMMAND,IDOK,0);});
        wait([]{return ss_desktop_files_pending()!=0;},"Selecting the original in the real native chooser did not queue it");report["chooserRequest"]=popRequest(1);passed("nativeFileDialogOriginalSelectionAccepted");
        auto blocked=desktop::wide(address.c_str());blocked.resize(blocked.size()-6);blocked+=L"/command";
        ui([&]{desktop::webview->Navigate(blocked.c_str());});std::this_thread::sleep_for(std::chrono::milliseconds(400));
        require(js(L"window.__probeDraft==='preserved' && location.pathname==='/admin'")=="true","Navigation escaped Admin or lost the existing document");
        require(ui([]{return !IsWindowVisible(desktop::errorLabel);}),"Intentionally blocked navigation displayed a connection error");passed("navigationOutsideAdminBlocked");
        js(L"window.__probeBlockedFetch=false;fetch('http://127.0.0.1:1/api/state').then(r=>window.__probeBlockedFetch=r.status===403).catch(()=>window.__probeBlockedFetch=true);true");
        wait([]{return js(L"window.__probeBlockedFetch===true")=="true";},"Cross-origin resource was not blocked");passed("crossOriginResourcesBlocked");
        // App's own native Quit is polled; it never terminates a process behind
        // Go's back. Then verify authenticated UI Quit against the real host.
        ui([]{desktop::command(desktop::quitID);});require(ss_desktop_poll_quit_request()==1 && ss_desktop_poll_quit_request()==0,"Native Quit request was not polled exactly once");passed("nativeQuitRequestPolled");
        js(L"document.getElementById('quit-app').click();true");wait([]{return js(L"document.getElementById('app-closed').hidden===false")=="true";},"Real Admin Quit was not acknowledged");passed("realAdminQuitAcknowledged");
        require(ss_desktop_choose_files(),"Could not open native chooser for shutdown verification");
        wait(chooserVisible,"Native shutdown-test chooser did not become visible");
        ss_windows_desktop_shutdown();require(!IsWindow(original),"Native shutdown left Admin open");passed("nativeQuitClosedAdminWindow");
        require(!ss_desktop_files_pending(),"Cancelling the chooser during shutdown queued unexpected files");passed("shutdownWithOpenNativeChooserCompleted");
        require(!std::filesystem::exists(desktop::directory),"Normal WebView shutdown left its private browser profile behind");passed("privateWebViewProfileRemovedAfterShutdown");report["status"]="\"passed\"";
        std::cout<<"{";bool firstField=true;for(auto& item:report) {if(!firstField)std::cout<<",";firstField=false;std::cout<<desktop::quote(item.first)<<":"<<item.second;}std::cout<<"}\n";return 0;
    } catch(const std::string& error) {std::cerr<<error<<"\n";} catch(const std::exception& error) {std::cerr<<error.what()<<"\n";}
    ss_windows_desktop_shutdown();return 5;
}
