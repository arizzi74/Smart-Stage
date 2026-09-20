// Package browseropen opens the localhost Admin page through the host OS.
package browseropen

import (
	"errors"
	"net/url"
)

// Open accepts only the application's localhost Admin URL. No shell command
// line is built from URL text, and remote pairing tokens never enter this path.
func Open(address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/admin" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("browser launch requires a localhost Admin URL")
	}
	return open(address)
}
