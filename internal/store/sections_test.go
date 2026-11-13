package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"smartstage/internal/model"
)

func TestLegacySectionMigrationIsStableAndPersistedOnSave(t *testing.T) {
	dir := t.TempDir()
	legacy := model.DefaultConfig()
	legacy.Cues = []model.Cue{{ID: "one", Label: "Opening", Path: filepath.Join(dir, "opening.wav")}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	if err = json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	delete(old, "sections")
	for _, cue := range old["cues"].([]any) {
		delete(cue.(map[string]any), "sectionId")
	}
	data, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, config, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(config.Sections, model.DefaultSections()) || config.Cues[0].SectionID != "main" {
			t.Fatalf("unstable migration: %+v", config)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	original, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("loading legacy sections rewrote the original")
	}
	s, config, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	config.Sections[0].Title = "Atto uno"
	config.Sections[0].Collapsed = true
	if err = s.Save(config); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, restored, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !reflect.DeepEqual(restored.Sections, config.Sections) || restored.Cues[0].SectionID != "main" {
		t.Fatal("saved section title/collapse/reference was not restored")
	}
}

func TestInvalidSectionSaveAndOpenRetainOriginalBytes(t *testing.T) {
	dir := t.TempDir()
	s, config, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	config.Cues = []model.Cue{{ID: "one", Label: "One", Path: filepath.Join(dir, "one.wav"), SectionID: "main"}}
	if err = s.Save(config); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	before, _ := os.ReadFile(path)
	bad := config.Clone()
	bad.Sections = []model.Section{}
	if s.Save(bad) == nil {
		t.Fatal("empty section array saved")
	}
	bad = config.Clone()
	bad.Cues[0].SectionID = "deleted"
	if s.Save(bad) == nil {
		t.Fatal("dangling section saved")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid section save replaced good state")
	}
	s.Close()
	data, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Open(dir); err == nil {
		t.Fatal("invalid stored reference loaded")
	}
	retained, _ := os.ReadFile(path)
	if !bytes.Equal(retained, data) {
		t.Fatal("corrupt section state was overwritten")
	}
}
