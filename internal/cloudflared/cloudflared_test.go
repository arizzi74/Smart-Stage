package cloudflared

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativePlatformSelection(t *testing.T) {
	for _, platform := range []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/386"} {
		osName, arch, _ := strings.Cut(platform, "/")
		if ok, reason := platformSupport(osName, arch); !ok || reason != "" {
			t.Fatalf("supported %s: %v %q", platform, ok, reason)
		}
		a := nativeAssets[platform]
		if digest, err := hex.DecodeString(a.sha256); err != nil || len(digest) != 32 || a.size <= 0 || a.size > maxAssetSize {
			t.Fatalf("invalid pinned asset for %s", platform)
		}
	}
	for _, platform := range []string{"windows/arm64", "freebsd/amd64", "linux/riscv64", "darwin/386"} {
		osName, arch, _ := strings.Cut(platform, "/")
		if ok, reason := platformSupport(osName, arch); ok || reason == "" {
			t.Fatalf("unsupported platform %s was allowed", platform)
		}
	}
	_, reason := platformSupport("windows", "arm64")
	if !strings.Contains(reason, "Windows ARM64") || !strings.Contains(reason, "native") {
		t.Fatalf("missing Windows ARM64 explanation: %q", reason)
	}
}

func TestOriginAndGeneratedURLBounds(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:1234", "http://[::1]:1234/", "http://127.0.0.2:65535"} {
		if !validOrigin(origin) {
			t.Errorf("rejected loopback origin %q", origin)
		}
	}
	for _, origin := range []string{"http://localhost:8080", "https://127.0.0.1:1234", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://0.0.0.0:8080", "http://192.168.1.2:8080", "http://u:p@127.0.0.1:8080", "http://127.0.0.1:8080/admin", "http://127.0.0.1:8080/?secret=yes", "http://127.0.0.1:8080/#x"} {
		if validOrigin(origin) {
			t.Errorf("accepted unsafe origin %q", origin)
		}
	}
	const good = "https://one-two-three.trycloudflare.com"
	if got := exactQuickURL(good); got != good {
		t.Fatal(got)
	}
	for _, candidate := range []string{"http://one.trycloudflare.com", "https://trycloudflare.com", "https://one.trycloudflare.com.evil.test", "https://one.trycloudflare.com:443", "https://one.trycloudflare.com/path", "https://one.trycloudflare.com?", "https://one.trycloudflare.com#x", "https://evil@one.trycloudflare.com", "https://nested.one.trycloudflare.com", "https://-bad.trycloudflare.com", "https://one.trycloudflare.com/"} {
		if got := exactQuickURL(candidate); got != "" {
			t.Errorf("accepted %q as %q", candidate, got)
		}
	}
}

func TestIsolatedEnvironmentAndArguments(t *testing.T) {
	env := isolatedEnvironment([]string{"PATH=/bin", "TUNNEL_TOKEN=secret", "TUNNEL_CONFIG=foreign", "CLOUDFLARED_CONFIG=foreign", "CF_API_TOKEN=secret", "NO_AUTOUPDATE=false", "HOME=foreign", "USERPROFILE=foreign", "XDG_CONFIG_HOME=foreign", "HTTPS_PROXY=http://proxy", "SystemRoot=C:\\Windows", "LC_ALL=C"}, "/private/run")
	joined := strings.Join(env, "\n")
	for _, forbidden := range []string{"secret", "foreign", "NO_AUTOUPDATE=false"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("ambient helper configuration retained: %s", forbidden)
		}
	}
	for _, required := range []string{"HOME=/private/run", "XDG_CONFIG_HOME=/private/run", "NO_AUTOUPDATE=true", "SystemRoot=C:\\Windows", "HTTPS_PROXY=http://proxy"} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing runtime setting %q", required)
		}
	}
	args := strings.Join(helperArgs("http://127.0.0.1:1234", "/private/config.yml"), " ")
	for _, required := range []string{"--no-autoupdate", "--config /private/config.yml", "--metrics 127.0.0.1:0", "--protocol auto", "--grace-period 1s", "--output json"} {
		if !strings.Contains(args, required) {
			t.Errorf("missing helper isolation argument %q", required)
		}
	}
	if strings.Contains(args, "--http-host-header") || strings.Contains(args, "--token") || strings.Contains(args, "--logformat") {
		t.Fatal("helper rewrites Host or uses a token")
	}
}

