package model

import (
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
	BackgroundCueID string  `json:"backgroundCueId"`
	BackgroundAudio bool    `json:"backgroundAudio"`
	FadeEnabled     bool    `json:"fadeEnabled"`
	FadeSeconds     float64 `json:"fadeSeconds"`
	ToggleAudio     bool    `json:"toggleAudio"`
}

func (s StageSettings) Valid() bool {
	return len(s.BackgroundCueID) <= 128 && !math.IsNaN(s.FadeSeconds) && !math.IsInf(s.FadeSeconds, 0) && s.FadeSeconds >= 0.1 && s.FadeSeconds <= 30
}

type Config struct {
	Schema           int           `json:"schema"`
	PlaylistRevision uint64        `json:"playlistRevision"`
	Outputs          Outputs       `json:"outputs"`
	Stage            StageSettings `json:"stage"`
	Cues             []Cue         `json:"cues"`
}

func DefaultConfig() Config {
	return Config{Schema: SchemaVersion, PlaylistRevision: 1, Outputs: Outputs{AudioID: "default"}, Stage: StageSettings{FadeSeconds: 1}, Cues: []Cue{}}
}

func (c Config) Clone() Config { c.Cues = append([]Cue{}, c.Cues...); return c }
