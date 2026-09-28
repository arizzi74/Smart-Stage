package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func TestBackgroundOverrideToggleRestoresDefaultOrBlackWithoutChangingForeground(t *testing.T) {
	for _, saved := range []string{"", "backdrop", "background-video"} {
		for _, stage := range []bool{false, true} {
			t.Run(fmt.Sprintf("default=%s/stage=%t", saved, stage), func(t *testing.T) {
				settings := model.DefaultConfig().Stage
				settings.BackgroundCueID = saved
				s, f, persistence, paths := sceneSetup(t, settings)
				if saved != "" {
					eventually(t, func() bool { return f.latest().BackgroundPath == paths[saved] })
				}
				if state := s.Snapshot(true); state.BackgroundOverrideCueID != "" || state.BackgroundCueID != saved {
					t.Fatalf("saved background became a selected override at startup: %+v", state)
				}
				music := scenePlaying(t, s, f, "music")
				if _, err := play(s, "image", "independent-overlay"); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
				if err := s.Stage(context.Background(), stage); err != nil {
					t.Fatal(err)
				}
				sceneStageCompletion(t, s, f, f.latest())
				s.nativeEvent(playback.Event{Kind: "progress", Generation: music.ForegroundID, TransportRevision: music.TransportRevision, Position: 8.25, Duration: 30, StageEnabled: stage})
				before := s.Snapshot(true)
				persistence.fail = true // Session buttons must not persist settings.
				check := func(override, effective string) {
					t.Helper()
					after, scene := s.Snapshot(true), f.latest()
					if after.BackgroundOverrideCueID != override || after.BackgroundCueID != effective || after.Stage.BackgroundCueID != saved || after.PlaylistRevision != before.PlaylistRevision {
						t.Fatalf("wrong session/default selection: %+v", after)
					}
					if after.ActiveCueID != before.ActiveCueID || after.Generation != before.Generation || after.TransportRevision != before.TransportRevision || after.StopEpoch != before.StopEpoch || after.Elapsed != before.Elapsed || after.ImageCueID != before.ImageCueID || after.StageEnabled != stage {
						t.Fatalf("background toggle changed independent playback: before=%+v after=%+v", before, after)
					}
					if scene.ForegroundID != music.ForegroundID || scene.ImagePath != paths["image"] || scene.StageEnabled != stage || scene.HardStop {
						t.Fatalf("background toggle changed native foreground/image/visibility: %+v", scene)
					}
				}
				first := PlayRequest{RequestID: "select-background-override", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "background-video"}
				previousScene := f.latest().Revision
				if _, err := s.Play(first); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool {
					return f.latest().Revision > previousScene && f.latest().BackgroundPath == paths["background-video"]
				})
				check("background-video", "background-video")
				selectedRevision := f.latest().Revision
				if ack, err := s.Play(first); err != nil || !ack.Duplicate {
					t.Fatalf("selection retry was not idempotent: %+v %v", ack, err)
				}
				check("background-video", "background-video")
				if f.latest().Revision != selectedRevision {
					t.Fatal("retry reloaded or cleared background")
				}
				second := first
				second.RequestID = "clear-background-override"
				if _, err := s.Play(second); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool {
					return f.latest().Revision > selectedRevision && f.latest().BackgroundPath == paths[saved]
				})
				check("", saved)
				clearedRevision := f.latest().Revision
				if ack, err := s.Play(second); err != nil || !ack.Duplicate {
					t.Fatalf("clear retry was not idempotent: %+v %v", ack, err)
				}
				check("", saved)
				if f.latest().Revision != clearedRevision {
					t.Fatal("clear retry selected the override again")
				}
				for _, document := range []any{s.Playlist(), playlistDocument(t, s)} {
					encoded, err := json.Marshal(document)
					if err != nil || strings.Contains(string(encoded), "backgroundOverrideCueId") {
						t.Fatalf("session override entered persisted/exported settings: %s %v", encoded, err)
					}
				}
			})
		}
	}
}

func TestBackgroundOverrideSecondPressCancelsPendingPreparation(t *testing.T) {
	settings := model.DefaultConfig().Stage
	settings.BackgroundCueID = "backdrop"
	s, f, _, paths := sceneSetup(t, settings)
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["backdrop"] })
	music := scenePlaying(t, s, f, "music")
	gate := &sceneInspectGate{path: paths["background-video"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	before := s.Snapshot(true)
	first := PlayRequest{RequestID: "pending-background", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "background-video"}
	if _, err := s.Play(first); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background preparation did not start")
	}
	if ack, err := s.Play(first); err != nil || !ack.Duplicate || s.Snapshot(true).BackgroundOverrideCueID != "background-video" {
		t.Fatalf("pending duplicate cancelled the override: %+v %v", ack, err)
	}
	second := first
	second.RequestID = "cancel-pending-background"
	if _, err := s.Play(second); err != nil {
		t.Fatal(err)
	}
	if state := s.Snapshot(true); state.BackgroundOverrideCueID != "" || state.BackgroundCueID != "backdrop" {
		t.Fatalf("pending override did not clear immediately: %+v", state)
	}
	beforeRelease := f.latest().Revision
	release.Do(func() { close(gate.release) })
	eventually(t, func() bool {
		return f.latest().Revision > beforeRelease && f.latest().BackgroundPath == paths["backdrop"]
	})
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	for _, scene := range f.scenes {
		if scene.BackgroundPath == paths["background-video"] {
			t.Fatalf("cancelled background was applied after second press: %+v", scene)
		}
	}
	if scene := f.scenes[len(f.scenes)-1]; scene.ForegroundID != music.ForegroundID || scene.StageEnabled {
		t.Fatalf("cancelled background changed music or enabled Stage: %+v", scene)
	}
}

