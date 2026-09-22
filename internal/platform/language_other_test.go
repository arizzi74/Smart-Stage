//go:build (!darwin && !windows) || !cgo

package platform

import "testing"

func TestSystemLanguageLocalePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, all, messages, lang, want string
	}{
		{"Italian region", "", "", "it_IT.UTF-8", "it"},
		{"Italian BCP47", "", "", "IT-ch", "it"},
		{"messages override", "", "en_GB.UTF-8", "it_IT.UTF-8", "en"},
		{"all override", "it_IT", "en_US", "en_US", "it"},
		{"unsupported messages override", "", "fr_FR", "it_IT", "en"},
		{"C locale", "C.UTF-8", "it_IT", "it_IT", "en"},
		{"unset", "", "", "", "en"},
		{"malformed", "", "", "---", "en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.all)
			t.Setenv("LC_MESSAGES", tc.messages)
			t.Setenv("LANG", tc.lang)
			if got := SystemLanguage(); got != tc.want {
				t.Fatalf("SystemLanguage() = %q, want %q", got, tc.want)
			}
		})
	}
}
