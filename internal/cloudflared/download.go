package cloudflared

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxAssetSize  = 96 << 20
	maxBinarySize = 128 << 20
)

var (
	errChecksum = errors.New("cloudflared checksum mismatch")
	errSize     = errors.New("cloudflared asset size mismatch")
	errArchive  = errors.New("invalid cloudflared archive")
	// Only preparation is serialized. A canceled waiter returns immediately,
	// and a running tunnel never holds this lock.
	preparationSlot = make(chan struct{}, 1)
)

func downloadClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 || !allowedDownloadURL(req.URL) {
				return errors.New("cloudflared download redirect rejected")
			}
			return nil
		},
	}
}

func allowedDownloadURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

func privateDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cloudflared cache is not a directory")
	}
	return os.Chmod(path, 0700)
}

// prepareBinary copies every verified cache hit to a private per-run snapshot.
// The copy itself is hashed, so a corrupted cache or changed pathname cannot
// bypass verification. No persistent extracted binary is ever trusted.
func prepareBinary(ctx context.Context, configDir string, a asset, client *http.Client, onStatus func(string)) (string, string, error) {
	select {
	case preparationSlot <- struct{}{}:
		defer func() { <-preparationSlot }()
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
	if configDir == "" {
		var err error
		configDir, err = os.UserCacheDir()
		if err != nil {
			return "", "", ErrCache
		}
	}
	var err error
	configDir, err = filepath.Abs(configDir)
	if err != nil {
		return "", "", ErrCache
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", "", ErrCache
	}
	base := filepath.Join(configDir, "cloudflared")
	if err := privateDirectory(base); err != nil {
		return "", "", ErrCache
	}
	cacheDir := filepath.Join(base, Version)
	if err := privateDirectory(cacheDir); err != nil {
		return "", "", ErrCache
	}
	runDir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return "", "", ErrCache
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(runDir)
		}
	}()
	if err := os.WriteFile(filepath.Join(runDir, runtimeMarker), []byte(runtimeMarkerValue), 0600); err != nil {
		return "", "", ErrCache
	}
	cachePath := filepath.Join(cacheDir, a.name)
	snapshot := filepath.Join(runDir, "verified-asset")
	if err := snapshotAsset(ctx, cachePath, snapshot, a); err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		_ = os.Remove(snapshot)
		if onStatus != nil {
			onStatus("downloading")
		}
		if err := downloadAsset(ctx, client, releaseBase+a.name, cachePath, a); err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "", "", ErrDownload
		}
		if err := snapshotAsset(ctx, cachePath, snapshot, a); err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "", "", ErrDownload
		}
	}
	binaryName := "cloudflared"
	if strings.HasSuffix(a.name, ".exe") {
		binaryName += ".exe"
	}
	binary := filepath.Join(runDir, binaryName)
	if a.tgz {
		if err := extractBinary(ctx, snapshot, binary); err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "", "", ErrDownload
		}
		_ = os.Remove(snapshot)
	} else if err := os.Rename(snapshot, binary); err != nil {
		return "", "", ErrCache
	}
	if err := os.Chmod(binary, 0700); err != nil {
		return "", "", ErrCache
	}
	ok = true
	return binary, runDir, nil
}

func downloadAsset(ctx context.Context, client *http.Client, source, target string, a asset) error {
	parsed, err := url.Parse(source)
	if err != nil || !allowedDownloadURL(parsed) {
		return errors.New("cloudflared download URL rejected")
	}
	if a.size <= 0 || a.size > maxAssetSize {
		return errSize
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "SmartStage-cloudflared/"+Version)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("cloudflared download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || (resp.ContentLength >= 0 && resp.ContentLength != a.size) {
		return errSize
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".download-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := copyVerified(ctx, f, resp.Body, a); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), target); err != nil {
		// Windows cannot replace an existing cache entry. No running process
		// executes this file; every process uses its own verified snapshot.
		if existing, openErr := os.Open(target); openErr == nil {
			verifyErr := copyVerified(ctx, io.Discard, existing, a)
			_ = existing.Close()
			if verifyErr == nil {
				return nil
			}
		}
		if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		return os.Rename(f.Name(), target)
	}
	return nil
}

func snapshotAsset(ctx context.Context, source, target string, a asset) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != a.size || a.size <= 0 || a.size > maxAssetSize {
		return errSize
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := copyVerified(ctx, out, in, a); err != nil {
		return err
	}
	return out.Close()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func copyVerified(ctx context.Context, out io.Writer, in io.Reader, a asset) error {
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(contextReader{ctx, in}, a.size+1))
	if err != nil {
		return err
	}
	if n != a.size {
		return errSize
	}
	if hex.EncodeToString(hash.Sum(nil)) != a.sha256 {
		return errChecksum
	}
	return nil
}

func extractBinary(ctx context.Context, source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(contextReader{ctx, in})
	if err != nil {
		return errArchive
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, maxBinarySize+(1<<20)))
	found := false
	for entries := 0; ; entries++ {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil || entries >= 16 {
			return errArchive
		}
		// Official archives contain exactly one root-level regular binary.
		// Do not extract filenames, links, directory trees, or archive metadata.
		if hdr.Name != "cloudflared" || (hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA) || found || hdr.Size <= 0 || hdr.Size > maxBinarySize {
			return errArchive
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(out, contextReader{ctx, tr})
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil || n != hdr.Size {
			return errArchive
		}
		found = true
	}
	// Drain the bounded gzip stream so CRC/truncation errors are checked too.
	if n, err := io.Copy(io.Discard, io.LimitReader(gz, (1<<20)+1)); err != nil || n > 1<<20 {
		return errArchive
	}
	if !found {
		return errArchive
	}
	return nil
}
