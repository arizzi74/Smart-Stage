package browseropen

import (
	"context"
	"os/exec"
	"time"
)

func open(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/open", address).Run()
}
