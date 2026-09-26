package platform

// DesktopFileRequest comes from an explicit native Finder/Dock or file-picker
// action. Paths refer to the user's original files; importing does not copy or
// play them. Acknowledge each request with DesktopFileResult after processing.
type DesktopFileRequest struct {
	ID    uint64   `json:"id"`
	Paths []string `json:"paths"`
}

const MaxDesktopFiles = 500
const MaxDesktopFileRequests = 8

// DesktopPlaylistResult is returned only after an explicit native Save/Open
// dialog. The host reads or writes this path; web content cannot provide it.
type DesktopPlaylistResult struct {
	ID        uint64 `json:"id"`
	Path      string `json:"path"`
	Cancelled bool   `json:"cancelled"`
	Error     string `json:"error"`
}
