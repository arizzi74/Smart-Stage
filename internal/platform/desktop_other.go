//go:build !darwin || !cgo

package platform

func DesktopAdmin(string)                     {}
func DesktopError(string)                     {}
func DesktopFiles() <-chan DesktopFileRequest { return nil }
func DesktopFilesPending() bool               { return false }
func DesktopFileResult(uint64, string)        {}
func pollDesktopFiles()                       {}
