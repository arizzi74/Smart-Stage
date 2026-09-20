package browseropen

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

func open(address string) error {
	// ShellExecute can activate COM-based URL handlers. Keep its apartment on
	// this thread through dispatch. Playback owns its separate native MTA.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2|4) // STA, disable OLE1 DDE
	if int32(hr) >= 0 {
		defer ole.NewProc("CoUninitialize").Call()
	} else if uint32(hr) != 0x80010106 { // An existing MTA is still usable for URL dispatch.
		return fmt.Errorf("initialize browser COM apartment: 0x%08x", uint32(hr))
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(address)
	if err != nil {
		return err
	}
	result, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(
		0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1,
	)
	if result <= 32 {
		return fmt.Errorf("system browser launch failed (ShellExecute code %d)", result)
	}
	return nil
}
