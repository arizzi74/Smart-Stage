package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func sectionPlaylistEdit(s *Service, sections *[]model.Section, moves map[string]string) PlaylistEdit {
	config := s.Playlist()
	edit := PlaylistEdit{ExpectedRevision: config.PlaylistRevision, Sections: sections, Cues: make([]CueEdit, 0, len(config.Cues))}
	for _, cue := range config.Cues {
		item := CueEdit{ID: cue.ID, Label: cue.Label, Path: cue.Path}
		if section, ok := moves[cue.ID]; ok {
			item.SectionID = &section
		}
		edit.Cues = append(edit.Cues, item)
	}
	return edit
}

func TestSectionMovesTitlesAndCollapseDoNotTouchPlayingLayersOrInspection(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	transportEvent(s, video, "progress", 5)
	setAudioFlags(s, "music", false, true)
	music := startLayer(t, s, f, "music")
	s.nativeEvent(playback.Event{Kind: "progress", Generation: music.ID, TransportRevision: music.TransportRevision, SceneRevision: f.latest().Revision, Position: 8, Duration: 30, StageEnabled: f.latest().StageEnabled})
	persistence := &playlistFileStore{}
	s.store = persistence
	beforeScene, beforeState := f.latest(), s.Snapshot(true)
	f.sceneMu.Lock()
	beforeInspections := f.inspectionCount
	f.sceneMu.Unlock()
	sections := []model.Section{{ID: "main", Title: "Opening", Collapsed: true}, {ID: "client-section-123", Title: "Act two"}}
	result, err := s.EditPlaylist(sectionPlaylistEdit(s, &sections, map[string]string{"video": "client-section-123"}))
	if err != nil {
		t.Fatal(err)
	}
	// Give a wrongly scheduled validation loop time to reveal itself. A section
	// edit must preserve native inspection as well as decoder/transport identity.
	time.Sleep(20 * time.Millisecond)
	f.sceneMu.Lock()
	afterInspections := f.inspectionCount
	f.sceneMu.Unlock()
	if afterInspections != beforeInspections || !reflect.DeepEqual(f.latest(), beforeScene) {
		t.Fatal("section metadata re-inspected or reapplied native playback")
	}
	state := s.Snapshot(true)
	if state.Generation != beforeState.Generation || state.TransportRevision != beforeState.TransportRevision || state.StopEpoch != beforeState.StopEpoch || state.Elapsed != beforeState.Elapsed || !cueView(t, s, "video").Selected || cueView(t, s, "video").SectionID != "client-section-123" || cueView(t, s, "music").SectionID != "main" {
		t.Fatalf("move changed a playing source: %+v", state)
	}
	if !reflect.DeepEqual(state.Sections, sections) || !reflect.DeepEqual(result.Sections, sections) || result.PlaylistRevision != beforeState.PlaylistRevision+1 {
		t.Fatal("section metadata or revision missing from state/config")
	}
	persistence.mu.Lock()
	if len(persistence.saved) != 1 || !reflect.DeepEqual(persistence.saved[0].Sections, sections) {
		t.Fatal("section metadata not saved atomically")
	}
	persistence.mu.Unlock()
	// Independent snapshots cannot mutate the authoritative section metadata.
	state.Sections[0].Collapsed = false
	result.Sections[0].Title = "Mutated clone"
	file, err := s.ExportPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	file.Sections[0].Title = "Mutated export"
	if current := s.Playlist(); !current.Sections[0].Collapsed || current.Sections[0].Title != "Opening" {
		t.Fatal("section slices shared across state/playlist/export")
	}
	// Older clients omit section fields. Renaming/reordering still preserves all
	// memberships and saved collapse flags rather than resetting them to Main.
	preserved, err := s.EditPlaylist(sectionPlaylistEdit(s, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preserved.Sections, sections) || cueView(t, s, "video").SectionID != "client-section-123" || !reflect.DeepEqual(f.latest(), beforeScene) {
		t.Fatal("omitted section fields reset membership or playback")
	}
}

func TestSectionDeletionRequiresAtomicMovesAndNewCuesUseFirstSection(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	before := s.Playlist()
	sections := []model.Section{{ID: "second", Title: "Second"}}
	if _, err := s.EditPlaylist(sectionPlaylistEdit(s, &sections, nil)); err == nil {
		t.Fatal("deleting a section left dangling cue references")
	}
	if !reflect.DeepEqual(s.Playlist(), before) {
		t.Fatal("rejected section delete changed saved show")
	}
	moves := map[string]string{}
	for _, cue := range before.Cues {
		moves[cue.ID] = "second"
	}
	applied, err := s.EditPlaylist(sectionPlaylistEdit(s, &sections, moves))
	if err != nil {
		t.Fatal(err)
	}
	for _, cue := range applied.Cues {
		if cue.SectionID != "second" {
			t.Fatal("atomic delete did not move every cue")
		}
	}
	oldScene := f.latest()
	edit := sectionPlaylistEdit(s, nil, nil)
	edit.Cues = append(edit.Cues, CueEdit{Label: "Added to first section", Path: paths["next"]})
	result, err := s.EditPlaylist(edit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cues[len(result.Cues)-1].SectionID != "second" || len(result.Sections) != 1 || result.Sections[0].ID != "second" {
		t.Fatal("new cue did not use first remaining section")
	}
	if !reflect.DeepEqual(f.latest(), oldScene) {
		t.Fatal("adding an inactive cue interrupted current playback")
	}
}

func TestInvalidSectionTransactionsAreRejectedBeforePersistence(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	persistence := &playlistFileStore{}
	s.store = persistence
	before, scene := s.Playlist(), f.latest()
	cases := map[string][]model.Section{
		"empty": {}, "duplicate IDs": {{ID: "main", Title: "One"}, {ID: "main", Title: "Two"}},
		"duplicate titles": {{ID: "main", Title: "Main"}, {ID: "other", Title: " MAIN "}},
		"invalid id":       {{ID: "main?", Title: "Main"}}, "bad title": {{ID: "main", Title: "bad\ntitle"}},
		"too many": make([]model.Section, model.MaxSections+1),
	}
	for name, sections := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.EditPlaylist(sectionPlaylistEdit(s, &sections, nil)); err == nil {
				t.Fatal("invalid sections accepted")
			}
		})
	}
	if _, err := s.EditPlaylist(sectionPlaylistEdit(s, nil, map[string]string{"music": "missing"})); err == nil {
		t.Fatal("dangling section move accepted")
	}
	if !reflect.DeepEqual(s.Playlist(), before) || !reflect.DeepEqual(f.latest(), scene) {
		t.Fatal("invalid section edit changed configuration/native scene")
	}
	persistence.mu.Lock()
	defer persistence.mu.Unlock()
	if len(persistence.saved) != 0 {
		t.Fatal("invalid section edit reached persistence")
	}
}

