package app

import (
	"context"
	"fmt"
	"testing"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func TestIndependentFadeRoutingPreservesSourcesAndRevisionGuards(t *testing.T) {
	for _, audio := range []bool{false, true} {
		for _, visual := range []bool{false, true} {
			t.Run(fmt.Sprintf("audio=%t/visual=%t", audio, visual), func(t *testing.T) {
				settings := model.StageSettings{AudioFadeEnabled: audio, AudioFadeSeconds: 1.25, VisualFadeEnabled: visual, VisualFadeSeconds: 4.75}
				s, f, _, paths := sceneSetup(t, settings)
				check := func(scene playback.Scene, want model.StageSettings) {
					t.Helper()
					audio, visual := 0.0, 0.0
					if want.AudioFadeEnabled {
						audio = want.AudioFadeSeconds
					}
					if want.VisualFadeEnabled {
						visual = want.VisualFadeSeconds
					}
					if scene.AudioFadeSeconds != audio || scene.VisualFadeSeconds != visual {
						t.Fatalf("fade groups crossed or disabled fade leaked: scene=%+v settings=%+v", scene, want)
					}
				}
				music := scenePlaying(t, s, f, "music")
				check(music, settings)
				if _, err := play(s, "image", "independent-image"); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
				check(f.latest(), settings)
				before := s.Playlist()
				settings.AudioFadeEnabled, settings.VisualFadeEnabled = !audio, !visual
				settings.AudioFadeSeconds, settings.VisualFadeSeconds = 0.4, 2.8
				updated, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: settings})
				if err != nil {
					t.Fatal(err)
				}
				scene := f.latest()
				check(scene, settings)
				if scene.ForegroundID != music.ForegroundID || scene.ImagePath != paths["image"] || updated.Stage != settings {
					t.Fatalf("changing fade controls replaced presentation sources: %+v", scene)
				}
				if _, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: before.Stage}); err == nil {
					t.Fatal("stale settings overwrote independent fades")
				}
				if s.Playlist().Stage != settings || f.latest().Revision != scene.Revision {
					t.Fatal("rejected settings edit changed saved or native fades")
				}
				if _, err := s.Stop(StopRequest{RequestID: "independent-stop"}); err != nil {
					t.Fatal(err)
				}
				check(f.latest(), settings)
				if _, err := s.EmergencyStop(StopRequest{RequestID: "independent-emergency"}); err != nil {
					t.Fatal(err)
				}
				if emergency := f.latest(); !emergency.HardStop || emergency.AudioFadeSeconds != 0 || emergency.VisualFadeSeconds != 0 || emergency.ForegroundID != 0 || emergency.StageEnabled {
					t.Fatalf("emergency did not bypass both fade groups: %+v", emergency)
				}
			})
		}
	}
}
