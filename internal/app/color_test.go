package app

import (
	"testing"

	"smartstage/internal/model"
)

func colorEdits(config model.Config) []CueEdit {
	edits := make([]CueEdit, 0, len(config.Cues))
	for _, cue := range config.Cues {
		edits = append(edits, CueEdit{ID: cue.ID, Label: cue.Label, Path: cue.Path})
	}
	return edits
}

func TestCueColorsPreserveOmittedValuesAndResetExplicitly(t *testing.T) {
	service, _, _ := setup(t, false)
	before := service.Snapshot(true)
	config := service.Playlist()
	red, blue := "#AA1122", "#12abEF"
	edits := colorEdits(config)
	edits[0].Color, edits[1].Color = &red, &blue
	colored, err := service.EditPlaylist(PlaylistEdit{ExpectedRevision: config.PlaylistRevision, Cues: edits})
	if err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{true, false} {
		state := service.Snapshot(admin)
		if state.Cues[0].Color != red || state.Cues[1].Color != blue {
			t.Fatalf("role admin=%v did not receive cue colors: %+v", admin, state.Cues)
		}
		if state.Generation != before.Generation || state.StopEpoch != before.StopEpoch {
			t.Fatal("color-only edit changed playback generation")
		}
	}
	edits = colorEdits(colored)
	edits[0].Label = "Renamed without a color field"
	renamed, err := service.EditPlaylist(PlaylistEdit{ExpectedRevision: colored.PlaylistRevision, Cues: edits})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Cues[0].Color != red || renamed.Cues[1].Color != blue {
		t.Fatal("omitting color discarded an existing selection")
	}
	reset := ""
	edits = colorEdits(renamed)
	edits[0].Color = &reset
	cleared, err := service.EditPlaylist(PlaylistEdit{ExpectedRevision: renamed.PlaylistRevision, Cues: edits})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Cues[0].Color != "" || cleared.Cues[1].Color != blue {
		t.Fatal("explicit default did not reset only the selected cue")
	}
}

func TestInvalidCueColorsCannotMutateTheShow(t *testing.T) {
	service, _, _ := setup(t, false)
	for _, color := range []string{"red", "default", "#123", "#12345678", "#12GG56", "#123456;display:none", " #123456", "#123456\n", "rgb(1,2,3)"} {
		before := service.Playlist()
		edits := colorEdits(before)
		edits[0].Label, edits[0].Color = "Must not save", &color
		if _, err := service.EditPlaylist(PlaylistEdit{ExpectedRevision: before.PlaylistRevision, Cues: edits}); err == nil {
			t.Fatalf("invalid color %q accepted", color)
		}
		after := service.Playlist()
		if after.PlaylistRevision != before.PlaylistRevision || len(after.Cues) != len(before.Cues) || after.Cues[0].Color != before.Cues[0].Color || after.Cues[0].Label != before.Cues[0].Label {
			t.Fatalf("rejected color %q mutated the show", color)
		}
	}
}