func TestBackgroundOverrideCanClearAfterValidationFailure(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.DefaultConfig().Stage)
	if _, err := play(s, "background-video", "select-before-invalid"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
	s.mu.Lock()
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == "background-video" {
			s.config.Cues[i].Cache.Status = "missing"
		}
	}
	s.mu.Unlock()
	if _, err := play(s, "background-video", "clear-invalid-override"); err != nil {
		t.Fatal("unavailable selected override could not be cleared:", err)
	}
	if state := s.Snapshot(true); state.BackgroundOverrideCueID != "" || state.BackgroundCueID != "" || f.latest().BackgroundPath != "" {
		t.Fatalf("invalid override was not cleared to black: %+v", state)
	}
}

func TestBackgroundOverrideReconcilesRelevantEditsAndKeepsUnrelatedEdits(t *testing.T) {
	for _, operation := range []string{"remove override", "replace override source", "unflag override", "change default", "remove default", "rename cue", "remove unrelated cue", "fade settings", "background audio"} {
		t.Run(operation, func(t *testing.T) {
			settings := model.DefaultConfig().Stage
			settings.BackgroundCueID = "backdrop"
			s, f, _, paths := sceneSetup(t, settings)
			if _, err := play(s, "background-video", "edit-override"); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
			before := s.Playlist()
			wantOverride, wantEffective := "", "backdrop"
			var err error
			switch operation {
			case "change default", "fade settings", "background audio":
				settings := before.Stage
				if operation == "change default" {
					settings.BackgroundCueID, wantEffective = "image", "image"
				} else {
					wantOverride, wantEffective = "background-video", "background-video"
					if operation == "fade settings" {
						settings.VisualFadeEnabled, settings.VisualFadeSeconds = true, 2.4
					} else {
						settings.BackgroundAudio = true
					}
				}
				_, err = s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: settings})
			default:
				edits := colorEdits(before)
				filtered := edits[:0]
				for _, edit := range edits {
					if operation == "remove override" && edit.ID == "background-video" || operation == "remove default" && edit.ID == "backdrop" || operation == "remove unrelated cue" && edit.ID == "next" {
						continue
					}
					if edit.ID == "background-video" {
						switch operation {
						case "replace override source":
							edit.Path = paths["image"]
						case "unflag override":
							flag := false
							edit.Background = &flag
						case "rename cue":
							edit.Label = "Renamed background"
						}
					}
					filtered = append(filtered, edit)
				}
				if operation == "remove default" {
					wantEffective = ""
				}
				if operation == "rename cue" || operation == "remove unrelated cue" {
					wantOverride, wantEffective = "background-video", "background-video"
				}
				_, err = s.EditPlaylist(PlaylistEdit{ExpectedRevision: before.PlaylistRevision, Cues: filtered})
			}
			if err != nil {
				t.Fatal(err)
			}
			if state := s.Snapshot(true); state.BackgroundOverrideCueID != wantOverride || state.BackgroundCueID != wantEffective {
				t.Fatalf("edit reconciled session background incorrectly: %+v", state)
			}
			eventually(t, func() bool { return f.latest().BackgroundPath == paths[wantEffective] })
		})
	}
}

func TestPlaylistLoadClearsBackgroundOverrideAndRemapsDefault(t *testing.T) {
	settings := model.DefaultConfig().Stage
	settings.BackgroundCueID = "backdrop"
	s, f, _, paths := sceneSetup(t, settings)
	if _, err := play(s, "background-video", "override-before-file-load"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
	file := playlistDocument(t, s)
	loaded, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: file})
	if err != nil {
		t.Fatal(err)
	}
	if state := s.Snapshot(true); state.BackgroundOverrideCueID != "" || state.BackgroundCueID != loaded.Stage.BackgroundCueID || state.BackgroundCueID == "backdrop" {
		t.Fatalf("playlist load retained override or old default identity: %+v", state)
	}
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["backdrop"] })
}

func TestBackgroundOverrideNewerIntentSurvivesUnrelatedSettingsSave(t *testing.T) {
	for _, previouslySelected := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously-selected=%t", previouslySelected), func(t *testing.T) {
			settings := model.DefaultConfig().Stage
			settings.BackgroundCueID = "backdrop"
			s, f, persistence, paths := sceneSetup(t, settings)
			eventually(t, func() bool { return f.latest().BackgroundPath == paths["backdrop"] })
			if previouslySelected {
				if _, err := play(s, "background-video", "override-before-save"); err != nil {
					t.Fatal(err)
				}
			}
			before := s.Playlist()
			settings.AudioFadeEnabled = true
			persistence.entered, persistence.release = make(chan struct{}), make(chan struct{})
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(persistence.release) }) })
			result := make(chan error, 1)
			go func() {
				_, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: settings})
				result <- err
			}()
			select {
			case <-persistence.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("settings save did not start")
			}
			if _, err := play(s, "background-video", "override-during-save"); err != nil {
				t.Fatal(err)
			}
			release.Do(func() { close(persistence.release) })
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			wantOverride, wantEffective := "background-video", "background-video"
			if previouslySelected {
				wantOverride, wantEffective = "", "backdrop"
			}
			if state := s.Snapshot(true); state.BackgroundOverrideCueID != wantOverride || state.BackgroundCueID != wantEffective || !state.Stage.AudioFadeEnabled {
				t.Fatalf("completed unrelated save overwrote newer session intent: %+v", state)
			}
			eventually(t, func() bool { return f.latest().BackgroundPath == paths[wantEffective] })
		})
	}
}