func TestSectionSaveFailureAndRevisionConflictPreserveMetadata(t *testing.T) {
	s, _, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	persistence := &playlistFileStore{fail: true}
	s.store = persistence
	before := s.Playlist()
	sections := []model.Section{{ID: "main", Title: "Renamed", Collapsed: true}}
	edit := sectionPlaylistEdit(s, &sections, nil)
	if _, err := s.EditPlaylist(edit); err == nil {
		t.Fatal("failed section save accepted")
	}
	if !reflect.DeepEqual(s.Playlist(), before) {
		t.Fatal("failed save changed section metadata")
	}
	persistence.mu.Lock()
	persistence.fail = false
	persistence.mu.Unlock()
	if _, err := s.EditPlaylist(edit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditPlaylist(edit); err == nil {
		t.Fatal("stale section revision accepted")
	}
}

func TestSectionSaveBlocksNewPlayButSTOPNeverWaitsOrRearmsSources(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	persistence := &playlistFileStore{entered: make(chan struct{}), release: make(chan struct{})}
	s.store = persistence
	var release sync.Once
	unblock := func() { release.Do(func() { close(persistence.release) }) }
	t.Cleanup(unblock)
	sections := []model.Section{{ID: "main", Title: "Main", Collapsed: true}}
	done := make(chan error, 1)
	go func() { _, err := s.EditPlaylist(sectionPlaylistEdit(s, &sections, nil)); done <- err }()
	select {
	case <-persistence.entered:
	case <-time.After(time.Second):
		t.Fatal("section save did not start")
	}
	if _, err := play(s, "next", "play-during-section-save"); err == nil {
		t.Fatal("new PLAY accepted before section transaction saved")
	}
	stopped := make(chan error, 1)
	go func() { _, err := s.Stop(StopRequest{RequestID: "stop-during-section-save"}); stopped <- err }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("section save blocked STOP")
	}
	stopScene := f.latest()
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stopScene, f.latest()) || len(f.latest().AudioLayers) != 0 || f.latest().ForegroundID != 0 || !s.Playlist().Sections[0].Collapsed {
		t.Fatal("completed section save rearmed stopped playback or lost metadata")
	}
}

