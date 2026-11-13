package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"smartstage/internal/web"
)

func TestPlaylistSorterAssetsStayOnAdmin(t *testing.T) {
	adminAPI, authentication, _, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "sorter-assets")
	commandAPI := NewCommand(adminAPI.app, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	for _, asset := range []struct{ path, contentType string }{
		{"/assets/playlist-sorter.js", "text/javascript"},
		{"/assets/playlist-sorter.css", "text/css"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := request(adminAPI, method, asset.path, "", admin, "")
			if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), asset.contentType) {
				t.Fatalf("Admin module delivery: %s %s returned %d, %q", method, asset.path, w.Code, w.Header().Get("Content-Type"))
			}
			if method == http.MethodGet && w.Body.Len() == 0 || method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatalf("Invalid %s asset response body", method)
			}
			if w := request(commandAPI, method, asset.path, "", remote, ""); w.Code != http.StatusNotFound {
				t.Fatalf("Remote unexpectedly received Admin sorter asset: %s %s returned %d", method, asset.path, w.Code)
			}
		}
	}
}
