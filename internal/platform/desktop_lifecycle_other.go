//go:build !windows || !cgo

package platform

func DesktopConfigure(string)              {}
func DesktopReopen(string) bool            { return false }
func DesktopQuitRequests() <-chan struct{} { return nil }
func pollDesktopQuit()                     {}
