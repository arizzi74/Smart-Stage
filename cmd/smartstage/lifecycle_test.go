package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestAdminStartupWaitsForReconnectThenOpensOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		done := make(chan bool, 1)
		go func() {
			opened, err := openAdminWhenReady(t.Context(), adminReconnectGrace, func() bool { return false }, func() bool { return false }, func() error { calls.Add(1); return nil })
			if err != nil {
				t.Error(err)
			}
			done <- opened
		}()
		synctest.Wait()
		time.Sleep(5 * time.Second)
		if calls.Load() != 0 {
			t.Fatal("Browser opened before the old Admin tab's reconnect grace")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if !<-done || calls.Load() != 1 {
			t.Fatal("Browser did not open exactly once after the grace")
		}
	})
}

func TestAdminReconnectSuppressesLaunchBeforeGraceEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var present atomic.Bool
		done := make(chan bool, 1)
		go func() {
			opened, _ := openAdminWhenReady(t.Context(), adminReconnectGrace, present.Load, func() bool { return false }, func() error { t.Error("Opened duplicate Admin tab"); return nil })
			done <- opened
		}()
		synctest.Wait()
		time.Sleep(time.Second)
		present.Store(true)
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		select {
		case opened := <-done:
			if opened {
				t.Fatal("Reconnected Admin did not suppress browser launch")
			}
		default:
			t.Fatal("Presence unnecessarily waited for the full startup grace")
		}
	})
}

func TestStartupUpdateDefersBrowserAndQuitCancelsPendingLaunch(t *testing.T) {
	for _, cancelInstead := range []bool{false, true} {
		t.Run(map[bool]string{false: "update-check-finished", true: "quit-or-update-handoff"}[cancelInstead], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var updating atomic.Bool
				updating.Store(true)
				var calls atomic.Int32
				done := make(chan bool, 1)
				go func() {
					opened, _ := openAdminWhenReady(ctx, adminReconnectGrace, func() bool { return false }, updating.Load, func() error { calls.Add(1); return nil })
					done <- opened
				}()
				synctest.Wait()
				time.Sleep(20 * time.Second)
				if calls.Load() != 0 {
					t.Fatal("Browser opened while startup updating owned the service")
				}
				if cancelInstead {
					cancel()
				} else {
					updating.Store(false)
				}
				time.Sleep(100 * time.Millisecond)
				synctest.Wait()
				opened := <-done
				if opened == cancelInstead || calls.Load() != map[bool]int32{false: 1, true: 0}[cancelInstead] {
					t.Fatal("Pending browser launch did not honor update completion or cancellation")
				}
			})
		})
	}
}

func TestStartupQuitBeforeGraceAndBrowserLaunchError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened, err := openAdminWhenReady(ctx, adminReconnectGrace, func() bool { return false }, func() bool { return false }, func() error { t.Fatal("Opened Admin after Quit"); return nil })
	if opened || err != nil {
		t.Fatal("Canceled browser request was not abandoned")
	}
	failure := errors.New("browser failed")
	opened, err = openAdminWhenReady(context.Background(), 0, func() bool { return false }, func() bool { return false }, func() error { return failure })
	if !opened || !errors.Is(err, failure) {
		t.Fatal("Browser launch failure was lost")
	}
}

func TestStartupAndNativeBrowserRequestsAreCoalesced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var present atomic.Bool
		var opens, activations atomic.Int32
		browser := newAdminBrowser(ctx, present.Load, func() bool { return false },
			func() error { opens.Add(1); return nil }, func() { activations.Add(1) })
		browser.Request(false)
		for range 10 {
			browser.Request(true)
		}
		synctest.Wait()
		time.Sleep(5 * time.Second)
		if opens.Load() != 0 {
			t.Fatal("Native request bypassed startup reconnect grace")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if opens.Load() != 1 {
			t.Fatal("Startup/native requests did not dispatch exactly one URL")
		}
		for range 10 {
			browser.Request(true)
		}
		synctest.Wait()
		time.Sleep(time.Second)
		if opens.Load() != 1 {
			t.Fatal("Native request duplicated a still-loading browser page")
		}
		present.Store(true)
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if opens.Load() != 1 || activations.Load() != 1 {
			t.Fatal("Existing browser was not activated without another URL")
		}
		cancel()
		browser.Request(true)
		synctest.Wait()
		if opens.Load() != 1 || activations.Load() != 1 {
			t.Fatal("Quit did not cancel native browser requests")
		}
	})
}
