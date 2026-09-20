//go:build !darwin || !cgo

package platform

func DesktopAdmin(string) {}
func DesktopError(string) {}
