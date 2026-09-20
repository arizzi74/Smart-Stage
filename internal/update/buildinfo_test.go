package update

import (
	"context"
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestTaggedTrimpathBuildHasValidReleaseIdentityWithoutLinkerFlags(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "smartstage"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module smartstage\n\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package main
import "fmt"
var version = "dev"
var commit = "unknown"
func main() { fmt.Println(version, commit) }
`
	if err := os.WriteFile(filepath.Join(repo, "cmd", "smartstage", "main.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOWORK=off", "GOFLAGS=", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %q: %v\n%s", name, args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	command("git", "init", "-q")
	command("git", "add", ".")
	command("git", "-c", "user.name=Smart Stage Test", "-c", "user.email=smartstage-test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "Tagged release fixture")
	command("git", "-c", "tag.gpgsign=false", "tag", "v1.2.3")
	revision := command("git", "rev-parse", "HEAD")
	binary := filepath.Join(root, "smartstage")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	// Match scripts/build.sh. In particular -trimpath intentionally causes Go
	// to omit -ldflags, even though main.version is set by the linker correctly.
	command("go", "build", "-trimpath", "-buildvcs=true", "-ldflags", "-s -w -X main.version=v1.2.3 -X main.commit="+revision, "-o", binary, "./cmd/smartstage")
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range info.Settings {
		if setting.Key == "-ldflags" {
			t.Fatal("fixture unexpectedly retained linker flags; expected production trimpath behavior")
		}
	}
	target := Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if err := validateGoBuild(info, target, "v1.2.3"); err != nil {
		t.Fatalf("actual clean tagged release rejected: %v\n%s", err, info)
	}
	if err := validateGoBuild(info, target, "v1.2.4"); err == nil {
		t.Fatal("actual executable was accepted as a different release")
	}
	// A dirty rebuild must never be treated as the official clean release.
	if err := os.WriteFile(filepath.Join(repo, "cmd", "smartstage", "main.go"), []byte(source+"\n// modified after tag\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command("go", "build", "-trimpath", "-buildvcs=true", "-ldflags", "-s -w -X main.version=v1.2.3 -X main.commit="+revision, "-o", binary, "./cmd/smartstage")
	info, err = buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoBuild(info, target, "v1.2.3"); err == nil {
		t.Fatal("dirty tagged source was accepted")
	}
}

func TestReleaseBuildIdentityRejectsMissingOrMismatchedMetadata(t *testing.T) {
	good := func() *buildinfo.BuildInfo {
		return &buildinfo.BuildInfo{
			Path: "smartstage/cmd/smartstage", Main: debug.Module{Path: "smartstage", Version: "v1.2.3"},
			Settings: []debug.BuildSetting{{Key: "GOOS", Value: "darwin"}, {Key: "GOARCH", Value: "arm64"}, {Key: "CGO_ENABLED", Value: "1"}, {Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: strings.Repeat("a", 40)}, {Key: "vcs.modified", Value: "false"}},
		}
	}
	set := func(info *buildinfo.BuildInfo, key, value string) {
		for i := range info.Settings {
			if info.Settings[i].Key == key {
				info.Settings[i].Value = value
				return
			}
		}
		info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
	}
	for _, test := range []struct {
		name   string
		change func(*buildinfo.BuildInfo)
	}{
		{"wrong package", func(info *buildinfo.BuildInfo) { info.Path = "smartstage/cmd/native-harness" }},
		{"wrong module", func(info *buildinfo.BuildInfo) { info.Main.Path = "another-app" }},
		{"replacement module", func(info *buildinfo.BuildInfo) { info.Main.Replace = &debug.Module{Path: "other"} }},
		{"wrong version", func(info *buildinfo.BuildInfo) { info.Main.Version = "v1.2.4" }},
		{"development module", func(info *buildinfo.BuildInfo) { info.Main.Version = "(devel)" }},
		{"missing version", func(info *buildinfo.BuildInfo) { info.Main.Version = "" }},
		{"dirty source", func(info *buildinfo.BuildInfo) { set(info, "vcs.modified", "true") }},
		{"missing dirty state", func(info *buildinfo.BuildInfo) { set(info, "vcs.modified", "") }},
		{"missing revision", func(info *buildinfo.BuildInfo) { set(info, "vcs.revision", "") }},
		{"invalid revision", func(info *buildinfo.BuildInfo) { set(info, "vcs.revision", strings.Repeat("z", 40)) }},
		{"wrong vcs", func(info *buildinfo.BuildInfo) { set(info, "vcs", "hg") }},
		{"wrong OS", func(info *buildinfo.BuildInfo) { set(info, "GOOS", "windows") }},
		{"wrong architecture", func(info *buildinfo.BuildInfo) { set(info, "GOARCH", "amd64") }},
		{"no native backend", func(info *buildinfo.BuildInfo) { set(info, "CGO_ENABLED", "0") }},
		{"duplicate metadata", func(info *buildinfo.BuildInfo) {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: "GOOS", Value: "darwin"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := good()
			test.change(info)
			if err := validateGoBuild(info, Target{GOOS: "darwin", GOARCH: "arm64"}, "v1.2.3"); err == nil {
				t.Fatal("invalid release metadata was accepted")
			}
		})
	}
}
