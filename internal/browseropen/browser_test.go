package browseropen

import "testing"

func TestRejectNonAdminURLsWithoutLaunching(t *testing.T) {
	for _, address := range []string{
		"https://127.0.0.1:8787/admin", "http://192.168.1.2:8787/admin",
		"http://127.0.0.1:8787/command", "http://127.0.0.1/admin",
		"http://user@127.0.0.1:8787/admin", "http://127.0.0.1:8787/admin?token=1234",
		"http://127.0.0.1:8787/admin#token=1234", "file:///tmp/admin", "not a URL",
	} {
		if err := Open(address); err == nil {
			t.Errorf("accepted invalid browser address %q", address)
		}
	}
}
