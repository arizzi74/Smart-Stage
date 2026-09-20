//go:build (windows || darwin) && cgo

package platform

/*
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"smartstage/internal/playback"
	"sync"
	"time"
	"unsafe"
)

type native struct {
	events   chan playback.Event
	done     chan struct{}
	pollDone chan struct{}
	once     sync.Once
	inspect  chan struct{}
	lifeMu   sync.Mutex
	closed   bool
	work     sync.WaitGroup
}

// Package initialization runs on the initial process thread. Lock before main
// performs any work that could otherwise migrate its goroutine (Cocoa requires
// the original main thread, not merely an arbitrary locked thread).
func init() { runtime.LockOSThread() }

// Run must be called from main. Cocoa and the Win32 window/message loop remain
// on that OS thread for the entire process lifetime.
func Run(app func(playback.Backend) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := readError(C.ss_init()); err != nil {
		return err
	}
	n := &native{events: make(chan playback.Event, 128), done: make(chan struct{}), pollDone: make(chan struct{}), inspect: make(chan struct{}, 1)}
	result := make(chan error, 1)
	go n.poll()
	go func() { result <- app(n); _ = n.Close() }()
	C.ss_run()
	return <-result
}

func readString(p *C.char) string {
	if p == nil {
		return ""
	}
	defer C.ss_free(p)
	return C.GoString(p)
}

func readError(p *C.char) error {
	if s := readString(p); s != "" {
		return errors.New(s)
	}
	return nil
}

func decode(p *C.char, target any) error {
	s := readString(p)
	var failure struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(s), &failure); err != nil {
		return err
	}
	if failure.Error != "" {
		return errors.New(failure.Error)
	}
	return json.Unmarshal([]byte(s), target)
}

func (n *native) Devices(ctx context.Context) (d playback.Devices, err error) {
	if err = n.begin(); err != nil {
		return
	}
	defer n.work.Done()
	if err = ctx.Err(); err != nil {
		return
	}
	err = decode(C.ss_devices(), &d)
	return
}

func (n *native) Inspect(ctx context.Context, path string) (playback.Media, error) {
	if err := n.begin(); err != nil {
		return playback.Media{}, err
	}
	// A slow filesystem/decoder occupies at most one native inspector. A cancelled
	// caller returns immediately; its worker releases the slot after native exit.
	select {
	case n.inspect <- struct{}{}:
	case <-ctx.Done():
		n.work.Done()
		return playback.Media{}, ctx.Err()
	case <-n.done:
		n.work.Done()
		return playback.Media{}, errors.New("backend closed")
	}
	type reply struct {
		media playback.Media
		err   error
	}
	out := make(chan reply, 1)
	go func() {
		defer n.work.Done()
		defer func() { <-n.inspect }()
		p := C.CString(path)
		defer C.free(unsafe.Pointer(p))
		var m playback.Media
		err := decode(C.ss_inspect(p), &m)
		out <- reply{m, err}
	}()
	select {
	case r := <-out:
		return r.media, r.err
	case <-ctx.Done():
		return playback.Media{}, ctx.Err()
	case <-n.done:
		return playback.Media{}, errors.New("backend closed")
	}
}

func (n *native) Start(s playback.Start) error {
	if err := n.begin(); err != nil {
		return err
	}
	defer n.work.Done()
	path, audio, display := C.CString(s.Path), C.CString(s.AudioID), C.CString(s.DisplayID)
	defer C.free(unsafe.Pointer(path))
	defer C.free(unsafe.Pointer(audio))
	defer C.free(unsafe.Pointer(display))
	v := C.int(0)
	if s.Video {
		v = 1
	}
	C.ss_start(C.uint64_t(s.Generation), path, audio, display, v)
	return nil
}
func (n *native) Stop(g uint64) error {
	if err := n.begin(); err != nil {
		return err
	}
	defer n.work.Done()
	C.ss_stop(C.uint64_t(g))
	return nil
}
func (n *native) Stage(g uint64, id string, enabled bool) error {
	if err := n.begin(); err != nil {
		return err
	}
	defer n.work.Done()
	p := C.CString(id)
	defer C.free(unsafe.Pointer(p))
	v := C.int(0)
	if enabled {
		v = 1
	}
	C.ss_stage(C.uint64_t(g), p, v)
	return nil
}
func (n *native) Events() <-chan playback.Event { return n.events }
func (n *native) Close() error {
	n.once.Do(func() {
		n.lifeMu.Lock()
		n.closed = true
		close(n.done)
		n.lifeMu.Unlock()
		// Keep the native event loop/framework alive until inspectors and device
		// calls release their OS objects. A cancelled caller may have returned
		// before its underlying native operation finished.
		n.work.Wait()
		<-n.pollDone
		C.ss_quit()
	})
	return nil
}
func (n *native) begin() error {
	n.lifeMu.Lock()
	defer n.lifeMu.Unlock()
	if n.closed {
		return errors.New("native backend is closed")
	}
	n.work.Add(1)
	return nil
}
func (n *native) poll() {
	defer close(n.pollDone)
	defer close(n.events)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-n.done:
			return
		case <-ticker.C:
		}
		pollDesktopFiles()
		for i := 0; i < 128; i++ {
			p := C.ss_poll()
			if p == nil {
				break
			}
			var e playback.Event
			if decode(p, &e) != nil {
				continue
			}
			select {
			case n.events <- e:
			case <-n.done:
				return
			}
		}
	}
}
