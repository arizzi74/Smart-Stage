package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"smartstage/internal/model"
)

func TestAtomicStoreBackupRestartAndLock(t *testing.T) {
	dir := t.TempDir()
	s, c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(dir); err == nil {
		t.Fatal("second process lock admitted")
	}
	c.Cues = []model.Cue{{ID: "one", Label: "Café's opening", Path: filepath.Join(dir, "media.wav")}}
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	c.PlaylistRevision++
	c.Cues[0].Label = "Finale"
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "state.json.bak"))
	if err != nil || !strings.Contains(string(backup), "Café's opening") {
		t.Fatalf("backup: %s %v", backup, err)
	}
	s.Close()
	s, c, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if c.Cues[0].Label != "Finale" || c.PlaylistRevision != 2 {
		t.Fatalf("wrong restored state: %+v", c)
	}
}
func TestCorruptionRetainsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	bad := []byte("{not json")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "original retained") {
		t.Fatalf("corruption not surfaced: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(bad) {
		t.Fatal("corrupt source overwritten")
	}
}
func TestInvalidSaveDoesNotReplaceGoodState(t *testing.T) {
	dir := t.TempDir()
	s, c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Save(c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	c.Schema = 900
	if err = s.Save(c); err == nil {
		t.Fatal("invalid schema accepted")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if string(before) != string(after) {
		t.Fatal("failed save changed state")
	}
}
