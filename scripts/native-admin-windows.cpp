// Test-only own-process observer. Includes the production desktop implementation
// without adding a debug endpoint or privileged bridge to the shipped executable.
#include "../internal/platform/desktop_windows.cpp"
#include <algorithm>
#include <chrono>
#include <future>
#include <iostream>
#include <tuple>

namespace probe {
std::map<std::string,std::string> report;
void require(bool condition,const char* message) {if(!condition)throw std::string(message);}
void passed(const char* name) {report[name]="true";std::cerr<<"Native Admin probe passed: "<<name<<"\n";}
void emitReport() {std::cout<<"{";bool first=true;for(const auto& item:report) {if(!first)std::cout<<",";first=false;std::cout<<desktop::quote(item.first)<<":"<<item.second;}std::cout<<"}\n";}
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
std::vector<HWND> quitDialogs(HWND owner) {
    struct Search {HWND owner;std::vector<HWND> dialogs;};Search found{owner,{}};
    DWORD thread=GetWindowThreadProcessId(owner,nullptr);
    require(thread!=0,"Admin has no native UI thread");
    EnumThreadWindows(thread,[](HWND handle,LPARAM parameter)->BOOL {
        auto& found=*reinterpret_cast<Search*>(parameter);wchar_t kind[64]{},title[128]{};
        GetClassNameW(handle,kind,64);GetWindowTextW(handle,title,128);
        if(GetWindow(handle,GW_OWNER)==found.owner && IsWindowVisible(handle)
           && wcscmp(kind,L"#32770")==0 && wcscmp(title,L"Quit Smart Stage?")==0)found.dialogs.push_back(handle);
        return TRUE;
    },reinterpret_cast<LPARAM>(&found));
    return found.dialogs;
}
HWND beginQuitConfirmation(HWND owner) {
    require(quitDialogs(owner).empty(),"A quit confirmation was already open before closing Admin");
    // Use the actual system-close route asynchronously: its WM_CLOSE handler
    // enters MessageBox's modal pump and must not block this observer thread.
    require(PostMessageW(owner,WM_SYSCOMMAND,SC_CLOSE,0)!=FALSE,"Could not request native Admin close");
    HWND dialog=nullptr;
    wait([&]{auto found=quitDialogs(owner);require(found.size()<=1,"Closing Admin opened multiple quit confirmations");if(!found.empty())dialog=found.front();return dialog!=nullptr;},"Closing Admin did not show its owned native quit confirmation");
    require(IsWindowVisible(owner) && !IsIconic(owner) && !IsWindowEnabled(owner),"Quit confirmation did not keep its visible Admin owner modal");
    require(GetDlgItem(dialog,IDOK) && GetDlgItem(dialog,IDCANCEL),"Quit confirmation is missing OK or Cancel");
    DWORD_PTR defaultButton=0;
    require(SendMessageTimeoutW(dialog,DM_GETDEFID,0,0,SMTO_ABORTIFHUNG,2000,&defaultButton)!=0
            && HIWORD(defaultButton)==DC_HASDEFID && LOWORD(defaultButton)==IDCANCEL,"Quit confirmation must default to Cancel");
    bool warning=false;
    EnumChildWindows(dialog,[](HWND handle,LPARAM parameter)->BOOL {
        wchar_t text[512]{};GetWindowTextW(handle,text,512);
        if(wcsstr(text,L"Playback will stop and the stage will close."))*reinterpret_cast<bool*>(parameter)=true;
        return TRUE;
    },reinterpret_cast<LPARAM>(&warning));
    require(warning,"Quit confirmation does not explain that playback and the stage will stop");
    return dialog;
}
void clickQuitChoice(HWND dialog,int choice) {
    HWND button=GetDlgItem(dialog,choice);
    // Deliver the real control's notification. A hosted runner may have an
    // unrelated active shell dialog, where synthetic BM_CLICK can be ignored.
    require(button && PostMessageW(dialog,WM_COMMAND,MAKEWPARAM(choice,BN_CLICKED),reinterpret_cast<LPARAM>(button)),"Could not press the native quit confirmation button");
    wait([&]{return !IsWindow(dialog);},"Native quit confirmation did not close after its button was pressed");
}
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
struct DropHit {
    bool matches=false,adminChild=false,clientMatches=false,adminEnabled=false,external=false;
    POINT screen{};
    std::string evidence;
};
bool enabledWithParents(HWND handle) {
    if(!handle)return false;
    for(unsigned count=0;handle && count<64;++count) {
        if(!IsWindowEnabled(handle))return false;
        HWND parent=GetAncestor(handle,GA_PARENT);if(parent==handle)return false;handle=parent;
    }
    return handle==nullptr;
}
std::string describeWindow(HWND handle) {
            wchar_t name[256]{};DWORD pid=0;RECT area{};
            if(handle) {GetClassNameW(handle,name,256);GetWindowThreadProcessId(handle,&pid);GetWindowRect(handle,&area);}
            return "{\"hwnd\":"+std::to_string((uintptr_t)handle)+",\"class\":"+desktop::quote(desktop::utf8(name))+",\"pid\":"+std::to_string(pid)+",\"visible\":"+(IsWindowVisible(handle)?"true":"false")+",\"minimized\":"+(IsIconic(handle)?"true":"false")+",\"enabled\":"+(IsWindowEnabled(handle)?"true":"false")+",\"ancestorsEnabled\":"+(enabledWithParents(handle)?"true":"false")+",\"rect\":["+std::to_string(area.left)+","+std::to_string(area.top)+","+std::to_string(area.right)+","+std::to_string(area.bottom)+"]}";
}
DropHit observeDropHit(int xPercent=50,int yPercent=50) {
    return ui([xPercent,yPercent]{
        HWND admin=desktop::window;RECT bounds{};GetClientRect(admin,&bounds);
        require(bounds.right>64 && bounds.bottom>64,"Admin client area is too small for an interior drop target");
        POINT client{std::clamp(bounds.right*xPercent/100,32L,bounds.right-32),std::clamp(bounds.bottom*yPercent/100,32L,bounds.bottom-32)},screen=client;ClientToScreen(admin,&screen);
        HWND hit=WindowFromPoint(screen),root=hit?GetAncestor(hit,GA_ROOT):nullptr;
        DropHit result;result.screen=screen;result.matches=hit==admin;result.adminChild=hit && hit!=admin && (root==admin || IsChild(admin,hit));
        result.clientMatches=ChildWindowFromPointEx(admin,client,CWP_SKIPINVISIBLE|CWP_SKIPDISABLED|CWP_SKIPTRANSPARENT)==admin;
        result.adminEnabled=enabledWithParents(admin);result.external=hit && root && root!=admin && !result.adminChild;
        result.evidence="{\"point\":["+std::to_string(screen.x)+","+std::to_string(screen.y)+"],\"clientPoint\":["+std::to_string(client.x)+","+std::to_string(client.y)+"],\"clientSize\":["+std::to_string(bounds.right)+","+std::to_string(bounds.bottom)+"],\"admin\":"+describeWindow(admin)+",\"hit\":"+describeWindow(hit)+",\"hitRoot\":"+describeWindow(root)+",\"foreground\":"+describeWindow(GetForegroundWindow())+",\"directChild\":"+describeWindow(ChildWindowFromPointEx(admin,client,CWP_SKIPINVISIBLE|CWP_SKIPDISABLED|CWP_SKIPTRANSPARENT))+",\"exactAdminHit\":"+(result.matches?"true":"false")+",\"adminChildHit\":"+(result.adminChild?"true":"false")+"}";
        return result;
    });
}
// A plain unowned Win32 window must exhibit the same global occlusion before
// classifying the failure as a hosted-desktop limitation rather than Admin UI.
void independentDropHitBaseline(POINT screen) {
    constexpr auto className=L"SmartStageProbe-PlainDropBaseline";
    HWND baseline=ui([screen]{
        WNDCLASSW type{};type.lpfnWndProc=DefWindowProcW;type.hInstance=GetModuleHandleW(nullptr);type.lpszClassName=L"SmartStageProbe-PlainDropBaseline";
        require(RegisterClassW(&type)!=0,"Could not register independent drop baseline class");
        HWND handle=CreateWindowExW(WS_EX_TOPMOST,L"SmartStageProbe-PlainDropBaseline",L"Smart Stage native drop baseline",WS_POPUP|WS_VISIBLE,screen.x-64,screen.y-64,128,128,nullptr,nullptr,type.hInstance,nullptr);
        require(handle && IsWindow(handle),"Could not create independent drop baseline window");
        require(SetWindowPos(handle,HWND_TOPMOST,screen.x-64,screen.y-64,128,128,SWP_SHOWWINDOW|SWP_NOACTIVATE)!=FALSE,"Could not position independent drop baseline");
        return handle;
    });
    struct Cleanup {
        HWND handle;const wchar_t* name;
        ~Cleanup(){try {ui([this]{if(IsWindow(handle))DestroyWindow(handle);UnregisterClassW(name,GetModuleHandleW(nullptr));report["globalDropHitBaselineDestroyed"]=IsWindow(handle)?"false":"true";});}catch(...) {}}
    } cleanup{baseline,className};
    bool valid=true,anyExact=false,allExternal=true;unsigned count=0;std::string observations="[";
    auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(3);
    do {
        auto result=ui([baseline,screen]{
            POINT client=screen;ScreenToClient(baseline,&client);RECT size{};GetClientRect(baseline,&size);
            HWND hit=WindowFromPoint(screen),root=hit?GetAncestor(hit,GA_ROOT):nullptr;
            HWND child=ChildWindowFromPointEx(baseline,client,CWP_SKIPINVISIBLE|CWP_SKIPDISABLED|CWP_SKIPTRANSPARENT);
            bool exact=hit==baseline;
            bool inputs=IsWindow(baseline) && IsWindowVisible(baseline) && !IsIconic(baseline) && enabledWithParents(baseline) && !GetWindow(baseline,GW_OWNER)
                && (GetWindowLongPtrW(baseline,GWL_EXSTYLE)&WS_EX_TOPMOST) && child==baseline
                && client.x>=32 && client.x<=size.right-32 && client.y>=32 && client.y<=size.bottom-32
                && SendMessageW(baseline,WM_NCHITTEST,0,MAKELPARAM(screen.x,screen.y))==HTCLIENT;
            bool external=hit && root && root!=baseline && root!=desktop::window && !IsChild(baseline,hit) && !IsChild(desktop::window,hit);
            std::string evidence="{\"point\":["+std::to_string(screen.x)+","+std::to_string(screen.y)+"],\"clientPoint\":["+std::to_string(client.x)+","+std::to_string(client.y)+"],\"clientSize\":["+std::to_string(size.right)+","+std::to_string(size.bottom)+"],\"baseline\":"+describeWindow(baseline)+",\"hit\":"+describeWindow(hit)+",\"hitRoot\":"+describeWindow(root)+",\"directChild\":"+describeWindow(child)+",\"foreground\":"+describeWindow(GetForegroundWindow())+",\"unowned\":"+(!GetWindow(baseline,GW_OWNER)?"true":"false")+",\"topmost\":"+((GetWindowLongPtrW(baseline,GWL_EXSTYLE)&WS_EX_TOPMOST)?"true":"false")+",\"nonClientHitIsClient\":"+(SendMessageW(baseline,WM_NCHITTEST,0,MAKELPARAM(screen.x,screen.y))==HTCLIENT?"true":"false")+",\"exactBaselineHit\":"+(exact?"true":"false")+",\"externalHit\":"+(external?"true":"false")+"}";
            return std::make_tuple(inputs,exact,external,evidence);
        });
        valid=valid && std::get<0>(result);anyExact=anyExact || std::get<1>(result);allExternal=allExternal && std::get<2>(result);
        if(count++)observations+=',';observations+=std::get<3>(result);
        report["globalDropHitBaseline"]="{\"class\":\"SmartStageProbe-PlainDropBaseline\",\"windowProcedure\":\"DefWindowProcW\",\"point\":["+std::to_string(screen.x)+","+std::to_string(screen.y)+"],\"validInputs\":"+(valid?"true":"false")+",\"anyExactGlobalHit\":"+(anyExact?"true":"false")+",\"allGlobalHitsExternal\":"+(allExternal?"true":"false")+",\"observations\":"+observations+"]}";
        if(!valid || anyExact || !allExternal)break;
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }while(std::chrono::steady_clock::now()<deadline);
    require(valid,"Independent plain drop baseline has invalid inputs; desktop capability is unresolved");
    require(!anyExact,"Independent plain Win32 window is globally hittable while Admin is not; investigate Admin hit testing");
    require(allExternal && count>=2,"Independent plain drop baseline did not confirm external desktop occlusion");
    require(ui([baseline]{return DestroyWindow(baseline)!=FALSE && !IsWindow(baseline);}),"Could not destroy the independent drop baseline");
    report["globalDropHitBaselineDestroyed"]="true";
    std::cerr<<"Native drop independent plain Win32 baseline is also externally occluded: "<<report["globalDropHitBaseline"]<<"\n";
}
struct DropSurface {
    bool raised=false,globalExact=false,allGridExternal=true,allGridClient=true,allGridEnabled=true,gridChild=false;
    unsigned gridCount=0;
    POINT point{};
    std::string initial,settled,beforeRaise="[]",afterRaise="[]";
    DropHit interiorGrid(DropHit previous,std::string& evidence) {
        evidence="[";bool first=true,selected=false;DropHit firstSample;
        for(int y:{20,35,50,65,80})for(int x:{20,35,50,65,80}) {
            auto candidate=observeDropHit(x,y);
            if(!first)evidence+=',';else firstSample=candidate;first=false;evidence+=candidate.evidence;
            ++gridCount;allGridExternal=allGridExternal && candidate.external;allGridClient=allGridClient && candidate.clientMatches;
            allGridEnabled=allGridEnabled && candidate.adminEnabled;gridChild=gridChild || candidate.adminChild;
            if(candidate.matches && !selected) {previous=candidate;selected=true;}
        }
        evidence+=']';return selected?previous:firstSample;
    }
    void retain(const DropHit& hit) {
        report["nativeDropHitTest"]="{\"initial\":"+initial+",\"afterRestoreSettled\":"+settled+",\"interiorCandidatesBeforeRaise\":"+beforeRaise+",\"interiorCandidatesAfterRaise\":"+afterRaise+",\"final\":"+hit.evidence+",\"temporarilyRaisedAboveExternalWindow\":"+(raised?"true":"false")+"}";
    }
    void prepare() {
        auto hit=observeDropHit();initial=hit.evidence;
        auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(3);
        while(!hit.matches && std::chrono::steady_clock::now()<deadline) {std::this_thread::sleep_for(std::chrono::milliseconds(100));hit=observeDropHit();}
        settled=hit.evidence;
        retain(hit);
        if(!hit.matches)std::cerr<<"Native drop hit after restore: "<<settled<<"\n";
        require(!hit.adminChild,"A child of Admin intercepts the file-drop point; see class/PID hit diagnostics");
        // A shell dialog may cover only the center. Use a real uncovered point
        // within the middle 20–80% of the WebView client, never its frame/edge.
        if(!hit.matches) {hit=interiorGrid(hit,beforeRaise);retain(hit);}
        require(!gridChild,"An Admin child intercepts an interior drop sample; see retained hit evidence");
        if(!hit.matches) {
            // An unrelated runner window can cover the restored app. Establish
            // a controlled drop surface only in this own-process test observer.
            require(ui([]{return !(GetWindowLongPtrW(desktop::window,GWL_EXSTYLE)&WS_EX_TOPMOST);}),"Probe Admin was already topmost before controlled drop");
            raised=ui([]{return SetWindowPos(desktop::window,HWND_TOPMOST,0,0,0,0,SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)!=FALSE;});
            require(raised,"Could not raise the owned probe window above unrelated desktop UI");
            deadline=std::chrono::steady_clock::now()+std::chrono::seconds(3);
            do {hit=observeDropHit();if(hit.matches || hit.adminChild)break;std::this_thread::sleep_for(std::chrono::milliseconds(100));}while(std::chrono::steady_clock::now()<deadline);
            retain(hit);
            require(!hit.adminChild,"An Admin child intercepts the raised drop target; see hit diagnostics");
            if(!hit.matches) {hit=interiorGrid(hit,afterRaise);retain(hit);}
        }
        point=hit.screen;
        retain(hit);
        if(!hit.matches)std::cerr<<"Native drop final hit: "<<hit.evidence<<"\n";
        require(!gridChild && hit.clientMatches && allGridClient,"An Admin child intercepts the native client drop target");
        require(hit.adminEnabled && allGridEnabled,"Admin or an ancestor is disabled and cannot accept native drops");
        globalExact=hit.matches;
        if(!globalExact) {
            require(raised && gridCount==50 && allGridExternal && allGridClient && hit.external,"Global drop hit testing failed without conclusive external desktop occlusion");
            independentDropHitBaseline(point);
            std::cerr<<"Native drop probe: unrelated desktop windows cover all 50 interior samples; verifying the enabled Admin client OLE handler without claiming a global desktop hit\n";
        }
        report["globalDesktopHitTest"]=desktop::quote(globalExact?"exact-admin-hit":"externally-occluded");
        report["globalExactHit"]=globalExact?"true":"false";
    }
    void restore() {
        if(raised) {require(ui([]{return SetWindowPos(desktop::window,HWND_NOTOPMOST,0,0,0,0,SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)!=FALSE;}),"Could not remove test-only topmost positioning");raised=false;}
        require(ui([]{return !(GetWindowLongPtrW(desktop::window,GWL_EXSTYLE)&WS_EX_TOPMOST);}),"Test-only drop positioning left Admin topmost");
        passed("nativeDropTestPositioningRestored");
    }
    ~DropSurface() {if(raised)try {ui([]{SetWindowPos(desktop::window,HWND_NOTOPMOST,0,0,0,0,SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE);});}catch(...) {}}
};
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
int main(int argc,char** argv) {
    using namespace probe;
    // Match production ss_init before creating any native UI or WebView.
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    try {
        auto* addressWide=_wgetenv(L"SMARTSTAGE_PROBE_ADMIN_URL");auto* first=_wgetenv(L"SMARTSTAGE_PROBE_ORIGINAL_ONE");auto* second=_wgetenv(L"SMARTSTAGE_PROBE_ORIGINAL_TWO");
        require(addressWide&&first&&second,"Missing test-only probe environment");
        auto address=desktop::utf8(addressWide);
        ss_desktop_identity("native-probe");ss_desktop_admin(address.c_str());ui([]{});
        require(ss_desktop_show_admin(),"Native Admin show was rejected");
        bool shutdownConfirmation=argc==2 && strcmp(argv[1],"--shutdown-confirmation")==0;
        wait([]{return js(L"typeof role!=='undefined' && role==='admin' && !localSessionBusy && typeof csrf!=='undefined' && csrf.length>0 && source?.readyState===EventSource.OPEN && state?.cues?.length===1 && document.getElementById('connection')?.textContent==='Connected to host'")=="true";},"Real Admin did not authenticate and connect its EventSource");
        if(shutdownConfirmation) {
            HWND original=ui([]{return desktop::window;});HWND dialog=beginQuitConfirmation(original);
            require(ss_desktop_poll_quit_request()==0,"Opening a quit confirmation prematurely requested shutdown");
            passed("quitConfirmationOpenBeforeExternalShutdown");
            auto started=std::chrono::steady_clock::now();ss_windows_desktop_shutdown();
            require(!IsWindow(original) && !IsWindow(dialog),"Native shutdown left Admin or its confirmation open");
            require(ss_desktop_poll_quit_request()==0,"Dismissing a confirmation for native shutdown queued a new quit request");
            report["shutdownMilliseconds"]=std::to_string(std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now()-started).count());
            passed("shutdownWithOpenQuitConfirmationCompleted");
            require(!std::filesystem::exists(desktop::directory),"Shutdown with an open confirmation left the private browser profile behind");
            passed("privateWebViewProfileRemovedAfterShutdown");report["status"]="\"passed\"";emitReport();return 0;
        }
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
            report["trayUnavailableReason"]=desktop::quote("The host shell rejected all six independent Win32 registrations with valid window and system icon; native close confirmation is verified independently of tray availability.");
            std::cerr<<"Native Admin probe: host notification area unavailable; close confirmation remains independent of the tray\n";
        }
        report["trayIconRegistered"]=ui([]{return desktop::trayAdded;})?"true":"false";
        report["trayCapability"]=desktop::quote(report["trayIconRegistered"]=="true"?"available":"unavailable");
        js(L"window.__probeEpoch=state.stopEpoch;window.__probeDraft='preserved';document.getElementById('remote-connection-settings').open=true;document.getElementById('gateway-url').value='https://unsaved.example/smartstage';document.getElementById('gateway-url').dispatchEvent(new Event('input',{bubbles:true}));true");
        auto stopPosition=js(L"(()=>{const r=document.getElementById('stop').getBoundingClientRect();return [Math.round((r.left+r.width/2)*devicePixelRatio),Math.round((r.top+r.height/2)*devicePixelRatio)]})()");
        long stopX=0,stopY=0;require(sscanf(stopPosition.c_str(),"[%ld,%ld]",&stopX,&stopY)==2,"Could not locate rendered STOP button");
        ui([&]{SendMessageW(desktop::window,WM_LBUTTONDOWN,MK_LBUTTON,MAKELPARAM(stopX,stopY));SendMessageW(desktop::window,WM_LBUTTONUP,0,MAKELPARAM(stopX,stopY));});
        wait([]{return js(L"state.stopEpoch>window.__probeEpoch && online && source.readyState===EventSource.OPEN")=="true";},"Native mouse input did not activate real CSRF-protected STOP");passed("compositionMouseInputActivatedStop");passed("realCSRFProtectedStopAccepted");passed("liveStateAfterStopObserved");
        const wchar_t* playbackState=L"JSON.stringify({state:state.state,stopEpoch:state.stopEpoch,stageEnabled:state.stageEnabled,activeCueId:state.activeCueId})";
        require(js(L"typeof state.state==='string' && typeof state.stopEpoch==='number' && typeof state.stageEnabled==='boolean' && typeof state.activeCueId==='string'")=="true","Host playback snapshot is missing required fields");
        auto beforeClose=js(playbackState);
        HWND confirmation=beginQuitConfirmation(original);
        passed("closeShowsOwnedNativeConfirmation");passed("quitConfirmationDefaultsToCancel");
        require(ss_desktop_poll_quit_request()==0 && ss_desktop_poll_emergency_request()==0,"Opening the confirmation requested Quit or STOP before a choice");
        require(PostMessageW(original,WM_CLOSE,0,0) && PostMessageW(original,WM_CLOSE,0,0),"Could not repeat Admin close while confirming");
        ui([]{});auto openDialogs=quitDialogs(original);
        require(openDialogs.size()==1 && openDialogs.front()==confirmation,"Repeated close created a nested confirmation");
        passed("repeatedCloseKeepsSingleConfirmation");
        clickQuitChoice(confirmation,IDCANCEL);
        wait([&]{return IsWindowEnabled(original)!=FALSE;},"Cancel did not re-enable Admin");
        require(IsWindowVisible(original) && !IsIconic(original),"Cancel hid or minimized Admin");
        require(ui([&]{BOOL visible=FALSE;return desktop::window==original && desktop::webview.p==web && desktop::controller
            && SUCCEEDED(desktop::controller->get_IsVisible(&visible)) && visible;}),"Cancel replaced or hid the existing Admin WebView");
        require(ss_desktop_poll_quit_request()==0 && ss_desktop_poll_emergency_request()==0,"Cancel queued an unexpected Quit or STOP");
        require(js(playbackState)==beforeClose,"Cancel changed host playback or stage state");
        passed("cancelKeepsAdminAndPlaybackState");
        confirmation=beginQuitConfirmation(original);
        require(PostMessageW(confirmation,WM_CLOSE,0,0)!=FALSE,"Could not close the quit confirmation itself");
        wait([&]{return !IsWindow(confirmation) && IsWindowEnabled(original);},"Closing the confirmation did not cancel it");
        require(ss_desktop_poll_quit_request()==0 && IsWindowVisible(original),"Closing the confirmation requested Quit or hid Admin");
        passed("closingConfirmationCancelsQuit");report["closeBehavior"]=desktop::quote("confirm-quit-or-cancel");
        ss_desktop_show_admin();ss_desktop_show_admin();wait([&]{return IsWindowVisible(original)!=FALSE && !IsIconic(original);},"Reopen did not restore Admin");
        require(ui([&]{return desktop::window==original&&desktop::webview.p==web;}),"Reopen replaced Admin or its WebView");
        const auto* draftPreserved=L"window.__probeDraft==='preserved' && document.getElementById('gateway-url').value==='https://unsaved.example/smartstage' && document.getElementById('remote-connection-settings').open";
        auto reopenDiagnostics=[]{std::cerr<<"Reopened Admin components: "<<js(L"({draft:window.__probeDraft,url:document.getElementById('gateway-url').value,settingsOpen:document.getElementById('remote-connection-settings').open,gatewayDirty,online,eventSourceState:source?.readyState,hidden:document.hidden})")<<"\n";};
        if(js(draftPreserved)!="true") {reopenDiagnostics();throw std::string("Reopen lost unsaved Admin fields");}
        // Visibility restoration deliberately reconnects the live stream. Drafts
        // must survive immediately; connectivity must recover asynchronously.
        try {wait([]{return js(L"online && source?.readyState===EventSource.OPEN")=="true";},"Reopened Admin did not restore its live connection");}
        catch(...) {reopenDiagnostics();throw;}
        if(js(draftPreserved)!="true") {reopenDiagnostics();throw std::string("Reconnect replaced unsaved Admin fields");}
        passed("reopenKeptSameWindowAndWebView");passed("unsavedUIAndLiveConnectionPreserved");
        ui([]{SendMessageW(desktop::window,WM_KEYDOWN,VK_ESCAPE,1);});
        require(ss_desktop_poll_emergency_request()==1 && ss_desktop_poll_emergency_request()==0,"Native Admin Escape did not queue exactly one emergency request");
        ui([]{SendMessageW(desktop::window,WM_KEYDOWN,VK_ESCAPE,(LPARAM(1)<<30)|1);});
        require(ss_desktop_poll_emergency_request()==0,"Held Escape repeated emergency requests");passed("nativeAdminEscapePolledWithoutJavaScript");
        require(!ss_desktop_files_pending(),"Native queue was not empty before drop");
        std::wstring firstPath=first,secondPath=second;
        DropSurface dropSurface;dropSurface.prepare();
        ui([&]{
            POINT location=dropSurface.point,client=location;RECT bounds{};GetClientRect(desktop::window,&bounds);ScreenToClient(desktop::window,&client);
            require(client.x>=32 && client.y>=32 && client.x<=bounds.right-32 && client.y<=bounds.bottom-32,"Selected drop point moved outside the client interior");
            require(enabledWithParents(desktop::window),"Admin became disabled before native drop delivery");
            require(ChildWindowFromPointEx(desktop::window,client,CWP_SKIPINVISIBLE|CWP_SKIPDISABLED|CWP_SKIPTRANSPARENT)==desktop::window,"An Admin child intercepted the selected delivery point");
            HWND actual=WindowFromPoint(location),root=actual?GetAncestor(actual,GA_ROOT):nullptr;
            if(dropSurface.globalExact)require(actual==desktop::window,"Native drop target changed after the recorded hit test");
            else require(actual && root && root!=desktop::window && !IsChild(desktop::window,actual),"External desktop occlusion changed before native drop delivery");
            report["nativeDropDeliveryPoint"]="["+std::to_string(location.x)+","+std::to_string(location.y)+"]";
            auto* object=new Files({firstPath,secondPath});DWORD effect=DROPEFFECT_COPY;POINTL point{location.x,location.y};
            require(SUCCEEDED(desktop::dropTarget->DragEnter(object,0,point,&effect))&&effect==DROPEFFECT_COPY,"Native drop enter was rejected");
            effect=DROPEFFECT_COPY;require(SUCCEEDED(desktop::dropTarget->Drop(object,0,point,&effect))&&effect==DROPEFFECT_COPY,"Native Explorer-format drop was rejected");object->Release();
        });
        dropSurface.restore();
        report["publicHitTestTargetsNativeAdminDropWindow"]=dropSurface.globalExact?"true":"false";
        passed("nativeClientHitTestTargetsAdminDropWindow");passed("nativeCFHDROPDeliveredToProductionDropTarget");report["dropRequest"]=popRequest(2);
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
        confirmation=beginQuitConfirmation(original);clickQuitChoice(confirmation,IDOK);
        wait([]{return ss_desktop_poll_quit_request()==1;},"Confirming close did not queue the normal Quit request");
        require(ss_desktop_poll_quit_request()==0,"Confirming close queued more than one Quit request");
        require(IsWindow(original),"Confirming close destroyed Admin before the host could perform orderly shutdown");
        passed("confirmedCloseQueuesOneOrderlyQuit");
        require(PostMessageW(original,WM_CLOSE,0,0)!=FALSE,"Could not repeat close after confirmation");ui([]{});
        require(quitDialogs(original).empty() && ss_desktop_poll_quit_request()==0,"Repeated close after confirmation reopened the prompt or queued another Quit");
        passed("confirmedCloseDoesNotPromptAgain");
        js(L"document.getElementById('quit-app').click();true");wait([]{return js(L"document.getElementById('app-closed').hidden===false")=="true";},"Real Admin Quit was not acknowledged");passed("realAdminQuitAcknowledged");
        require(ss_desktop_choose_files(),"Could not open native chooser for shutdown verification");
        wait(chooserVisible,"Native shutdown-test chooser did not become visible");
        ss_windows_desktop_shutdown();require(!IsWindow(original),"Native shutdown left Admin open");passed("nativeQuitClosedAdminWindow");
        require(!ss_desktop_files_pending(),"Cancelling the chooser during shutdown queued unexpected files");passed("shutdownWithOpenNativeChooserCompleted");
        require(!std::filesystem::exists(desktop::directory),"Normal WebView shutdown left its private browser profile behind");passed("privateWebViewProfileRemovedAfterShutdown");report["status"]="\"passed\"";
        emitReport();return 0;
    } catch(const std::string& error) {std::cerr<<error<<"\n";} catch(const std::exception& error) {std::cerr<<error.what()<<"\n";}
    ss_windows_desktop_shutdown();report["status"]="\"failed\"";emitReport();return 5;
}
