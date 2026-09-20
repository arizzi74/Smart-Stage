//go:build darwin && cgo

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

func DesktopFiles() <-chan DesktopFileRequest { return desktopFiles }

func DesktopFilesPending() bool { return C.ss_desktop_files_pending() != 0 }

func DesktopFileResult(id uint64, message string) {
	p := C.CString(message)
	defer C.free(unsafe.Pointer(p))
	C.ss_desktop_files_result(C.uint64_t(id), p)
}

func pollDesktopFiles() {
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

// DesktopAdmin enables the app's menu using the actual bound Admin URL.
// It is a no-op for an executable launched directly from a terminal.
func DesktopAdmin(url string) {
	p := C.CString(url)
	defer C.free(unsafe.Pointer(p))
	C.ss_desktop_admin(p)
}

// DesktopError must run on the process main thread, after Run has returned.
func DesktopError(message string) {
	p := C.CString(message)
	defer C.free(unsafe.Pointer(p))
	C.ss_desktop_error(p)
}
