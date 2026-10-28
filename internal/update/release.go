package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	manifestURL   = "https://arizzi74.github.io/Smart-Stage/updates.json"
	repositoryURL = "https://github.com/arizzi74/Smart-Stage"
	maxMetadata   = 8 << 20
	maxArchive    = 512 << 20
	maxChecksum   = 4 << 10
)

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type githubRelease struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Releases      []githubRelease `json:"releases"`
}

type candidate struct {
	version  string
	archive  releaseAsset
	checksum releaseAsset
}

type releaseRateLimitError struct {
	until time.Time
}

func (e *releaseRateLimitError) Error() string {
	return "Update website temporarily limited update checks; retry after " + e.until.UTC().Format(time.RFC3339)
}

func releaseRetryTime(header http.Header, now time.Time) time.Time {
	until := now.Add(5 * time.Minute)
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 && seconds <= 86400 {
		until = now.Add(time.Duration(seconds) * time.Second)
	} else if date, err := http.ParseTime(header.Get("Retry-After")); err == nil && date.After(now) {
		until = date
	} else if header.Get("X-RateLimit-Remaining") == "0" {
		if unix, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && time.Unix(unix, 0).After(now) {
			until = time.Unix(unix, 0)
		}
	}
	if until.After(now.Add(24 * time.Hour)) {
		until = now.Add(24 * time.Hour)
	}
	return until
}

func archiveName(target Target) (string, error) {
	if target.GOOS != "darwin" && target.GOOS != "windows" || target.GOARCH != "arm64" && target.GOARCH != "amd64" {
		return "", fmt.Errorf("automatic updates are available for Mac and Windows release builds")
	}
	suffix := ".zip"
	if target.Kind == "bundle" && target.GOOS == "darwin" {
		suffix = ".app.zip"
	} else if target.Kind != "binary" {
		return "", fmt.Errorf("this application layout cannot be updated automatically")
	}
	return "smartstage-" + target.GOOS + "-" + target.GOARCH + suffix, nil
}

func (m *Manager) discover(ctx context.Context) (*candidate, error) {
	current, err := parseVersion(m.options.CurrentVersion)
	if err != nil {
		return nil, err
	}
	name, err := archiveName(m.target)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", "Smart-Stage/"+m.options.CurrentVersion)
	// Discovery never falls back to the GitHub API, including on feed errors.
	// Download redirects retain their separate GitHub-only policy.
	client := *m.client
	client.CheckRedirect = manifestRedirect
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot check update website: %w", err)
	}
	data, readErr := readLimited(resp.Body, maxMetadata)
	resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &releaseRateLimitError{until: releaseRetryTime(resp.Header, m.now())}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update website returned HTTP %d; try again later", resp.StatusCode)
	}
	if readErr != nil {
		return nil, fmt.Errorf("cannot read update manifest: %w", readErr)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid update manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Releases == nil || len(manifest.Releases) > 500 {
		return nil, fmt.Errorf("unsupported or incomplete update manifest")
	}
	var best *candidate
	bestVersion := current
	// A website manifest can contain both channels in any order. Keep semantic
	// selection and exact official archive/checksum validation unchanged.
	for _, release := range manifest.Releases {
		version, err := parseVersion(release.Tag)
		if err != nil || release.Draft || len(current.pre) == 0 && (release.Prerelease || len(version.pre) != 0) || version.compare(bestVersion) <= 0 {
			continue
		}
		archive, archiveOK := matchingAsset(release, name, maxArchive)
		checksum, checksumOK := matchingAsset(release, name+".sha256", maxChecksum)
		if !archiveOK || !checksumOK {
			continue // A partially uploaded release is not installable yet.
		}
		best = &candidate{version: release.Tag, archive: archive, checksum: checksum}
		bestVersion = version
	}
	return best, nil
}

func manifestRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.Host != "arizzi74.github.io" || req.URL.User != nil || req.URL.Path != "/Smart-Stage/updates.json" || req.URL.RawQuery != "" || req.URL.Fragment != "" {
		return fmt.Errorf("update manifest redirect is outside the official website")
	}
	return nil
}

func matchingAsset(release githubRelease, name string, limit int64) (releaseAsset, bool) {
	expected := repositoryURL + "/releases/download/" + url.PathEscape(release.Tag) + "/" + name
	var found releaseAsset
	count := 0
	for _, asset := range release.Assets {
		if asset.Name == name {
			count++
			found = asset
		}
	}
	return found, count == 1 && found.URL == expected && found.Size > 0 && found.Size <= limit
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds size limit")
	}
	return data, nil
}

func githubRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("too many download redirects")
	}
	u := req.URL
	if u.Scheme != "https" || u.User != nil || u.Port() != "" && u.Port() != "443" || u.Fragment != "" {
		return fmt.Errorf("update redirect must use HTTPS")
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("update redirect is outside GitHub release hosting")
	}
}
