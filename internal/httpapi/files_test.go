package httpapi

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/web"
)

func TestFileBrowserHiddenToggleIsExplicitAndAdminOnly(t *testing.T) {
	api, authentication, service, media := setupAPI(t)
	root := filepath.Dir(media)
	if err := os.WriteFile(filepath.Join(root, ".hidden.wav"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".hidden-folder"), 0700); err != nil {
		t.Fatal(err)
	}
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	base := "/api/files?path=" + url.QueryEscape(root)
	for _, tc := range []struct {
		query string
		count int
	}{{"", 1}, {"&showHidden=false", 1}, {"&showHidden=true", 3}} {
		route := base + tc.query
		response := request(api, "GET", route, "", admin, "")
		var listing files.Listing
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &listing) != nil || len(listing.Entries) != tc.count {
			t.Fatalf("hidden toggle %q: HTTP %d %s", tc.query, response.Code, response.Body.String())
		}
		if denied := request(api, "GET", route, "", auth.Session{}, ""); denied.Code != 401 {
			t.Fatalf("unauthenticated file listing: HTTP %d", denied.Code)
		}
		if denied := request(command, "GET", route, "", remote, ""); denied.Code != 403 {
			t.Fatalf("remote file listing: HTTP %d", denied.Code)
		}
		if denied := request(api, "GET", route, "", admin, "http://attacker.example"); denied.Code != 403 {
			t.Fatalf("cross-origin file listing: HTTP %d", denied.Code)
		}
	}
}

func TestFileBrowserRejectsAmbiguousOrMalformedQueries(t *testing.T) {
	api, authentication, _, media := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	path := url.QueryEscape(filepath.Dir(media))
	for _, query := range []string{
		"showHidden=1", "showHidden=yes", "showHidden=TRUE", "showHidden=", "showHidden",
		"showHidden=true&showHidden=false", "path=" + path + "&path=" + path,
		"unknown=value", "path=" + path + "&showHidden=true&extra=x",
		"path=%ZZ", "showHidden=true;path=x",
	} {
		response := request(api, "GET", "/api/files?"+query, "", admin, "")
		if response.Code != 400 {
			t.Errorf("accepted file query %q: HTTP %d %s", query, response.Code, response.Body.String())
		}
	}
}
