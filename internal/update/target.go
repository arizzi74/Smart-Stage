package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Target describes the installation replaced by an update. A Finder installation
// is replaced as a complete bundle, including its launcher, icon and signature.
type Target struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

func DetectTarget(executable string) (Target, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return Target{}, errors.New("automatic installation is available on macOS and Windows")
	}
	path, err := filepath.Abs(executable)
	if err != nil {
		return Target{}, err
	}
	path = canonicalSystemPath(path)
	if err := noSymlinks(path); err != nil {
		return Target{}, err
	}
	t := Target{Path: path, Kind: "binary", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if runtime.GOOS == "darwin" {
		if strings.Contains(path, "/AppTranslocation/") {
			return Target{}, errors.New("move Smart Stage to Applications before updating; this copy is running from macOS App Translocation")
		}
		if filepath.Base(path) == "smartstage" && filepath.Base(filepath.Dir(path)) == "MacOS" && filepath.Base(filepath.Dir(filepath.Dir(path))) == "Contents" {
			bundle := filepath.Dir(filepath.Dir(filepath.Dir(path)))
			if strings.HasSuffix(bundle, ".app") {
				t.Path, t.Kind = bundle, "bundle"
			}
		}
		if os.Getenv("SMARTSTAGE_APP_LAUNCH") == "1" && t.Kind != "bundle" {
			return Target{}, errors.New("the running Finder app does not have the expected bundle layout; reinstall it before updating")
		}
	}
	if err := checkTarget(t); err != nil {
		return Target{}, err
	}
	return t, nil
}

// macOS exposes these ordinary system locations through fixed aliases. Resolve
// only their known destinations; user-created installation symlinks remain an
// error rather than changing the executable that an update would replace.
func canonicalSystemPath(path string) string {
	if runtime.GOOS != "darwin" {
		return path
	}
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if path == alias || strings.HasPrefix(path, alias+"/") {
			if resolved, err := filepath.EvalSymlinks(alias); err == nil && resolved == "/private"+alias {
				return resolved + strings.TrimPrefix(path, alias)
			}
		}
	}
	return path
}

func corePath(t Target) string {
	if t.Kind == "bundle" {
		return filepath.Join(t.Path, "Contents", "MacOS", "smartstage")
	}
	return t.Path
}

func checkTarget(t Target) error {
	if !filepath.IsAbs(t.Path) || filepath.Clean(t.Path) != t.Path || (t.Kind != "binary" && t.Kind != "bundle") {
		return errors.New("invalid update installation path")
	}
	if t.GOOS != runtime.GOOS || t.GOARCH != runtime.GOARCH {
		return errors.New("the update target does not match this computer")
	}
	if t.Kind == "bundle" && (t.GOOS != "darwin" || !strings.HasSuffix(t.Path, ".app")) {
		return errors.New("invalid application bundle target")
	}
	if strings.Contains(t.Path, "/AppTranslocation/") {
		return errors.New("a translocated Mac app cannot update itself; move it to Applications first")
	}
	if err := noSymlinks(corePath(t)); err != nil {
		return err
	}
	info, err := os.Stat(t.Path)
	if err != nil {
		return err
	}
	if (t.Kind == "bundle") != info.IsDir() {
		return errors.New("the installed update target has an unexpected file type")
	}
	if t.Kind == "binary" && !info.Mode().IsRegular() {
		return errors.New("the installed executable is not a regular file")
	}
	if t.Kind == "bundle" {
		if err := checkBundleIdentity(t.Path, ""); err != nil {
			return err
		}
	}
	return nil
}

// Checking every existing component prevents an installation through a symlink
// from silently replacing an unexpected destination. It also covers junctions
// represented as symlinks by Go on Windows.
func noSymlinks(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot update through a symbolic link: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
