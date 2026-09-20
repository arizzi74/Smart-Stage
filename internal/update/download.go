package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// download verifies both the exact release filename and its SHA-256 before any
// archive extraction or native helper preparation happens.
func (m *Manager) download(ctx context.Context, release candidate) (string, func(), error) {
	checksumResp, err := m.fetch(ctx, release.checksum)
	if err != nil {
		return "", nil, err
	}
	checksum, readErr := readLimited(checksumResp.Body, maxChecksum)
	checksumResp.Body.Close()
	if readErr != nil {
		return "", nil, fmt.Errorf("cannot read release checksum: %w", readErr)
	}
	if int64(len(checksum)) != release.checksum.Size {
		return "", nil, fmt.Errorf("release checksum size does not match published metadata")
	}
	expected, err := parseChecksum(checksum, release.archive.Name)
	if err != nil {
		return "", nil, err
	}
	resp, err := m.fetch(ctx, release.archive)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	dir, err := os.MkdirTemp("", "smartstage-update-download-")
	if err != nil {
		return "", nil, fmt.Errorf("cannot prepare update download: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, release.archive.Name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, release.archive.Size+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != release.archive.Size {
		cleanup()
		if copyErr != nil {
			return "", nil, fmt.Errorf("update download failed: %w", copyErr)
		}
		if closeErr != nil {
			return "", nil, fmt.Errorf("cannot save update download: %w", closeErr)
		}
		return "", nil, fmt.Errorf("update download size does not match published metadata")
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		cleanup()
		return "", nil, fmt.Errorf("update checksum verification failed; the installed app was not changed")
	}
	return path, cleanup, nil
}

func (m *Manager) fetch(ctx context.Context, asset releaseAsset) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Smart-Stage/"+m.options.CurrentVersion)
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s: %w", asset.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download of %s returned HTTP %d", asset.Name, resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != asset.Size {
		resp.Body.Close()
		return nil, fmt.Errorf("download size for %s does not match published metadata", asset.Name)
	}
	return resp, nil
}

func parseChecksum(data []byte, name string) (string, error) {
	fields := strings.Fields(string(data))
	if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("invalid release checksum file")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", fmt.Errorf("invalid release checksum digest")
	}
	return strings.ToLower(fields[0]), nil
}
