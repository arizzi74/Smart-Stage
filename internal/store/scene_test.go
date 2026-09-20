package store

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"smartstage/internal/model"
)

func TestLegacyShowDefaultsAndSceneSettingsPersistWithoutRewritingOnRead(t *testing.T) {
	dir := t.TempDir()
	// The pre-background schema has no stage object or per-cue flags. Use the
	// old wire shape so this test cannot accidentally supply new defaults.
	legacy := struct {
		Schema           int           `json:"schema"`
		PlaylistRevision uint64        `json:"playlistRevision"`
		Outputs          model.Outputs `json:"outputs"`
		Cues             []model.Cue   `json:"cues"`
	}{Schema: 1, PlaylistRevision: 7, Outputs: model.Outputs{AudioID: "default"}, Cues: []model.Cue{{ID: "image", Label: "Old show image", Path: filepath.Join(dir, "backdrop.png"), Color: "#12abEF"}}}
	original, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	storage, config, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if config.Stage != (model.StageSettings{FadeSeconds: 1}) || config.Cues[0].Hidden || config.Cues[0].Background || config.PlaylistRevision != 7 || config.Cues[0].Color != "#12abEF" {
		t.Fatalf("legacy show defaults changed existing behavior: %+v", config)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("reading a legacy show rewrote its saved bytes")
	}
	config.Stage = model.StageSettings{BackgroundCueID: "image", BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 1.7, ToggleAudio: true}
	config.Cues[0].Hidden, config.Cues[0].Background = true, true
	config.PlaylistRevision++
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "state.json.bak"))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("first save after migration did not preserve the exact legacy backup")
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, restored, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if restored.Stage != config.Stage || !restored.Cues[0].Hidden || !restored.Cues[0].Background || restored.PlaylistRevision != 8 || restored.Cues[0].Color != config.Cues[0].Color {
		t.Fatalf("new settings or old cue data changed after reopening: %+v", restored)
	}
}

func TestInvalidSceneSettingsCannotReplaceSavedShowOrBackup(t *testing.T) {
	dir := t.TempDir()
	storage, config, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	config.Cues = []model.Cue{{ID: "background", Label: "Hidden background", Path: filepath.Join(dir, "background.mp4"), Hidden: true, Background: true}}
	config.Stage = model.StageSettings{BackgroundCueID: "background", BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 1}
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	config.PlaylistRevision++
	config.Stage.FadeSeconds = 2
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, name := range []string{"state.json", "state.json.bak"} {
		before[name], err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(*model.Config)
	}{
		{"zero duration", func(c *model.Config) { c.Stage.FadeSeconds = 0 }},
		{"negative duration", func(c *model.Config) { c.Stage.FadeSeconds = -1 }},
		{"too long", func(c *model.Config) { c.Stage.FadeSeconds = 30.1 }},
		{"nonfinite", func(c *model.Config) { c.Stage.FadeSeconds = math.NaN() }},
		{"infinite", func(c *model.Config) { c.Stage.FadeSeconds = math.Inf(1) }},
		{"missing background", func(c *model.Config) { c.Stage.BackgroundCueID = "removed-cue" }},
		{"background removed from cues", func(c *model.Config) { c.Cues = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := config.Clone()
			test.mutate(&bad)
			if err := storage.Save(bad); err == nil {
				t.Fatal("invalid stage settings were saved")
			}
			for name, original := range before {
				current, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !bytes.Equal(current, original) {
					t.Fatalf("rejected stage settings changed %s", name)
				}
			}
		})
	}
}

func TestInvalidStoredSceneFieldsRetainOriginalBytes(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"empty document", func(c map[string]any) { clear(c) }},
		{"missing schema", func(c map[string]any) { delete(c, "schema") }},
		{"missing revision", func(c map[string]any) { delete(c, "playlistRevision") }},
		{"zero fade", func(c map[string]any) { c["stage"].(map[string]any)["fadeSeconds"] = 0 }},
		{"dangling background", func(c map[string]any) { c["stage"].(map[string]any)["backgroundCueId"] = "missing" }},
		{"unknown stage option", func(c map[string]any) { c["stage"].(map[string]any)["script"] = "unexpected" }},
		{"hidden is not boolean", func(c map[string]any) { c["cues"].([]any)[0].(map[string]any)["hidden"] = "true" }},
		{"background is not boolean", func(c map[string]any) { c["cues"].([]any)[0].(map[string]any)["background"] = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			config := model.DefaultConfig()
			config.Cues = []model.Cue{{ID: "one", Label: "Background", Path: filepath.Join(dir, "image.png")}}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document)
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if storage, _, err := Open(dir); err == nil {
				storage.Close()
				t.Fatal("invalid stored scene options were accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("rejected stored scene options changed the original file")
			}
		})
	}
}
