package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"smartstage/internal/model"
)

func TestCueColorPersistsAcrossRestartAndInvalidSaveRetainsBytes(t *testing.T) {
	dir := t.TempDir()
	storage, config, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	config.Cues = []model.Cue{
		{ID: "colored", Label: "Colored cue", Path: filepath.Join(dir, "song.wav"), Color: "#Ab12EF"},
		{ID: "default", Label: "Default cue", Path: filepath.Join(dir, "other.wav")},
	}
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	config.Cues[0].Color = "url(https://example.com/)"
	if err := storage.Save(config); err == nil {
		t.Fatal("invalid persisted color accepted")
	}
	after, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil || string(after) != string(original) {
		t.Fatal("invalid color changed saved show bytes")
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, restored, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(restored.Cues) != 2 || restored.Cues[0].Color != "#Ab12EF" || restored.Cues[1].Color != "" {
		t.Fatalf("colors changed across restart: %+v", restored.Cues)
	}
}

func TestInvalidStoredColorIsRejectedWithoutChangingOriginal(t *testing.T) {
	dir := t.TempDir()
	config := model.DefaultConfig()
	config.Cues = []model.Cue{{ID: "one", Label: "Cue", Path: filepath.Join(dir, "tone.wav"), Color: "#ffffff;background:red"}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if storage, _, err := Open(dir); err == nil {
		storage.Close()
		t.Fatal("invalid color loaded from disk")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(data) {
		t.Fatal("rejected stored color overwrote the saved show")
	}
}
