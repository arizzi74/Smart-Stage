package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootsCanonicalContainmentAndUnicode(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "media")
	outside := filepath.Join(base, "media-other")
	for _, p := range []string{root, outside} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	name := "Café's song – 1.wav"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{outside, filepath.Join(root, "..", "media-other"), "https://example.com/song.mp3"} {
		if _, err := b.Resolve(p); err == nil {
			t.Fatalf("escaped root: %s", p)
		}
	}
	list, err := b.Browse("")
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if list.Path != canonicalRoot || list.Parent != "" || len(list.Entries) != 1 || list.Entries[0].Name != name {
		t.Fatalf("bad restricted listing: %+v", list)
	}
	canonicalFile, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, _, err := b.File(path); err != nil || p != canonicalFile {
		t.Fatalf("unicode file: %s %v", p, err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := b.Resolve(link); err == nil {
			t.Fatal("symlink escape admitted")
		}
	}
}
func TestListingBoundsAndMissingFile(t *testing.T) {
	root := t.TempDir()
	b, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.File(filepath.Join(root, "missing.mp3")); !os.IsNotExist(err) {
		t.Fatalf("missing: %v", err)
	}
	if _, _, err := b.File(root); err == nil {
		t.Fatal("directory admitted as cue")
	}
	if _, err := b.Browse(root + string(os.PathSeparator) + "missing"); err == nil {
		t.Fatal("missing directory admitted")
	}
	if _, err := b.Resolve(strings.Repeat("a", 32769)); err == nil {
		t.Fatal("unbounded path")
	}
	for i := 0; i <= MaxEntries; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("cue-%04d.wav", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	listing, err := b.Browse(root)
	if err != nil {
		t.Fatal(err)
	}
	if !listing.Truncated || len(listing.Entries) != MaxEntries {
		t.Fatalf("large listing returned %d entries, truncated=%v", len(listing.Entries), listing.Truncated)
	}
}

func TestHiddenEntriesAreOptionalAndExplicitPathsStillWork(t *testing.T) {
	root := t.TempDir()
	hiddenDirectory := filepath.Join(root, ".media")
	if err := os.Mkdir(hiddenDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".hidden.wav", "visible.wav", "song.with.dots.wav", ".media/inside.wav", ".media/.notes"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	b, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	list, err := b.Browse(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 2 || list.Entries[0].Name != "song.with.dots.wav" || list.Entries[1].Name != "visible.wav" {
		t.Fatalf("default listing did not hide dot files and folders: %+v", list)
	}
	all, err := b.BrowseWithHidden(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 4 || all.Entries[0].Name != ".media" || !all.Entries[0].Directory {
		t.Fatalf("show hidden did not retain directory-first ordering: %+v", all)
	}
	inside, err := b.Browse(hiddenDirectory)
	if err != nil || len(inside.Entries) != 1 || inside.Entries[0].Name != "inside.wav" || inside.Parent != list.Path {
		t.Fatalf("explicit hidden-directory navigation failed: %+v %v", inside, err)
	}
	if _, _, err := b.File(filepath.Join(root, ".hidden.wav")); err != nil {
		t.Fatal("hidden preference became an access restriction:", err)
	}
	outside := t.TempDir()
	if _, err := b.BrowseWithHidden(outside, true); err == nil {
		t.Fatal("show hidden bypassed media-root containment")
	}
}

func TestHiddenEntriesDoNotConsumeVisibleLimitOrCauseTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= MaxEntries; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf(".hidden-%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("visible-%04d.wav", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	b, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	list, err := b.Browse(root)
	if err != nil || len(list.Entries) != 3 || list.Truncated {
		t.Fatalf("hidden entries consumed the visible limit: count=%d truncated=%v err=%v", len(list.Entries), list.Truncated, err)
	}
	all, err := b.BrowseWithHidden(root, true)
	if err != nil || len(all.Entries) != MaxEntries || !all.Truncated {
		t.Fatalf("show hidden bypassed the result limit: count=%d truncated=%v err=%v", len(all.Entries), all.Truncated, err)
	}
	for i := 3; i < MaxEntries; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("visible-%04d.wav", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	list, err = b.Browse(root)
	if err != nil || len(list.Entries) != MaxEntries || list.Truncated {
		t.Fatalf("hidden-only overflow incorrectly marked a full visible page truncated: count=%d truncated=%v err=%v", len(list.Entries), list.Truncated, err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible-extra.wav"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	list, err = b.Browse(root)
	if err != nil || len(list.Entries) != MaxEntries || !list.Truncated {
		t.Fatalf("visible overflow not bounded: count=%d truncated=%v err=%v", len(list.Entries), list.Truncated, err)
	}
}
