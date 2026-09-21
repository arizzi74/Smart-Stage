package update

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type managerRoundTripper func(*http.Request) (*http.Response, error)

func (f managerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func managerResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Header: make(http.Header)}
}

func managerRelease(tag, name string) githubRelease {
	base := repositoryURL + "/releases/download/" + tag + "/"
	return githubRelease{Tag: tag, Assets: []releaseAsset{
		{Name: name, URL: base + name, Size: 4},
		{Name: name + ".sha256", URL: base + name + ".sha256", Size: 100},
	}}
}

func TestDiscoverSelectsChannelVersionAndCompleteExactAssets(t *testing.T) {
	name := "smartstage-darwin-arm64.app.zip"
	preview9 := managerRelease("v0.1.0-preview.9", name)
	preview10 := managerRelease("v0.1.0-preview.10", name)
	preview10.Prerelease = true
	incomplete := managerRelease("v9.0.0", name)
	incomplete.Assets = incomplete.Assets[:1]
	untrusted := managerRelease("v8.0.0", name)
	untrusted.Assets[0].URL = "https://example.com/" + name
	draft := managerRelease("v7.0.0", name)
	draft.Draft = true
	wrongArchitecture := managerRelease("v6.0.0", "smartstage-darwin-amd64.app.zip")
	duplicate := managerRelease("v5.0.0", name)
	duplicate.Assets = append(duplicate.Assets, duplicate.Assets[0])
	oversize := managerRelease("v4.0.0", name)
	oversize.Assets[0].Size = maxArchive + 1
	stable := managerRelease("v0.0.9", name)
	for _, tc := range []struct{ current, want string }{
		{"v0.1.0-preview.8", "v0.1.0-preview.10"},
		{"v0.0.8", "v0.0.9"},
		{"v1.0.0", ""},
	} {
		t.Run(tc.current, func(t *testing.T) {
			data, _ := json.Marshal([]githubRelease{preview9, incomplete, untrusted, preview10, draft, wrongArchitecture, duplicate, oversize, stable})
			m := &Manager{options: Options{CurrentVersion: tc.current}, target: Target{Kind: "bundle", GOOS: "darwin", GOARCH: "arm64"}, client: &http.Client{Transport: managerRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.github.com" || r.Header.Get("X-GitHub-Api-Version") == "" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				return managerResponse(string(data)), nil
			})}}
			got, err := m.discover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && got != nil || tc.want != "" && (got == nil || got.version != tc.want) {
				t.Fatalf("candidate = %+v, want %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverPreviewToStableAndStableChannel(t *testing.T) {
	name := "smartstage-windows-arm64.zip"
	preview21 := managerRelease("v0.1.0-preview.21", name)
	preview21.Prerelease = true
	stable := managerRelease("v1.0.0", name)
	patch := managerRelease("v1.0.1", name)
	futurePreview := managerRelease("v1.1.0-preview.1", name)
	futurePreview.Prerelease = true
	// Stable installs must respect both the semantic version and GitHub's flag.
	mislabelledPreview := managerRelease("v2.0.0-preview.1", name)
	flaggedStable := managerRelease("v3.0.0", name)
	flaggedStable.Prerelease = true
	for _, tc := range []struct {
		name, current, want string
		releases            []githubRelease
	}{
		{"preview21 migrates to first stable", "v0.1.0-preview.21", "v1.0.0", []githubRelease{preview21, stable}},
		{"stable excludes all future previews", "v1.0.0", "", []githubRelease{futurePreview, mislabelledPreview, flaggedStable, stable, preview21}},
		{"stable finds newer stable among previews", "v1.0.0", "v1.0.1", []githubRelease{futurePreview, patch, mislabelledPreview, flaggedStable, stable}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.releases)
			if err != nil {
				t.Fatal(err)
			}
			m := &Manager{options: Options{CurrentVersion: tc.current}, target: Target{Kind: "binary", GOOS: "windows", GOARCH: "arm64"}, client: &http.Client{Transport: managerRoundTripper(func(*http.Request) (*http.Response, error) {
				return managerResponse(string(data)), nil
			})}}
			got, err := m.discover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && got != nil || tc.want != "" && (got == nil || got.version != tc.want) {
				t.Fatalf("candidate = %+v, want %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverPaginatesInsteadOfTrustingReleaseOrder(t *testing.T) {
	name := "smartstage-windows-amd64.zip"
	page1 := make([]githubRelease, 100)
	for i := range page1 {
		page1[i] = managerRelease("v0.1.0-preview.8", name)
	}
	calls := 0
	m := &Manager{options: Options{CurrentVersion: "v0.1.0-preview.8"}, target: Target{Kind: "binary", GOOS: "windows", GOARCH: "amd64"}, client: &http.Client{Transport: managerRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		var data []byte
		if r.URL.Query().Get("page") == "1" {
			data, _ = json.Marshal(page1)
		} else {
			data, _ = json.Marshal([]githubRelease{managerRelease("v0.1.0-preview.10", name)})
		}
		return managerResponse(string(data)), nil
	})}}
	got, err := m.discover(context.Background())
	if err != nil || got == nil || got.version != "v0.1.0-preview.10" || calls != 2 {
		t.Fatalf("candidate=%+v calls=%d error=%v", got, calls, err)
	}
}

func TestArchiveNames(t *testing.T) {
	for _, tc := range []struct {
		target Target
		want   string
	}{
		{Target{Kind: "bundle", GOOS: "darwin", GOARCH: "arm64"}, "smartstage-darwin-arm64.app.zip"},
		{Target{Kind: "binary", GOOS: "darwin", GOARCH: "amd64"}, "smartstage-darwin-amd64.zip"},
		{Target{Kind: "binary", GOOS: "windows", GOARCH: "arm64"}, "smartstage-windows-arm64.zip"},
	} {
		got, err := archiveName(tc.target)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc.target, got, err)
		}
	}
	if _, err := archiveName(Target{Kind: "binary", GOOS: "linux", GOARCH: "amd64"}); err == nil {
		t.Fatal("Linux automatic replacement must be unsupported")
	}
}

func TestRedirectsStayOnHTTPSGitHubReleaseHosting(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		allow bool
	}{
		{"https://release-assets.githubusercontent.com/github-production-release-asset/1", true},
		{"https://objects.githubusercontent.com/path?signature=value", true},
		{"http://release-assets.githubusercontent.com/path", false},
		{"https://evil.github.com/path", false},
		{"https://github.com.evil.test/path", false},
		{"https://127.0.0.1:8787/admin", false},
		{"https://user:password@github.com/path", false},
		{"https://github.com:444/path", false},
	} {
		u, _ := url.Parse(tc.raw)
		if got := githubRedirect(&http.Request{URL: u}, nil) == nil; got != tc.allow {
			t.Errorf("redirect %s allowed=%v", tc.raw, got)
		}
	}
	u, _ := url.Parse(repositoryURL)
	if githubRedirect(&http.Request{URL: u}, make([]*http.Request, 5)) == nil {
		t.Fatal("unbounded redirect chain accepted")
	}
}
