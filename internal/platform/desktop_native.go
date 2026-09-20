//go:build (darwin || windows) && cgo

package platform

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

var desktopFiles = make(chan DesktopFileRequest, MaxDesktopFileRequests)
var desktopAdminRequests = make(chan struct{}, 1)

// DesktopAdminRequests coalesces native Dock/menu/file-import requests. The Go
// application decides whether to reuse an existing Admin page or open its URL.
func DesktopAdminRequests() <-chan struct{} { return desktopAdminRequests }

// DesktopCanChooseFiles reports whether the native desktop is ready for a
// picker. It does not show a window or access files.
func DesktopCanChooseFiles() bool { return C.ss_desktop_can_choose_files() != 0 }

// DesktopChooseFiles queues the native picker or reuses its pending/open panel.
// False means this process has no available native desktop picker.
func DesktopChooseFiles() bool { return C.ss_desktop_choose_files() != 0 }

// DesktopActivateBrowser queues activation of the running default browser. It
// never opens a URL, starts a browser process, or selects a particular tab.
func DesktopActivateBrowser() bool { return C.ss_desktop_activate_browser() != 0 }

// DesktopHasAdminWindow is a static native desktop capability. It does
// not depend on the asynchronous registration of the Admin URL.
func DesktopHasAdminWindow() bool { return C.ss_desktop_has_admin_window() != 0 }

// DesktopShowAdmin queues show/focus of the one retained native Admin window.
// A request made before DesktopAdmin registers its URL waits for registration.
func DesktopShowAdmin() bool { return C.ss_desktop_show_admin() != 0 }

func DesktopFiles() <-chan DesktopFileRequest { return desktopFiles }

func DesktopFilesPending() bool { return C.ss_desktop_files_pending() != 0 }

func DesktopFileResult(id uint64, message string) {
	p := C.CString(message)
	defer C.free(unsafe.Pointer(p))
	C.ss_desktop_files_result(C.uint64_t(id), p)
}

func pollDesktop() {
	pollDesktopQuit()
	if len(desktopAdminRequests) < cap(desktopAdminRequests) && C.ss_desktop_poll_admin_request() != 0 {
		desktopAdminRequests <- struct{}{}
	}
	for i := 0; i < MaxDesktopFileRequests && len(desktopFiles) < cap(desktopFiles); i++ {
		p := C.ss_desktop_poll_files()
		if p == nil {
			return
		}
		var request DesktopFileRequest
		if err := json.Unmarshal([]byte(readString(p)), &request); err != nil || request.ID == 0 || len(request.Paths) == 0 || len(request.Paths) > MaxDesktopFiles {
			continue
		}
		desktopFiles <- request
	}
}

// DesktopAdmin enables the native menu using the actual bound Admin URL.
// On macOS it is enabled only for an app-bundle launch.
func DesktopAdmin(url string) {
	p := C.CString(url)
	defer C.free(unsafe.Pointer(p))
	C.ss_desktop_admin(p)
}

// DesktopError must run after Run has returned (on the main thread for Cocoa).
func DesktopError(message string) {
	p := C.CString(message)
	defer C.free(unsafe.Pointer(p))
	C.ss_desktop_error(p)
}
