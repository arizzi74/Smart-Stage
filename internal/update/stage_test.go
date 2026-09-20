package update

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

type archiveTestEntry struct {
	name string
	mode os.FileMode
	data string
}

func testArchive(t *testing.T, entries []archiveTestEntry) string {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "update.zip")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestExtractionRejectsUnsafeAndAmbiguousEntries(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []archiveTestEntry
	}{
		{"traversal", []archiveTestEntry{{"Smart Stage.app/../escape", 0600, "bad"}}},
		{"absolute", []archiveTestEntry{{"/Smart Stage.app/file", 0600, "bad"}}},
		{"backslash", []archiveTestEntry{{"Smart Stage.app/..\\escape", 0600, "bad"}}},
		{"symlink", []archiveTestEntry{{"Smart Stage.app/link", os.ModeSymlink | 0777, "/tmp"}}},
		{"pipe", []archiveTestEntry{{"Smart Stage.app/pipe", os.ModeNamedPipe | 0600, ""}}},
		{"duplicate", []archiveTestEntry{{"Smart Stage.app/file", 0600, "one"}, {"Smart Stage.app/file", 0600, "two"}}},
		{"case alias", []archiveTestEntry{{"Smart Stage.app/file", 0600, "one"}, {"Smart Stage.app/FILE", 0600, "two"}}},
		{"device", []archiveTestEntry{{"Smart Stage.app/NUL.exe", 0600, "bad"}}},
		{"alternate stream", []archiveTestEntry{{"Smart Stage.app/file:stream", 0600, "bad"}}},
		{"outside bundle", []archiveTestEntry{{"unrelated", 0600, "bad"}}},
		{"trailing dot", []archiveTestEntry{{"Smart Stage.app/file.", 0600, "bad"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := extractArchive(context.Background(), testArchive(t, test.entries), t.TempDir(), Target{Kind: "bundle"}); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
		})
	}
}
func TestExtractionLimitsAndPortableShape(t *testing.T) {
	entries := make([]archiveTestEntry, maxArchiveEntries+1)
	for i := range entries {
		entries[i] = archiveTestEntry{"smartstage", 0700, ""}
	}
	if err := extractArchive(context.Background(), testArchive(t, entries), t.TempDir(), Target{Kind: "binary", GOOS: "darwin"}); err == nil {
		t.Fatal("accepted too many entries")
	}
	archive := testArchive(t, []archiveTestEntry{{"smartstage.exe", 0700, "one"}, {"extra.txt", 0600, "two"}})
	if err := extractArchive(context.Background(), archive, t.TempDir(), Target{Kind: "binary", GOOS: "windows"}); err == nil {
		t.Fatal("accepted extra portable payload")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := extractArchive(ctx, archive, t.TempDir(), Target{Kind: "binary", GOOS: "windows"}); err != context.Canceled {
		t.Fatalf("cancellation ignored: %v", err)
	}
	// Patch the central directory's declared size without allocating a bomb.
	archive = testArchive(t, []archiveTestEntry{{"smartstage.exe", 0700, "one"}})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(data, []byte{'P', 'K', 1, 2})
	if offset < 0 {
		t.Fatal("missing central directory")
	}
	binary.LittleEndian.PutUint32(data[offset+24:offset+28], uint32(maxEntrySize+1))
	if err := os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(context.Background(), archive, t.TempDir(), Target{Kind: "binary", GOOS: "windows"}); err == nil {
		t.Fatal("accepted oversized declared entry")
	}
}
func TestExtractionKeepsPayloadAndIgnoresBoundedFinderMetadata(t *testing.T) {
	archive := testArchive(t, []archiveTestEntry{{"Smart Stage.app/", os.ModeDir | 0755, ""}, {"Smart Stage.app/Contents/MacOS/smartstage", 0755, "payload"}, {"__MACOSX/._Smart Stage.app", 0600, "metadata"}})
	dir := t.TempDir()
	if err := extractArchive(context.Background(), archive, dir, Target{Kind: "bundle"}); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(dir, "Smart Stage.app", "Contents", "MacOS", "smartstage")
	data, err := os.ReadFile(payload)
	if err != nil || string(data) != "payload" {
		t.Fatalf("payload missing: %s %v", data, err)
	}
	info, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatalf("incorrect executable mode: %s", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(dir, "__MACOSX")); !os.IsNotExist(err) {
		t.Fatal("Finder metadata was extracted")
	}
}
func TestRestartArgsPreserveOptionsAndPinAbsoluteConfig(t *testing.T) {
	got := restartArgs([]string{"--port", "9900", "--config-dir=relative", "--media-root", "/some media", "--update-receipt", "old", "--no-browser"}, "/absolute/config")
	want := []string{"--port", "9900", "--media-root", "/some media", "--no-browser", "--config-dir", "/absolute/config"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restart arguments=%q want=%q", got, want)
	}
}
func TestArchitectureRejectsWrongOrNonExecutablePayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(path, []byte("not an executable"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"darwin", "windows"} {
		if err := checkArchitecture(path, platform, "arm64"); err == nil {
			t.Fatalf("accepted invalid %s executable", platform)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if err := checkArchitecture(self, runtime.GOOS, runtime.GOARCH); err != nil {
			t.Fatal(err)
		}
		other := "arm64"
		if runtime.GOARCH == other {
			other = "amd64"
		}
		if err := checkArchitecture(self, runtime.GOOS, other); err == nil {
			t.Fatal("accepted wrong architecture")
		}
	}
}

func TestInstallLockCoordinatesInstallerAndRetainsOwnership(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, ".smartstage-install.lock")
	if err := acquireInstallLock(lock, "first", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := acquireInstallLock(lock, "second", os.Getpid()); err == nil {
		t.Fatal("a second updater acquired the same installation lock")
	}
	if err := releaseInstallLock(lock, "second"); err == nil {
		t.Fatal("another updater released a lock it did not own")
	}
	if err := transferInstallLock(lock, "first", 42); err != nil {
		t.Fatal(err)
	}
	var owner installLockOwner
	if err := readJSON(filepath.Join(lock, "owner.json"), &owner); err != nil {
		t.Fatal(err)
	}
	if owner.PID != 42 || owner.Nonce != "first" {
		t.Fatalf("helper ownership was not retained: %+v", owner)
	}
	work, err := os.MkdirTemp(root, ".smartstage-update-")
	if err != nil {
		t.Fatal(err)
	}
	prepared := &Prepared{plan: applyPlan{Work: work, Lock: lock, Nonce: "first"}}
	if err := prepared.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("aborted staging retained its lock")
	}
	// The shell installer intentionally has only a pid file; an updater must
	// respect that lock and must never claim ownership of it.
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte("42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := acquireInstallLock(lock, "updater", os.Getpid()); err == nil {
		t.Fatal("updater ignored the shell installer's lock")
	}
	if err := releaseInstallLock(lock, "updater"); err == nil {
		t.Fatal("updater removed the shell installer's lock")
	}
}

func TestMacSystemAliasesDoNotPermitUserInstallationSymlinks(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if got := canonicalSystemPath("/var/tmp/SmartStage"); got != "/private/var/tmp/SmartStage" {
			t.Fatalf("standard system alias was not resolved: %s", got)
		}
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	actual := filepath.Join(root, "actual")
	if err := os.WriteFile(actual, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if err := noSymlinks(link); err == nil {
		t.Fatal("an installation symlink was accepted")
	}
}
