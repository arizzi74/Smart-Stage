package files

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
)

func platformPath(path string) error {
	p := strings.ReplaceAll(path, "/", "\\")
	if strings.HasPrefix(p, `\\.\`) || strings.HasPrefix(p, `\\?\`) {
		return errors.New("Windows device namespaces are not media paths")
	}
	rest := strings.TrimPrefix(p, filepath.VolumeName(p))
	if strings.Contains(rest, ":") {
		return errors.New("alternate data streams are not media files")
	}
	return nil
}
func platformRoots(home string) []string {
	roots := []string{home}
	mask, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetLogicalDrives").Call()
	for i := 0; i < 26; i++ {
		if mask&(1<<i) != 0 {
			roots = append(roots, fmt.Sprintf("%c:\\", 'A'+i))
		}
	}
	return roots
}
