package playlistfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveReplacesOnlySelectedRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Show.smartstage.json")
	if err := saveFile(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := saveFile(path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatal("Failed to replace the selected file:", err)
	}
	for _, invalid := range []string{"relative.json", filepath.Join(dir, "media.mp3"), dir} {
		if err := saveFile(invalid, []byte("bad")); err == nil {
			t.Fatal("Accepted invalid destination:", invalid)
		}
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(dir, "Link.smartstage.json")
		if err := os.Symlink(path, alias); err != nil {
			t.Fatal(err)
		}
		if err := saveFile(alias, []byte("bad")); err == nil {
			t.Fatal("Overwrote a symlink destination")
		}
		if _, err := readFile(alias); err == nil {
			t.Fatal("Read a symlink playlist")
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".smartstage-playlist-*"))
	if len(leftovers) != 0 {
		t.Fatal("Temporary playlist files were left behind")
	}
}
