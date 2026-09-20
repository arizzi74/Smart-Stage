//go:build !darwin || !cgo

package platform

func DesktopAdmin(string)                     {}
func DesktopError(string)                     {}
func DesktopFiles() <-chan DesktopFileRequest { return nil }
func DesktopFilesPending() bool               { return false }
func DesktopFileResult(uint64, string)        {}
func DesktopAdminRequests() <-chan struct{}   { return nil }
func DesktopCanChooseFiles() bool             { return false }
func DesktopChooseFiles() bool                { return false }
func DesktopActivateBrowser() bool            { return false }
func DesktopHasAdminWindow() bool             { return false }
func DesktopShowAdmin() bool                  { return false }
func pollDesktop()                            {}
