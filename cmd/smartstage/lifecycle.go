package main

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const adminReconnectGrace = 6 * time.Second

// adminBrowser coalesces automatic startup and native Dock/import requests.
// A second native request waits for a just-opened page to establish presence
// instead of dispatching another URL while that browser page is still loading.
type adminBrowser struct {
	ctx               context.Context
	present, updating func() bool
	open              func() error
	activate          func()
	deadline          time.Time
	mu                sync.Mutex
	pending           bool
	explicit          bool
	lastOpen          time.Time
}

func newAdminBrowser(ctx context.Context, present, updating func() bool, open func() error, activate func()) *adminBrowser {
	return &adminBrowser{ctx: ctx, present: present, updating: updating, open: open, activate: activate, deadline: time.Now().Add(adminReconnectGrace)}
}

func (b *adminBrowser) Request(explicit bool) {
	b.mu.Lock()
	b.explicit = b.explicit || explicit
	if b.pending || b.ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	b.pending = true
	b.mu.Unlock()
	go func() {
		defer func() {
			b.mu.Lock()
			b.pending = false
			b.explicit = false
			b.mu.Unlock()
		}()
		opened, err := openAdminWhenReady(b.ctx, time.Until(b.deadline), b.present, func() bool {
			b.mu.Lock()
			loading := !b.lastOpen.IsZero() && time.Since(b.lastOpen) < adminReconnectGrace
			b.mu.Unlock()
			return loading || b.updating()
		}, func() error {
			err := b.open()
			if err == nil {
				b.mu.Lock()
				b.lastOpen = time.Now()
				b.mu.Unlock()
			}
			return err
		})
		if err != nil {
			slog.Warn("Could not open the system browser; open the printed Admin URL", "error", err)
		} else if opened {
			b.mu.Lock()
			explicit := b.explicit
			b.mu.Unlock()
			if explicit {
				slog.Info("Reopened Admin in the system browser")
			} else {
				slog.Info("Opened Admin in the system browser")
			}
		} else if b.ctx.Err() == nil && b.present() {
			slog.Info("Reusing the existing Admin browser page")
			if b.activate != nil {
				b.activate()
			}
		}
	}()
}

// openAdminWhenReady gives an existing tab time to reacquire its local session.
// It never opens a replacement tab while startup updating owns the service, and
// cancellation abandons the pending launch during Quit or update handoff.
func openAdminWhenReady(ctx context.Context, grace time.Duration, present, updating func() bool, open func() error) (bool, error) {
	deadline := time.Now().Add(grace)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil || present() {
			return false, nil
		}
		if !time.Now().Before(deadline) && !updating() {
			if ctx.Err() != nil || present() {
				return false, nil
			}
			return true, open()
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-ticker.C:
		}
	}
}
