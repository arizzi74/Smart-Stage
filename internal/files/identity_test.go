package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootIdentityFallbackRejectsDistinctRootsAndSymlinkEscapes(t *testing.T) {
	base := t.TempDir()
	root, other := filepath.Join(base, "media"), filepath.Join(base, "media-other")
	for _, directory := range []string{root, other} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "song.wav"), []byte("same bytes, distinct files"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	browser, err := New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := browser.File(filepath.Join(other, "song.wav")); err == nil {
		t.Fatal("filesystem fallback admitted a distinct sibling root")
	}
	link := filepath.Join(root, "outside")
	if err := os.Symlink(other, link); err == nil {
		if _, _, err := browser.File(filepath.Join(link, "song.wav")); err == nil {
			t.Fatal("filesystem fallback followed an escaping candidate symlink")
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(root, root+"-original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, root); err != nil {
			t.Fatal(err)
		}
		if _, _, err := browser.File(filepath.Join(root, "song.wav")); err == nil {
			t.Fatal("filesystem fallback authorized a root replaced with an escaping symlink")
		}
	} else {
		t.Logf("Symlink escape portion unavailable on this host: %v", err)
	}
}

func TestRootIdentityAcceptsActualUnicodeAndCaseAliases(t *testing.T) {
	for _, names := range [][2]string{{"Caf\u00e9 media", "Cafe\u0301 media"}, {"CaseMedia", "casemedia"}} {
		t.Run(names[0], func(t *testing.T) {
			base := t.TempDir()
			root, alias := filepath.Join(base, names[0]), filepath.Join(base, names[1])
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			rootInfo, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			aliasInfo, err := os.Stat(alias)
			if os.IsNotExist(err) {
				t.Skip("This filesystem treats these Unicode/case spellings as distinct paths")
			}
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(rootInfo, aliasInfo) {
				t.Skip("These spellings do not alias the same directory on this filesystem")
			}
			if err := os.WriteFile(filepath.Join(root, "song.wav"), []byte("original media"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2]string{{root, alias}, {alias, root}} {
				browser, err := New([]string{pair[0]})
				if err != nil {
					t.Fatal(err)
				}
				path, info, err := browser.File(filepath.Join(pair[1], "song.wav"))
				if err != nil {
					t.Fatalf("actual filesystem alias rejected: configured=%q file=%q: %v", pair[0], pair[1], err)
				}
				original, err := os.Stat(filepath.Join(root, "song.wav"))
				if err != nil || !os.SameFile(info, original) {
					t.Fatalf("alias resolved to different media: %q %v", path, err)
				}
				list, err := browser.Browse(pair[1])
				if err != nil || len(list.Entries) != 1 || list.Parent != "" {
					t.Fatalf("alias browsing escaped the root or lost the file: %+v %v", list, err)
				}
				outside := filepath.Join(base, names[0]+"-outside")
				if err := os.MkdirAll(outside, 0700); err != nil {
					t.Fatal(err)
				}
				if _, err := browser.Resolve(outside); err == nil {
					t.Fatal("Unicode/case fallback admitted a distinct directory")
				}
			}
		})
	}
}
