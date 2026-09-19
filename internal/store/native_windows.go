package store

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var moveFileEx = kernel32.NewProc("MoveFileExW")
var replace = kernel32.NewProc("ReplaceFileW")

func lockFile(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
func replaceFile(from, to string) error {
	src, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if _, err := os.Stat(to); err == nil {
		ok, _, e := replace.Call(uintptr(unsafe.Pointer(dst)), uintptr(unsafe.Pointer(src)), 0, 0, 0, 0)
		if ok == 0 {
			return e
		}
		return nil
	}
	ok, _, e := moveFileEx.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 0x1|0x8)
	if ok == 0 {
		return e
	}
	return nil
}
