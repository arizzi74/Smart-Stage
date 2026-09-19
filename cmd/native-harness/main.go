// native-harness exercises the actual platform backend; there is no mock mode.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"smartstage/internal/platform"
	"smartstage/internal/playback"
)

var version = "dev"
var commit = "unknown"

func main() {
	list := flag.Bool("list", false, "enumerate real native audio outputs and displays, then exit")
	inspect := flag.String("inspect", "", "decode a sample without rendering and report native metadata")
	file := flag.String("file", "", "local media file to play")
	audio := flag.String("audio", "default", "concrete endpoint ID, or default resolved once at start")
	display := flag.String("display", "", "stable display ID from --list")
	stopAfter := flag.Duration("stop-after", 0, "automatically issue STOP after this duration; leave black window open")
	stopPlayingAfter := flag.Duration("stop-playing-after", 0, "issue STOP this long after native playback reports playing")
	exitAfter := flag.Duration("exit-after", 0, "exit after this duration (automated native smoke testing)")
	showVersion := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *showVersion {
		fmt.Printf("Smart Stage native harness %s (%s), %s %s/%s\n", version, commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}
	if err := platform.Run(func(b playback.Backend) error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		devices, err := b.Devices(ctx)
		if err != nil {
			return err
		}
		if *list {
			return printJSON(devices)
		}
		if *inspect != "" {
			path, err := localFile(*inspect)
			if err != nil {
				return err
			}
			m, err := b.Inspect(ctx, path)
			if err != nil {
				return err
			}
			return printJSON(m)
		}
		var generation atomic.Uint64
		generation.Store(1)
		var path string
		var media playback.Media
		if *file != "" {
			path, err = localFile(*file)
			if err != nil {
				return err
			}
			media, err = b.Inspect(ctx, path)
			if err != nil {
				return err
			}
			if *audio == "default" {
				*audio = ""
				for _, d := range devices.Audio {
					if d.Default {
						*audio = d.ID
						break
					}
				}
			}
			if media.HasAudio && *audio == "" {
				return fmt.Errorf("no available audio endpoint; use --list")
			}
			if media.HasVideo && *display == "" {
				return fmt.Errorf("video requires --display with a stable ID from --list")
			}
			if *display != "" {
				fmt.Fprintln(os.Stderr, "Stage output will cover the selected display. STOP retains a black window; quit closes it.")
			}
		}
		start := func() error {
			if path == "" {
				return fmt.Errorf("supply --file to play")
			}
			return b.Start(playback.Start{Generation: generation.Add(1), Path: path, AudioID: *audio, DisplayID: *display, Video: media.HasVideo})
		}
		stop := func() { _ = b.Stop(generation.Add(1)) }
		if path != "" {
			if err := start(); err != nil {
				return err
			}
		}
		commands := make(chan string, 8)
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				select {
				case commands <- strings.TrimSpace(scanner.Text()):
				case <-ctx.Done():
					return
				}
			}
		}()
		var stopC, exitC <-chan time.Time
		if *stopAfter > 0 {
			t := time.NewTimer(*stopAfter)
			defer t.Stop()
			stopC = t.C
		}
		if *exitAfter > 0 {
			t := time.NewTimer(*exitAfter)
			defer t.Stop()
			exitC = t.C
		}
		fmt.Fprintln(os.Stderr, "Commands: stop, play (restart), enable, disable, quit. Escape in the stage window stops; Ctrl+C exits.")
		for {
			select {
			case <-ctx.Done():
				stop()
				return nil
			case <-exitC:
				stop()
				return nil
			case <-stopC:
				stop()
				stopC = nil
			case e, ok := <-b.Events():
				if !ok {
					return fmt.Errorf("native event stream closed")
				}
				_ = printJSON(e)
				if e.Kind == "playing" && *stopPlayingAfter > 0 {
					t := time.NewTimer(*stopPlayingAfter)
					defer t.Stop()
					stopC = t.C
				}
				if e.Kind == "escape" {
					stop()
				}
			case command := <-commands:
				switch command {
				case "stop":
					stop()
				case "play":
					if err := start(); err != nil {
						fmt.Fprintln(os.Stderr, err)
					}
				case "enable":
					_ = b.Stage(generation.Add(1), *display, true)
				case "disable":
					_ = b.Stage(generation.Add(1), *display, false)
				case "quit":
					stop()
					return nil
				}
			}
		}
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func localFile(name string) (string, error) {
	path, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("media must be a regular local file")
	}
	return path, nil
}
func printJSON(v any) error { return json.NewEncoder(os.Stdout).Encode(v) }