// Opt-in compatibility check against the pinned native release. CI can download
// and prepare the actual release asset (including Darwin archive extraction),
// or a local uncompressed native asset can be supplied for an offline check.
// --help parses the full production argv without provisioning a tunnel.
func TestPinnedBinaryAcceptsProductionArguments(t *testing.T) {
	binary := os.Getenv("SMARTSTAGE_TEST_CLOUDFLARED_BINARY")
	download := os.Getenv("SMARTSTAGE_TEST_CLOUDFLARED_DOWNLOAD") == "1"
	if binary == "" && !download {
		t.Skip("set SMARTSTAGE_TEST_CLOUDFLARED_DOWNLOAD=1 or SMARTSTAGE_TEST_CLOUDFLARED_BINARY to check the pinned native CLI")
	}
	a, ok := nativeAssets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		_, reason := Support()
		t.Skip(reason)
	}
	dir := t.TempDir()
	if binary == "" {
		prepareCtx, prepareCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer prepareCancel()
		prepared, runDir, err := prepareBinary(prepareCtx, dir, a, downloadClient(), nil)
		if err != nil {
			t.Fatal("cannot prepare the pinned official native test helper:", err)
		}
		defer os.RemoveAll(runDir)
		binary = prepared
	} else {
		if a.tgz {
			t.Skip("set SMARTSTAGE_TEST_CLOUDFLARED_DOWNLOAD=1 without SMARTSTAGE_TEST_CLOUDFLARED_BINARY to verify and extract the pinned Darwin archive")
		}
		var err error
		binary, err = filepath.Abs(binary)
		if err != nil {
			t.Fatal("cannot locate the pinned native test helper")
		}
		f, err := os.Open(binary)
		if err != nil {
			t.Fatal("cannot open the pinned native test helper")
		}
		err = copyVerified(context.Background(), io.Discard, f, a)
		_ = f.Close()
		if err != nil {
			t.Fatal("test helper does not match the pinned official native asset:", err)
		}
	}
	config := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(config, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := append(helperArgs("http://127.0.0.1:1", config), "--help")
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = isolatedEnvironment(os.Environ(), dir)
	output, err := cmd.CombinedOutput()
	if err != nil || bytes.Contains(output, []byte("Incorrect Usage")) || !bytes.Contains(output, []byte("NAME:")) {
		t.Fatal("pinned native helper rejected the production arguments")
	}
	t.Logf("verified official %s release %s accepts production arguments", a.name, Version)
}

func TestEventWriterBoundsAndSplitLogs(t *testing.T) {
	events := make(chan processEvent, 4)
	w := &eventWriter{events: events}
	_, _ = w.Write([]byte(strings.Repeat("x", 70<<10) + "https://bad.trycloudflare.com\n"))
	_, _ = w.Write([]byte("https://spoof.trycloudflare.com.evil.invalid\n"))
	_, _ = w.Write([]byte(`{"message":"| https://valid-`))
	_, _ = w.Write([]byte("name.trycloudflare.com |\"}\n"))
	_, _ = w.Write([]byte("Registered tunnel connection\nRegistered tunnel connection\n"))
	_, _ = w.Write([]byte("https://another.trycloudflare.com\n"))
	if len(events) != 2 {
		t.Fatalf("got %d events; want URL and registration only", len(events))
	}
	if e := <-events; e.url != "https://valid-name.trycloudflare.com" {
		t.Fatalf("invalid URL event: %+v", e)
	}
	if e := <-events; !e.registered {
		t.Fatalf("invalid registration event: %+v", e)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func fixtureAsset(data []byte) asset {
	digest := sha256.Sum256(data)
	return asset{name: "fixture-binary", sha256: hex.EncodeToString(digest[:]), size: int64(len(data))}
}

func dataClient(data []byte, calls *int) *http.Client {
	client := downloadClient()
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		(*calls)++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: -1, Request: req}, nil
	})
	return client
}

