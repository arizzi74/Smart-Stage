//go:build darwin && cgo

package platform

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import "unsafe"

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
