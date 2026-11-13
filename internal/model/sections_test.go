package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestSectionsMigrateLegacyShowsAndCloneMetadata(t *testing.T) {
	legacy := Config{Cues: []Cue{{ID: "one"}}}
	legacy.NormalizeSections()
	if len(legacy.Sections) != 1 || legacy.Sections[0] != (Section{ID: "main", Title: "Main"}) || legacy.Cues[0].SectionID != "main" {
		t.Fatalf("legacy sections not normalized: %+v", legacy)
	}
	if err := legacy.ValidateSectionReferences(); err != nil {
		t.Fatal(err)
	}
	cloned := legacy.Clone()
	cloned.Sections[0].Collapsed = true
	cloned.Sections[0].Title = "Changed"
	cloned.Cues[0].SectionID = "other"
	if legacy.Sections[0].Collapsed || legacy.Sections[0].Title != "Main" || legacy.Cues[0].SectionID != "main" {
		t.Fatal("section/cue clone shares saved metadata")
	}
	explicitEmpty := Config{Sections: []Section{}}
	explicitEmpty.NormalizeSections()
	if ValidateSections(explicitEmpty.Sections) == nil {
		t.Fatal("explicit empty sections silently became a legacy default")
	}
	custom := Config{Sections: []Section{{ID: "act-two", Title: "Act two", Collapsed: true}}, Cues: []Cue{{ID: "new"}}}
	custom.NormalizeSections()
	if custom.Cues[0].SectionID != "act-two" || !custom.Sections[0].Collapsed {
		t.Fatal("new cue did not use the first configured section")
	}
}

func TestSectionValidationBoundsTitlesIdentitiesAndReferences(t *testing.T) {
	cases := map[string][]Section{
		"empty": {}, "empty id": {{Title: "Main"}}, "invalid id": {{ID: "bad/id", Title: "Main"}},
		"long id": {{ID: strings.Repeat("x", 129), Title: "Main"}}, "empty title": {{ID: "main", Title: "  "}},
		"long title": {{ID: "main", Title: strings.Repeat("x", 129)}}, "newline": {{ID: "main", Title: "two\nlines"}},
		"nul": {{ID: "main", Title: "bad\x00title"}}, "bad utf8": {{ID: "main", Title: string([]byte{255})}},
		"duplicate id":    {{ID: "main", Title: "One"}, {ID: "main", Title: "Two"}},
		"duplicate title": {{ID: "main", Title: "Opening"}, {ID: "second", Title: " opening "}},
		"too many":        make([]Section, MaxSections+1),
	}
	for name, sections := range cases {
		t.Run(name, func(t *testing.T) {
			if ValidateSections(sections) == nil {
				t.Fatal("invalid section metadata accepted")
			}
		})
	}
	valid := make([]Section, MaxSections)
	for i := range valid {
		valid[i] = Section{ID: fmt.Sprintf("section-%d", i), Title: fmt.Sprintf("Parte %d", i)}
	}
	if err := ValidateSections(valid); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.Cues = []Cue{{ID: "one", SectionID: "missing"}}
	if config.ValidateSectionReferences() == nil {
		t.Fatal("dangling cue section admitted")
	}
}
