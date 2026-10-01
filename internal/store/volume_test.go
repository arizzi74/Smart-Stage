package store

import (
	"os"
	"path/filepath"
	"testing"

	"smartstage/internal/model"
)

func TestVolumesPersistAndLegacyDefaultsRemainCentered(t *testing.T) {
	dir := t.TempDir()
	storage, config, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mute, loud := 0.0, 1.0
	config.Cues = []model.Cue{
		{ID: "mute", Label: "Mute", Path: filepath.Join(dir, "mute.wav"), Volume: &mute},
		{ID: "loud", Label: "Loud", Path: filepath.Join(dir, "loud.wav"), Volume: &loud},
		{ID: "old", Label: "Old", Path: filepath.Join(dir, "old.wav")},
	}
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	invalid := 1.01
	config.Cues[0].Volume = &invalid
	if storage.Save(config) == nil {
		t.Fatal("out-of-range saved volume accepted")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("invalid volume overwrote saved state")
	}
	storage.Close()
	reopened, restored, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if restored.Cues[0].PlaybackVolume() != 0 || restored.Cues[1].PlaybackVolume() != 1 || restored.Cues[2].PlaybackVolume() != .5 {
		t.Fatalf("saved/default volumes changed on launch: %+v", restored.Cues)
	}
}
