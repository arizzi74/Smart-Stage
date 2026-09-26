package playlistfile

import (
	"syscall"
	"unsafe"
)

func replaceFile(from, to string) error {
	src, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	move := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
	ok, _, callErr := move.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 0x1|0x8)
	if ok == 0 {
		return callErr
	}
	return nil
}
