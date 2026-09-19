package files

import (
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
	if list.Path != root || list.Parent != "" || len(list.Entries) != 1 || list.Entries[0].Name != name {
		t.Fatalf("bad restricted listing: %+v", list)
	}
	if p, _, err := b.File(path); err != nil || p != path {
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
}
