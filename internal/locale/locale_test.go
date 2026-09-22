package locale

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestPreferenceSurvivesRestartAndSystemChanges(t *testing.T) {
	dir := t.TempDir()
	var applied []string
	m, err := New(dir, "it", func(language string) { applied = append(applied, language) })
	if err != nil || m.Snapshot() != (Snapshot{"system", "it", "it"}) {
		t.Fatalf("default: %+v %v", m.Snapshot(), err)
	}
	for _, mode := range []string{"en", "it", "en"} {
		if err := m.Set(mode); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(applied, []string{"it", "en", "it", "en"}) {
		t.Fatal(applied)
	}
	restarted, err := New(dir, "it", nil)
	if err != nil || restarted.Snapshot() != (Snapshot{"en", "en", "it"}) {
		t.Fatalf("restart: %+v %v", restarted.Snapshot(), err)
	}
	if err := restarted.Set("system"); err != nil {
		t.Fatal(err)
	}
	restarted, err = New(dir, "en", nil)
	if err != nil || restarted.Snapshot() != (Snapshot{"system", "en", "en"}) {
		t.Fatalf("new system: %+v %v", restarted.Snapshot(), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Fatal("language must not modify show file")
	}
}

func TestInvalidOrFailedSaveDoesNotChangeLanguage(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	m, _ := New(dir, "it", func(string) { calls++ })
	for _, mode := range []string{"", "IT", "fr", "it-IT", "../it", "auto"} {
		if err := m.Set(mode); err != ErrMode {
			t.Fatalf("accepted %q: %v", mode, err)
		}
	}
	// A directory at the destination causes a real replacement failure on all OSes.
	if err := os.Mkdir(filepath.Join(dir, "language.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Set("en"); err == nil {
		t.Fatal("expected save failure")
	}
	if m.Snapshot().Effective != "it" || calls != 1 {
		t.Fatal("failed save applied")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".language-*.tmp"))
	if len(leftovers) != 0 {
		t.Fatal(leftovers)
	}
}

func TestCorruptPreferencesFallbackWithoutOverwriting(t *testing.T) {
	for _, content := range []string{`null`, `{}`, `{"mode":"fr"}`, `{"mode":"it","extra":true}`, `{"mode":"en"} {}`, `broken`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "language.json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		m, err := New(dir, "it", nil)
		if err == nil || m.Snapshot() != (Snapshot{"system", "it", "it"}) {
			t.Fatalf("%s: %+v %v", content, m.Snapshot(), err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != content {
			t.Fatal("corrupt preference overwritten")
		}
	}
}

func TestConcurrentReadAndChanges(t *testing.T) {
	m, _ := New(t.TempDir(), "unsupported", nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				if err := m.Set([]string{"it", "en", "system"}[(i+j)%3]); err != nil {
					t.Error(err)
				}
				state := m.Snapshot()
				if !valid(state.Mode) || state.System != "en" {
					t.Error(state)
				}
			}
		}(i)
	}
	wg.Wait()
	reloaded, err := New(m.dir, "en", nil)
	if err != nil || reloaded.Snapshot() != m.Snapshot() {
		t.Fatalf("concurrent persistence: %v", err)
	}
}
