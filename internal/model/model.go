package model

import "smartstage/internal/playback"

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
	ID    string     `json:"id"`
	Label string     `json:"label"`
	Path  string     `json:"path"`
	Color string     `json:"color,omitempty"`
	Cache Validation `json:"cache"`
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

type Config struct {
	Schema           int     `json:"schema"`
	PlaylistRevision uint64  `json:"playlistRevision"`
	Outputs          Outputs `json:"outputs"`
	Cues             []Cue   `json:"cues"`
}

func DefaultConfig() Config {
	return Config{Schema: SchemaVersion, PlaylistRevision: 1, Outputs: Outputs{AudioID: "default"}, Cues: []Cue{}}
}

func (c Config) Clone() Config { c.Cues = append([]Cue{}, c.Cues...); return c }
