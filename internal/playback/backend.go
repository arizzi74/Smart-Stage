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

// AudioLayer is one independently controlled audio-only cue. ID is a unique
// start identity; scene changes preserve its decoder and transport. Events use
// ID as Generation and must carry its exact TransportRevision.
type AudioLayer struct {
	ID                uint64
	Path              string
	AudioID           string
	Paused            bool
	TransportRevision uint64
	SeekRevision      uint64
	SeekSeconds       float64
	Volume            float64
}

// Scene is a complete desired presentation. Revision orders native work;
// ForegroundID identifies the primary audio/video start and remains stable
// across image/background/stage changes. Images never replace foreground sound.
// TransportRevision changes for each start, pause, resume or seek. Foreground
// events must carry the applied revision so superseded callbacks can be ignored.
// SeekRevision is zero until the first seek and changes only for a new seek;
// applying later scenes with that same revision must not replay the seek.
// AudioLayers add independent audio-only sources; each has its own identity and
// transport. ForegroundMuted suppresses a retained video's soundtrack without
// changing its decoder, timeline or HasAudio validation. BackgroundAudio is the
// final eligibility decision, including replacement versus mixed-audio policy.
// Generation orders command epochs and can exceed every currently applied
// source ID while another source prepares. Stage/image updates preserve all
// source identities; HardStop disposes the foreground and every audio layer.
type Scene struct {
	Revision           uint64
	Generation         uint64
	ForegroundID       uint64
	ForegroundPath     string
	ForegroundKind     string
	ForegroundHasAudio bool
	ForegroundMuted    bool
	AudioLayers        []AudioLayer
	ForegroundPaused   bool
	TransportRevision  uint64
	SeekRevision       uint64
	SeekSeconds        float64
	ImagePath          string
	BackgroundPath     string
	BackgroundKind     string
	BackgroundAudio    bool // final soundtrack eligibility; native must not infer foreground suppression
	AudioID            string
	DisplayID          string
	StageEnabled       bool
	AudioFadeSeconds   float64
	VisualFadeSeconds  float64
	HardStop           bool
	MasterVolume       float64
	ForegroundVolume   float64
	BackgroundVolume   float64
}

// SceneBackend adds independent stage/background presentation and audio mixing.
// Legacy Start/Stop/Stage remain available to the native diagnostic harness.
type SceneBackend interface {
	ApplyScene(Scene) error
}

type Event struct {
	Generation        uint64  `json:"generation"`
	SceneRevision     uint64  `json:"sceneRevision,omitempty"`
	TransportRevision uint64  `json:"transportRevision,omitempty"`
	Kind              string  `json:"kind"` // playing, paused, progress, ended, stopped, error, devices, escape
	Position          float64 `json:"position"`
	Duration          float64 `json:"duration"`
	Message           string  `json:"message,omitempty"`
	StageEnabled      bool    `json:"stageEnabled"`
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