func TestDownloadChecksumSizeAndAtomicity(t *testing.T) {
	good := []byte("verified native binary fixture")
	a := fixtureAsset(good)
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"valid", good, nil},
		{"checksum", bytes.Repeat([]byte{'x'}, len(good)), errChecksum},
		{"oversized", append(append([]byte(nil), good...), 'x'), errSize},
		{"truncated", good[:len(good)-1], errSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, a.name)
			if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			err := downloadAsset(context.Background(), dataClient(tc.data, &calls), releaseBase+a.name, target, a)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			got, _ := os.ReadFile(target)
			want := good
			if tc.want != nil {
				want = []byte("old")
			}
			if !bytes.Equal(got, want) {
				t.Fatal("invalid download replaced cache or valid download was not published")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 || calls != 1 {
				t.Fatalf("temporary download leaked or unexpected requests: files=%d calls=%d", len(entries), calls)
			}
		})
	}
}

func TestDownloadRedirectRestrictions(t *testing.T) {
	for _, raw := range []string{"http://github.com/a", "https://github.com.evil.test/a", "https://evil@github.com/a", "https://github.com:444/a", "https://example.com/a"} {
		u, _ := url.Parse(raw)
		if allowedDownloadURL(u) {
			t.Errorf("unsafe download URL accepted %q", raw)
		}
	}
	for _, host := range []string{"github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com"} {
		u, _ := url.Parse("https://" + host + "/asset")
		if !allowedDownloadURL(u) {
			t.Errorf("official host rejected %s", host)
		}
	}
	for _, redirect := range []string{"https://evil.test/steal", "http://github.com/insecure", "https://github.com/redirect-loop"} {
		t.Run(redirect, func(t *testing.T) {
			calls := 0
			client := downloadClient()
			client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{redirect}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			})
			err := downloadAsset(context.Background(), client, releaseBase+"fixture", filepath.Join(t.TempDir(), "fixture"), fixtureAsset([]byte("x")))
			if err == nil || calls > 4 {
				t.Fatalf("unbounded or accepted redirect: calls=%d err=%v", calls, err)
			}
			if strings.Contains(redirect, "evil") && calls != 1 {
				t.Fatal("followed an untrusted redirect")
			}
		})
	}
}

