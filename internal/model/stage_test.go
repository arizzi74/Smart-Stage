package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestStageFadeMigrationPreservesExplicitIndependentFields(t *testing.T) {
	for _, test := range []struct {
		name, input string
		want        StageSettings
	}{
		{"legacy enabled", `{"fadeEnabled":true,"fadeSeconds":2.5}`, StageSettings{AudioFadeEnabled: true, AudioFadeSeconds: 2.5, VisualFadeEnabled: true, VisualFadeSeconds: 2.5}},
		{"legacy disabled", `{"fadeEnabled":false,"fadeSeconds":3}`, StageSettings{AudioFadeSeconds: 3, VisualFadeSeconds: 3}},
		{"explicit false and different durations", `{"fadeEnabled":true,"fadeSeconds":2.5,"audioFadeEnabled":false,"audioFadeSeconds":0.4,"visualFadeSeconds":7}`, StageSettings{AudioFadeSeconds: 0.4, VisualFadeEnabled: true, VisualFadeSeconds: 7}},
		{"visual explicit false", `{"fadeEnabled":true,"fadeSeconds":2.5,"visualFadeEnabled":false}`, StageSettings{AudioFadeEnabled: true, AudioFadeSeconds: 2.5, VisualFadeSeconds: 2.5}},
		{"canonical independent", `{"audioFadeEnabled":true,"audioFadeSeconds":0.1,"visualFadeEnabled":false,"visualFadeSeconds":30}`, StageSettings{AudioFadeEnabled: true, AudioFadeSeconds: 0.1, VisualFadeSeconds: 30}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var settings StageSettings
			if err := json.Unmarshal([]byte(test.input), &settings); err != nil {
				t.Fatal(err)
			}
			if settings != test.want || !settings.Valid() {
				t.Fatalf("migration changed explicit settings: got %+v want %+v", settings, test.want)
			}
			encoded, err := json.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			for _, old := range []string{`"fadeEnabled"`, `"fadeSeconds"`} {
				if strings.Contains(string(encoded), old) {
					t.Fatalf("canonical output retained legacy field %s: %s", old, encoded)
				}
			}
			var roundTrip StageSettings
			if err := json.Unmarshal(encoded, &roundTrip); err != nil || roundTrip != test.want {
				t.Fatalf("canonical round trip changed migrated settings: %+v %v", roundTrip, err)
			}
		})
	}
}

func TestStageFadeDecodeRetainsStrictValidationAndContextDefaults(t *testing.T) {
	canonical := `"audioFadeEnabled":false,"audioFadeSeconds":1,"visualFadeEnabled":false,"visualFadeSeconds":2`
	for _, input := range []string{
		`null`, `[]`, `{` + canonical + `,"unexpected":true}`,
		`{` + canonical + `,"fadeSeconds":0}`, `{` + canonical + `,"fadeSeconds":31}`,
		`{` + canonical + `,"fadeSeconds":null}`, `{` + canonical + `,"fadeSeconds":1e999}`,
		`{` + canonical + `,"fadeEnabled":null}`, `{` + canonical + `,"backgroundAudio":null}`,
		`{"audioFadeEnabled":null,"audioFadeSeconds":1,"visualFadeSeconds":1}`,
		`{"audioFadeSeconds":null,"visualFadeSeconds":1}`,
		`{"audioFadeSeconds":1,"visualFadeSeconds":null}`,
		`{"audioFadeSeconds":1,"visualFadeEnabled":"false","visualFadeSeconds":1}`,
	} {
		settings := DefaultConfig().Stage
		before := settings
		if err := json.Unmarshal([]byte(input), &settings); err == nil {
			t.Errorf("malformed settings accepted: %s", input)
		} else if settings != before {
			t.Errorf("rejected decode modified existing settings: %+v", settings)
		}
	}
	for _, input := range []string{`{}`, `{"audioFadeSeconds":1}`, `{"visualFadeSeconds":1}`, `{"fadeEnabled":true}`} {
		var settings StageSettings
		if err := json.Unmarshal([]byte(input), &settings); err != nil || settings.Valid() {
			t.Errorf("incomplete API/playlist settings became valid: %s %+v %v", input, settings, err)
		}
	}
	defaults := DefaultConfig().Stage
	if defaults.AudioFadeEnabled || defaults.VisualFadeEnabled || defaults.AudioFadeSeconds != 1 || defaults.VisualFadeSeconds != 1 {
		t.Fatalf("unexpected initial fades: %+v", defaults)
	}
	if err := json.Unmarshal([]byte(`{}`), &defaults); err != nil || defaults != DefaultConfig().Stage {
		t.Fatalf("old config settings lost supplied migration defaults: %+v %v", defaults, err)
	}
}

func TestStageFadeDurationsValidateIndependentlyEvenWhenDisabled(t *testing.T) {
	for _, field := range []string{"audio", "visual"} {
		for _, value := range []float64{0, -1, 0.099, 30.001, math.NaN(), math.Inf(1), math.Inf(-1)} {
			settings := DefaultConfig().Stage
			if field == "audio" {
				settings.AudioFadeSeconds = value
			} else {
				settings.VisualFadeSeconds = value
			}
			if settings.Valid() {
				t.Errorf("disabled %s fade accepted invalid duration %v", field, value)
			}
		}
	}
	for _, audio := range []bool{false, true} {
		for _, visual := range []bool{false, true} {
			settings := StageSettings{AudioFadeEnabled: audio, AudioFadeSeconds: 0.1, VisualFadeEnabled: visual, VisualFadeSeconds: 30}
			if !settings.Valid() {
				t.Errorf("independent settings rejected at valid bounds: %+v", settings)
			}
		}
	}
}