func TestSectionMetadataRoundTripsFilesAndRemapsAllReferences(t *testing.T) {
	s, _, _, _ := sceneSetup(t, model.StageSettings{BackgroundCueID: "background-video", AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	file, err := s.ExportPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	file.Sections = []model.Section{{ID: "main", Title: "Main", Collapsed: true}, {ID: "act-two", Title: "Atto due"}}
	file.Cues[0].SectionID = "act-two"
	file.Cues[0].KeepVideo, file.Cues[0].MixAudio = true, true
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlaylist(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(file, decoded) {
		t.Fatal("section metadata changed across portable encoding")
	}
	loaded, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: decoded})
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string]string{}
	for i, section := range loaded.Sections {
		if section.ID == decoded.Sections[i].ID || section.Title != decoded.Sections[i].Title || section.Collapsed != decoded.Sections[i].Collapsed {
			t.Fatal("load failed to remap identities while preserving section metadata")
		}
		sections[decoded.Sections[i].ID] = section.ID
	}
	for i, cue := range loaded.Cues {
		if cue.SectionID != sections[decoded.Cues[i].SectionID] || cue.ID == decoded.Cues[i].ID {
			t.Fatal("load left an old/dangling section or cue identity")
		}
	}
	if loaded.Stage.BackgroundCueID != loaded.Cues[4].ID || !loaded.Cues[0].KeepVideo || !loaded.Cues[0].MixAudio {
		t.Fatal("section loading lost background/audio flags")
	}
	// Legacy portable shows contain neither field. Their deterministic migration
	// exports explicit membership, and then loading creates fresh show identities.
	var legacy map[string]any
	if err = json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "sections")
	for _, cue := range legacy["cues"].([]any) {
		delete(cue.(map[string]any), "sectionId")
	}
	data, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	old, err := DecodePlaylist(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Sections, model.DefaultSections()) {
		t.Fatal("legacy portable show did not gain Main")
	}
	for _, cue := range old.Cues {
		if cue.SectionID != "main" {
			t.Fatal("legacy cue not assigned to Main")
		}
	}
}

func TestDecodePlaylistRejectsInvalidSectionMetadataAndReferences(t *testing.T) {
	s, _, _ := setup(t, false)
	valid := playlistDocument(t, s)
	for name, change := range map[string]func(*PlaylistFile){
		"empty sections":     func(p *PlaylistFile) { p.Sections = []model.Section{} },
		"duplicate sections": func(p *PlaylistFile) { p.Sections = append(p.Sections, p.Sections[0]) },
		"dangling reference": func(p *PlaylistFile) { p.Cues[0].SectionID = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			p := valid.clone()
			change(&p)
			data, _ := json.Marshal(p)
			if _, err := DecodePlaylist(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid portable sections accepted")
			}
		})
	}
	// Existing bounded-decoder and unknown-field checks still cover sections.
	data, _ := json.Marshal(valid)
	malformed := strings.Replace(string(data), `"collapsed":false`, `"collapsed":"yes"`, 1)
	if _, err := DecodePlaylist(strings.NewReader(malformed)); err == nil {
		t.Fatal("non-boolean collapse accepted")
	}
}

func TestConcurrentSectionEditsAcceptOneRevision(t *testing.T) {
	s, _, _ := setup(t, false)
	base := sectionPlaylistEdit(s, nil, nil)
	var wg sync.WaitGroup
	successes := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sections := []model.Section{{ID: "main", Title: fmt.Sprintf("Main %d", i), Collapsed: i%2 == 0}}
			edit := base
			edit.Sections = &sections
			_, err := s.EditPlaylist(edit)
			successes <- err == nil
		}(i)
	}
	wg.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}
	if count != 1 || s.Playlist().PlaylistRevision != base.ExpectedRevision+1 {
		t.Fatal("concurrent section edits lost the revision boundary")
	}
}