func TestCacheVerifiedEveryRunAndRuntimeIsolation(t *testing.T) {
	data := []byte("verified native binary fixture")
	a := fixtureAsset(data)
	calls := 0
	client := dataClient(data, &calls)
	config := t.TempDir()
	var previousRun string
	for run := 0; run < 3; run++ {
		var statuses []string
		binary, runDir, err := prepareBinary(context.Background(), config, a, client, func(s string) { statuses = append(statuses, s) })
		if err != nil {
			t.Fatal(err)
		}
		if runDir == previousRun || filepath.Dir(binary) != runDir {
			t.Fatal("runtime binary was not isolated")
		}
		previousRun = runDir
		got, _ := os.ReadFile(binary)
		if !bytes.Equal(got, data) {
			t.Fatal("runtime binary was not verified")
		}
		if run == 1 && len(statuses) != 0 {
			t.Fatal("valid cached asset was downloaded again")
		}
		if err := os.RemoveAll(runDir); err != nil {
			t.Fatal(err)
		}
		if run == 1 {
			// Preserve the size to ensure SHA verification detects corruption.
			if err := os.WriteFile(filepath.Join(config, "cloudflared", Version, a.name), bytes.Repeat([]byte{'x'}, len(data)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls != 2 {
		t.Fatalf("cache was not reused then repaired: downloads=%d", calls)
	}
}

func TestFailedPreparationCleansRuntimeAndSanitizesErrors(t *testing.T) {
	config := t.TempDir()
	a := fixtureAsset([]byte("good"))
	client := downloadClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("secret-token-or-path")
	})
	_, _, err := prepareBinary(context.Background(), config, a, client, nil)
	if !errors.Is(err, ErrDownload) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsanitized failure: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(config, "cloudflared"))
	if len(entries) != 1 || entries[0].Name() != Version {
		t.Fatal("failed runtime directory leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentCachePreparation(t *testing.T) {
	data := []byte("verified native binary fixture")
	a := fixtureAsset(data)
	config := t.TempDir()
	client := downloadClient()
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), ContentLength: -1, Request: req}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, runDir, err := prepareBinary(context.Background(), config, a, client, nil)
			if err != nil {
				t.Errorf("concurrent prepare: %v", err)
				return
			}
			_ = os.RemoveAll(runDir)
		}()
	}
	wg.Wait()
	entries, _ := os.ReadDir(filepath.Join(config, "cloudflared", Version))
	if len(entries) != 1 {
		t.Fatal("concurrent download temporary files leaked")
	}
}

func TestPreparationWaiterCancellation(t *testing.T) {
	preparationSlot <- struct{}{}
	defer func() { <-preparationSlot }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := prepareBinary(ctx, t.TempDir(), fixtureAsset([]byte("x")), downloadClient(), nil)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled cache waiter did not return")
	}
}

func tarFixture(t *testing.T, headers []*tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, hdr := range headers {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			if _, err := tw.Write(bytes.Repeat([]byte{'b'}, int(hdr.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestSafeArchiveExtraction(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []*tar.Header
		valid   bool
	}{
		{"single-binary", []*tar.Header{{Name: "cloudflared", Typeflag: tar.TypeReg, Size: 12, Mode: 0755}}, true},
		{"traversal", []*tar.Header{{Name: "../cloudflared", Typeflag: tar.TypeReg, Size: 1}}, false},
		{"absolute", []*tar.Header{{Name: "/cloudflared", Typeflag: tar.TypeReg, Size: 1}}, false},
		{"symlink", []*tar.Header{{Name: "cloudflared", Typeflag: tar.TypeSymlink, Linkname: "/bin/sh"}}, false},
		{"hardlink", []*tar.Header{{Name: "cloudflared", Typeflag: tar.TypeLink, Linkname: "/bin/sh"}}, false},
		{"directory", []*tar.Header{{Name: "cloudflared/", Typeflag: tar.TypeDir}}, false},
		{"duplicate", []*tar.Header{{Name: "cloudflared", Typeflag: tar.TypeReg, Size: 1}, {Name: "cloudflared", Typeflag: tar.TypeReg, Size: 1}}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source, target := filepath.Join(dir, "asset.tgz"), filepath.Join(dir, "binary")
			if err := os.WriteFile(source, tarFixture(t, tc.headers), 0600); err != nil {
				t.Fatal(err)
			}
			err := extractBinary(context.Background(), source, target)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid {
				data, _ := os.ReadFile(target)
				if len(data) != 12 {
					t.Fatal("extracted binary truncated")
				}
			}
		})
	}
	// A tar file can finish before the gzip footer; corrupted CRC must fail.
	data := tarFixture(t, []*tar.Header{{Name: "cloudflared", Typeflag: tar.TypeReg, Size: 12}})
	data[len(data)-8] ^= 1
	dir := t.TempDir()
	source := filepath.Join(dir, "corrupt.tgz")
	_ = os.WriteFile(source, data, 0600)
	if err := extractBinary(context.Background(), source, filepath.Join(dir, "binary")); err == nil {
		t.Fatal("accepted corrupt gzip checksum")
	}
}
