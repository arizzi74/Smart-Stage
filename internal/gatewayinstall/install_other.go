//go:build !linux

package gatewayinstall

import "fmt"

func Run(args []string) error {
	return fmt.Errorf("the gateway installer supports Linux with systemd only")
}
