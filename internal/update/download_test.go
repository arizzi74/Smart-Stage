package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

func managerDownloadFixture(payload string) (candidate, managerRoundTripper) {
	name := "smartstage-darwin-arm64.app.zip"
	version := "v0.1.0-preview.10"
	base := repositoryURL + "/releases/download/" + version + "/"
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(payload)), name)
	c := candidate{version: version,
		archive:  releaseAsset{Name: name, URL: base + name, Size: int64(len(payload))},
		checksum: releaseAsset{Name: name + ".sha256", URL: base + name + ".sha256", Size: int64(len(checksum))}}
	return c, managerRoundTripper(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			return managerResponse(checksum), nil
		}
		return managerResponse(payload), nil
	})
}

func TestDownloadRequiresExactChecksumSizeAndBytes(t *testing.T) {
	for _, mode := range []string{"valid", "digest mismatch", "truncated", "oversize", "wrong checksum filename"} {
		t.Run(mode, func(t *testing.T) {
			c, base := managerDownloadFixture("verified archive bytes")
			transport := managerRoundTripper(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, ".sha256") && mode == "wrong checksum filename" {
					return managerResponse(fmt.Sprintf("%x  other.zip\n", sha256.Sum256([]byte("verified archive bytes")))), nil
				}
				if !strings.HasSuffix(r.URL.Path, ".sha256") {
					switch mode {
					case "digest mismatch":
						return managerResponse("modified archive bytes"), nil
					case "truncated":
						r := managerResponse("short")
						r.ContentLength = -1
						return r, nil
					case "oversize":
						r := managerResponse("verified archive bytesEXTRA")
						r.ContentLength = -1
						return r, nil
					}
				}
				return base(r)
			})
			m := &Manager{client: &http.Client{Transport: transport}}
			path, cleanup, err := m.download(context.Background(), c)
			if mode != "valid" {
				if err == nil || path != "" || cleanup != nil {
					t.Fatalf("invalid download escaped verification: path=%q error=%v", path, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "verified archive bytes" {
				t.Fatalf("downloaded %q: %v", data, err)
			}
			cleanup()
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("temporary download survived cleanup: %v", err)
			}
		})
	}
}

func TestChecksumRequiresOneExactAsset(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, data := range []string{digest, digest + "  wrong.zip", digest + "  app.zip\n" + digest + "  app.zip", strings.Repeat("z", 64) + "  app.zip"} {
		if _, err := parseChecksum([]byte(data), "app.zip"); err == nil {
			t.Fatalf("accepted checksum %q", data)
		}
	}
	if _, err := parseChecksum([]byte(strings.ToUpper(digest)+" *app.zip\n"), "app.zip"); err != nil {
		t.Fatal(err)
	}
}
