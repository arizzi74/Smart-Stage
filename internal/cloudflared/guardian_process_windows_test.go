//go:build windows

package cloudflared

import "syscall"

func guardianTestProcessExited(pid int) bool {
	const synchronize = 0x00100000
	handle, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return true
	}
	defer syscall.CloseHandle(handle)
	status, err := syscall.WaitForSingleObject(handle, 0)
	return err == nil && status == syscall.WAIT_OBJECT_0
}
