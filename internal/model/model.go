package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"smartstage/internal/playback"
)

const SchemaVersion = 1
const MaxCues = 500

type Validation struct {
	Status   string         `json:"status"`
	Reason   string         `json:"reason,omitempty"`
	Media    playback.Media `json:"media"`
	Size     int64          `json:"size"`
	Modified int64          `json:"modified"`
}

type Cue struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Path       string     `json:"path"`
	Color      string     `json:"color,omitempty"`
	Hidden     bool       `json:"hidden,omitempty"`
	Background bool       `json:"background,omitempty"`
	Cache      Validation `json:"cache"`
}

// ValidCueColor accepts the default appearance or a plain RGB color. Keeping
// this value narrower than CSS prevents saved shows from supplying style text.
func ValidCueColor(color string) bool {
	if color == "" {
		return true
	}
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, c := range color[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

type Outputs struct {
	AudioID      string `json:"audioId"`
	DisplayID    string `json:"displayId"`
	AllowPrimary bool   `json:"allowPrimary"`
}

type StageSettings struct {
	BackgroundCueID   string  `json:"backgroundCueId"`
	BackgroundAudio   bool    `json:"backgroundAudio"`
	AudioFadeEnabled  bool    `json:"audioFadeEnabled"`
	AudioFadeSeconds  float64 `json:"audioFadeSeconds"`
	VisualFadeEnabled bool    `json:"visualFadeEnabled"`
	VisualFadeSeconds float64 `json:"visualFadeSeconds"`
	ToggleAudio       bool    `json:"toggleAudio"` // Legacy stored preference; foreground buttons always pause/resume.
}

// stageField distinguishes an explicit false/zero from an absent field. Null
// must not turn malformed saved settings into valid migration defaults.
type stageField[T any] struct {
	value   T
	present bool
}

func (f *stageField[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("stage settings fields cannot be null")
	}
	if err := json.Unmarshal(data, &f.value); err != nil {
		return err
	}
	f.present = true
	return nil
}

func (f stageField[T]) or(fallback T) T {
	if f.present {
		return f.value
	}
	return fallback
}

// UnmarshalJSON accepts the original shared fade fields as a migration input.
// Each explicit canonical field wins independently, including false. Omitted
// fields retain the receiver's defaults: config loading supplies defaults,
// while incomplete API/playlist settings still fail Valid.
func (s *StageSettings) UnmarshalJSON(data []byte) error {
	if data = bytes.TrimSpace(data); len(data) == 0 || data[0] != '{' {
		return errors.New("stage settings must be an object")
	}
	var wire struct {
		BackgroundCueID   stageField[string]  `json:"backgroundCueId"`
		BackgroundAudio   stageField[bool]    `json:"backgroundAudio"`
		AudioFadeEnabled  stageField[bool]    `json:"audioFadeEnabled"`
		AudioFadeSeconds  stageField[float64] `json:"audioFadeSeconds"`
		VisualFadeEnabled stageField[bool]    `json:"visualFadeEnabled"`
		VisualFadeSeconds stageField[float64] `json:"visualFadeSeconds"`
		ToggleAudio       stageField[bool]    `json:"toggleAudio"`
		FadeEnabled       stageField[bool]    `json:"fadeEnabled"`
		FadeSeconds       stageField[float64] `json:"fadeSeconds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("trailing stage settings data")
	}
	if wire.FadeSeconds.present && !validFadeSeconds(wire.FadeSeconds.value) {
		return errors.New("legacy fade duration must be between 0.1 and 30 seconds")
	}
	*s = StageSettings{
		BackgroundCueID: wire.BackgroundCueID.or(s.BackgroundCueID), BackgroundAudio: wire.BackgroundAudio.or(s.BackgroundAudio),
		AudioFadeEnabled:  wire.AudioFadeEnabled.or(wire.FadeEnabled.or(s.AudioFadeEnabled)),
		AudioFadeSeconds:  wire.AudioFadeSeconds.or(wire.FadeSeconds.or(s.AudioFadeSeconds)),
		VisualFadeEnabled: wire.VisualFadeEnabled.or(wire.FadeEnabled.or(s.VisualFadeEnabled)),
		VisualFadeSeconds: wire.VisualFadeSeconds.or(wire.FadeSeconds.or(s.VisualFadeSeconds)),
		ToggleAudio:       wire.ToggleAudio.or(s.ToggleAudio),
	}
	return nil
}

func validFadeSeconds(seconds float64) bool {
	return !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds >= 0.1 && seconds <= 30
}

func (s StageSettings) Valid() bool {
	return len(s.BackgroundCueID) <= 128 && validFadeSeconds(s.AudioFadeSeconds) && validFadeSeconds(s.VisualFadeSeconds)
}

type Config struct {
	Schema           int           `json:"schema"`
	PlaylistRevision uint64        `json:"playlistRevision"`
	Outputs          Outputs       `json:"outputs"`
	Stage            StageSettings `json:"stage"`
	Cues             []Cue         `json:"cues"`
}

func DefaultConfig() Config {
	return Config{Schema: SchemaVersion, PlaylistRevision: 1, Outputs: Outputs{AudioID: "default"}, Stage: StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1}, Cues: []Cue{}}
}

func (c Config) Clone() Config { c.Cues = append([]Cue{}, c.Cues...); return c }
