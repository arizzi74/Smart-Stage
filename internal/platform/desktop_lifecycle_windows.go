//go:build windows && cgo

package platform

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unsafe"
)

var desktopQuitRequests = make(chan struct{}, 1)
var desktopEmergencyRequests = make(chan struct{}, 1)

func DesktopQuitRequests() <-chan struct{}      { return desktopQuitRequests }
func DesktopEmergencyRequests() <-chan struct{} { return desktopEmergencyRequests }

func pollDesktopQuit() {
	if len(desktopEmergencyRequests) < cap(desktopEmergencyRequests) && C.ss_desktop_poll_emergency_request() != 0 {
		desktopEmergencyRequests <- struct{}{}
	}
	if len(desktopQuitRequests) < cap(desktopQuitRequests) && C.ss_desktop_poll_quit_request() != 0 {
		desktopQuitRequests <- struct{}{}
	}
}

func desktopIdentity(config string) *C.char {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(config))))
	return C.CString(hex.EncodeToString(hash[:]))
}

// DesktopConfigure selects the retained window identity before Run starts.
func DesktopConfigure(config string) {
	key := desktopIdentity(config)
	defer C.free(unsafe.Pointer(key))
	C.ss_desktop_identity(key)
	logPath := C.CString(filepath.Join(config, "smartstage.log"))
	defer C.free(unsafe.Pointer(logPath))
	C.ss_desktop_log_path(logPath)
}

// DesktopReopen requests focus only; no file path or command is sent to a peer.
func DesktopReopen(config string) bool {
	key := desktopIdentity(config)
	defer C.free(unsafe.Pointer(key))
	return C.ss_desktop_reopen(key) != 0
}
