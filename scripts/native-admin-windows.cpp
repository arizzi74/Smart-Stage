// Test-only own-process observer. Includes the production desktop implementation
// without adding a debug endpoint or privileged bridge to the shipped executable.
#include "../internal/platform/desktop_windows.cpp"
#include <chrono>
#include <future>
#include <iostream>

namespace probe {
std::map<std::string,std::string> report;
void require(bool condition,const char* message) {if(!condition)throw std::string(message);}
void passed(const char* name) {report[name]="true";}
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
        ss_desktop_identity("native-probe");ss_desktop_admin(address.c_str());require(ss_desktop_show_admin(),"Native Admin show was rejected");
        wait([]{return js(L"typeof role!=='undefined' && role==='admin' && !localSessionBusy && typeof csrf!=='undefined' && csrf.length>0 && source?.readyState===EventSource.OPEN && state?.cues?.length===1 && document.getElementById('connection')?.textContent==='Connected to host'")=="true";},"Real Admin did not authenticate and connect its EventSource");
        passed("realAdminAssetsLoadedInWebView2");passed("localSessionAndCSRFInitialized");passed("authenticatedEventSourceConnected");
        require(js(L"navigator.userAgent.includes('SmartStageDesktop') && navigator.userAgent.includes('SmartStageWindowsDesktop') && document.getElementById('playlist').children.length>0 && !document.getElementById('choose-files').hidden && document.getElementById('media-path-settings').hidden")=="true","Desktop UI identity or chooser capability missing");
        passed("nativeUserAgentAndPlaylistRendered");
        HWND original=ui([]{return desktop::window;});auto* web=ui([]{return desktop::webview.p;});
        require(ui([]{return IsWindowVisible(desktop::window)!=FALSE && desktop::composition.p && desktop::controller.p && desktop::trayAdded;}),"Composition Admin window and tray not visible");passed("compositionWindowAndTrayVisible");
        js(L"window.__probeEpoch=state.stopEpoch;window.__probeDraft='preserved';document.getElementById('remote-connection-settings').open=true;document.getElementById('gateway-url').value='https://unsaved.example/smartstage';document.getElementById('gateway-url').dispatchEvent(new Event('input',{bubbles:true}));true");
        auto stopPosition=js(L"(()=>{const r=document.getElementById('stop').getBoundingClientRect();return [Math.round((r.left+r.width/2)*devicePixelRatio),Math.round((r.top+r.height/2)*devicePixelRatio)]})()");
        long stopX=0,stopY=0;require(sscanf(stopPosition.c_str(),"[%ld,%ld]",&stopX,&stopY)==2,"Could not locate rendered STOP button");
        ui([&]{SendMessageW(desktop::window,WM_LBUTTONDOWN,MK_LBUTTON,MAKELPARAM(stopX,stopY));SendMessageW(desktop::window,WM_LBUTTONUP,0,MAKELPARAM(stopX,stopY));});
        wait([]{return js(L"state.stopEpoch>window.__probeEpoch && online && source.readyState===EventSource.OPEN")=="true";},"Native mouse input did not activate real CSRF-protected STOP");passed("compositionMouseInputActivatedStop");passed("realCSRFProtectedStopAccepted");passed("liveStateAfterStopObserved");
        ui([]{SendMessageW(desktop::window,WM_CLOSE,0,0);});require(!IsWindowVisible(original),"Closing Admin did not hide its window");passed("closeHidWindowWithoutTerminating");
        ss_desktop_show_admin();ss_desktop_show_admin();wait([&]{return IsWindowVisible(original)!=FALSE;},"Reopen did not restore Admin");
        require(ui([&]{return desktop::window==original&&desktop::webview.p==web;}),"Reopen replaced Admin or its WebView");
        require(js(L"window.__probeDraft==='preserved' && document.getElementById('gateway-url').value==='https://unsaved.example/smartstage' && document.getElementById('remote-connection-settings').open && online")=="true","Reopen lost unsaved UI or its connection");passed("reopenKeptSameWindowAndWebView");passed("unsavedUIAndLiveConnectionPreserved");
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
        wait([]{return ui([]{return bool(desktop::chooser);});},"Native Windows file dialog did not open");
        ui([&]{desktop::check(desktop::chooser->SetFileName(secondPath.c_str()),"Select original file in native chooser");desktop::Ptr<IOleWindow> native;desktop::check(desktop::chooser->QueryInterface(IID_PPV_ARGS(native.out())),"Observe native file dialog");HWND dialog=nullptr;desktop::check(native->GetWindow(&dialog),"Find native chooser HWND");require(dialog&&IsWindowVisible(dialog),"Native file dialog is not visible");PostMessageW(dialog,WM_COMMAND,IDOK,0);});
        wait([]{return ss_desktop_files_pending()!=0;},"Selecting the original in the real native chooser did not queue it");report["chooserRequest"]=popRequest(1);passed("nativeFileDialogOriginalSelectionAccepted");
        auto blocked=desktop::wide(address.c_str());blocked.resize(blocked.size()-6);blocked+=L"/command";
        ui([&]{desktop::webview->Navigate(blocked.c_str());});std::this_thread::sleep_for(std::chrono::milliseconds(400));
        require(js(L"window.__probeDraft==='preserved' && location.pathname==='/admin'")=="true","Navigation escaped Admin or lost the existing document");passed("navigationOutsideAdminBlocked");
        js(L"window.__probeBlockedFetch=false;fetch('http://127.0.0.1:1/api/state').then(r=>window.__probeBlockedFetch=r.status===403).catch(()=>window.__probeBlockedFetch=true);true");
        wait([]{return js(L"window.__probeBlockedFetch===true")=="true";},"Cross-origin resource was not blocked");passed("crossOriginResourcesBlocked");
        // App's own native Quit is polled; it never terminates a process behind
        // Go's back. Then verify authenticated UI Quit against the real host.
        ui([]{desktop::command(desktop::quitID);});require(ss_desktop_poll_quit_request()==1 && ss_desktop_poll_quit_request()==0,"Native Quit request was not polled exactly once");passed("nativeQuitRequestPolled");
        js(L"document.getElementById('quit-app').click();true");wait([]{return js(L"document.getElementById('app-closed').hidden===false")=="true";},"Real Admin Quit was not acknowledged");passed("realAdminQuitAcknowledged");
        ss_windows_desktop_shutdown();require(!IsWindow(original),"Native shutdown left Admin open");passed("nativeQuitClosedAdminWindow");
        require(!std::filesystem::exists(desktop::directory),"Normal WebView shutdown left its private browser profile behind");passed("privateWebViewProfileRemovedAfterShutdown");report["status"]="\"passed\"";
        std::cout<<"{";bool firstField=true;for(auto& item:report) {if(!firstField)std::cout<<",";firstField=false;std::cout<<desktop::quote(item.first)<<":"<<item.second;}std::cout<<"}\n";return 0;
    } catch(const std::string& error) {std::cerr<<error<<"\n";} catch(const std::exception& error) {std::cerr<<error.what()<<"\n";}
    ss_windows_desktop_shutdown();return 5;
}
