//go:build !windows

package files

import (
	"os"
	"path/filepath"
	"runtime"
)

func platformPath(string) error { return nil }
func platformRoots(home string) []string {
	roots := []string{home, "/"}
	if runtime.GOOS == "darwin" {
		if entries, err := os.ReadDir("/Volumes"); err == nil {
			for _, e := range entries {
				if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
					roots = append(roots, filepath.Join("/Volumes", e.Name()))
				}
			}
		}
	}
	return roots
}
