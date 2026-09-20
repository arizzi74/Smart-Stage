// Package files provides read-only host browsing and canonical root containment.
package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MaxEntries = 1000

type Browser struct {
	roots []string
	home  string
}
type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified"`
}
type Crumb struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type Listing struct {
	Path        string   `json:"path"`
	Parent      string   `json:"parent"`
	Roots       []string `json:"roots"`
	Breadcrumbs []Crumb  `json:"breadcrumbs"`
	Entries     []Entry  `json:"entries"`
	Truncated   bool     `json:"truncated"`
}

func New(roots []string) (*Browser, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	b := &Browser{home: home}
	for _, root := range roots {
		p, err := canonical(root)
		if err != nil {
			return nil, fmt.Errorf("media root: %w", err)
		}
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("media root must be an accessible directory: %s", root)
		}
		b.roots = append(b.roots, p)
	}
	if len(b.roots) > 0 {
		if _, err := b.Resolve(home); err != nil {
			b.home = b.roots[0]
		}
	}
	return b, nil
}

func canonical(path string) (string, error) {
	if path == "" || len(path) > 32768 || strings.IndexByte(path, 0) >= 0 || strings.Contains(path, "://") {
		return "", errors.New("invalid host path")
	}
	if err := platformPath(path); err != nil {
		return "", err
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(p))
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func (b *Browser) allowed(path string) bool {
	if len(b.roots) == 0 {
		return true
	}
	for _, root := range b.roots {
		if within(root, path) {
			return true
		}
	}
	return false
}
func (b *Browser) Resolve(path string) (string, error) {
	p, err := canonical(path)
	if err != nil {
		return "", err
	}
	if !b.allowed(p) {
		return "", errors.New("path is outside the configured media roots")
	}
	return p, nil
}
func (b *Browser) File(path string) (string, os.FileInfo, error) {
	p, err := b.Resolve(path)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("media must be a regular file")
	}
	return p, info, nil
}
func (b *Browser) Browse(path string) (Listing, error) {
	return b.BrowseWithHidden(path, false)
}

// BrowseWithHidden controls presentation of dot-prefixed directory entries.
// Explicit paths still use the same canonical media-root checks regardless of
// this preference: hiding a name is not an access restriction.
func (b *Browser) BrowseWithHidden(path string, showHidden bool) (Listing, error) {
	if path == "" {
		path = b.home
	}
	p, err := b.Resolve(path)
	if err != nil {
		return Listing{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return Listing{}, err
	}
	defer f.Close()
	list := Listing{Path: p, Entries: []Entry{}, Roots: b.Roots()}
	// Read in bounded batches so hidden entries cannot consume the visible
	// result limit, even when they occur before the first visible media file.
listing:
	for {
		entries, readErr := f.ReadDir(128)
		if readErr != nil && readErr != io.EOF {
			return Listing{}, fmt.Errorf("cannot list directory: %w", readErr)
		}
		for _, entry := range entries {
			if !showHidden && strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			name := filepath.Join(p, entry.Name())
			resolved, e := b.Resolve(name)
			if e != nil {
				continue
			}
			info, e := os.Stat(resolved)
			if e != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
				continue
			}
			if len(list.Entries) == MaxEntries {
				list.Truncated = true
				break listing
			}
			list.Entries = append(list.Entries, Entry{Name: entry.Name(), Path: resolved, Directory: info.IsDir(), Size: info.Size(), Modified: info.ModTime().UnixNano()})
		}
		if readErr == io.EOF {
			break
		}
	}
	sort.Slice(list.Entries, func(i, j int) bool {
		a, c := list.Entries[i], list.Entries[j]
		if a.Directory != c.Directory {
			return a.Directory
		}
		return strings.ToLower(a.Name) < strings.ToLower(c.Name)
	})
	parent := filepath.Dir(p)
	if parent != p && b.allowed(parent) {
		list.Parent = parent
	}
	for crumb := p; b.allowed(crumb); crumb = filepath.Dir(crumb) {
		label := filepath.Base(crumb)
		if label == string(filepath.Separator) || label == "." {
			label = crumb
		}
		list.Breadcrumbs = append([]Crumb{{Name: label, Path: crumb}}, list.Breadcrumbs...)
		if filepath.Dir(crumb) == crumb {
			break
		}
	}
	return list, nil
}
func (b *Browser) Roots() []string {
	if len(b.roots) > 0 {
		return append([]string{}, b.roots...)
	}
	return platformRoots(b.home)
}
