// Package playback defines the platform-independent native playback boundary.
package playback

import "context"

type AudioDevice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

type Display struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Primary  bool   `json:"primary"`
	Mirrored bool   `json:"mirrored"`
}

type Devices struct {
	Audio    []AudioDevice `json:"audio"`
	Displays []Display     `json:"displays"`
}

type Media struct {
	Kind     string  `json:"kind"`
	Duration float64 `json:"duration"` // seconds; zero means unknown
	HasAudio bool    `json:"hasAudio"`
	HasVideo bool    `json:"hasVideo"`
}

// Start describes one generation. AudioID must be a concrete endpoint; the
// coordinator resolves the system-default preference afresh for each cue.
type Start struct {
	Generation uint64
	Path       string
	AudioID    string
	DisplayID  string
	Video      bool
}

type Event struct {
	Generation   uint64  `json:"generation"`
	Kind         string  `json:"kind"` // playing, progress, ended, stopped, error, devices, escape
	Position     float64 `json:"position"`
	Duration     float64 `json:"duration"`
	Message      string  `json:"message,omitempty"`
	StageEnabled bool    `json:"stageEnabled"`
}

// Backend does not retain Go pointers in native objects. Start/Stop/Stage enqueue
// native work and return immediately; authoritative completion arrives in Events.
// Stop invalidates older generations before it enqueues UI work. Implementations
// must also check generation immediately before native start and video reveal.
// Inspect never uses a live renderer. Callers must bound concurrent inspections.
type Backend interface {
	Devices(context.Context) (Devices, error)
	Inspect(context.Context, string) (Media, error)
	Start(Start) error
	Stop(generation uint64) error
	Stage(generation uint64, displayID string, enabled bool) error
	Events() <-chan Event
	Close() error
}
