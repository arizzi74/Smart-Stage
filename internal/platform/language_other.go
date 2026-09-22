//go:build (!darwin && !windows) || !cgo

package platform

import (
	"os"
	"strings"
)

// SystemLanguage uses the process locale where a native UI API is unavailable.
// LC_ALL and LC_MESSAGES override LANG, as they do for localized process text.
func SystemLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == '-' || r == '_' || r == '.' || r == '@'
		})
		if len(parts) > 0 && strings.EqualFold(parts[0], "it") {
			return "it"
		}
		return "en"
	}
	return "en"
}

func DesktopLanguage(string) {}
