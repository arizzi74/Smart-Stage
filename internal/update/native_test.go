package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test executable plays three real processes: the old app, its copied
// helper, and the candidate. Production helper code performs the handoff; only
// the tiny candidate startup/receipt behavior is supplied by this test binary.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "--smartstage-apply-update" {
		if err := RunHelper(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) > 2 && os.Args[1] == "--updater-test-parent" {
		var p applyPlan
		if err := readJSON(os.Args[2], &p); err != nil {
			panic(err)
		}
		p.ParentPID = os.Getpid()
		if err := writeJSON(os.Args[2], p); err != nil {
			panic(err)
		}
		if err := (&Prepared{plan: p}).Launch(); err != nil {
			panic(err)
		}
		// Leave the original running briefly after READY. The helper must not
		// replace its image until this process has really exited.
		time.Sleep(150 * time.Millisecond)
		if _, err := os.Stat(p.Backup); !os.IsNotExist(err) {
			panic("helper replaced the executable while its parent was still running")
		}
		os.Exit(0)
	}
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "--updater-test-candidate=") {
		mode := strings.TrimPrefix(os.Args[1], "--updater-test-candidate=")
		var receipt, config string
		for i := 2; i+1 < len(os.Args); i++ {
			if os.Args[i] == "--update-receipt" {
				receipt = os.Args[i+1]
			}
			if os.Args[i] == "--config-dir" {
				config = os.Args[i+1]
			}
		}
		if receipt == "" {
			_ = os.WriteFile(filepath.Join(config, "previous-restarted"), []byte(strconv.Itoa(os.Getpid())), 0600)
			os.Exit(0)
		}
		if err := RegisterStartup(receipt, "v0.0.0-preview.1"); err != nil {
			panic(err)
		}
		_ = os.WriteFile(filepath.Join(config, "candidate-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
		if mode == "fail" {
			os.Exit(47)
		}
		if err := ConfirmStartup(receipt, "v0.0.0-preview.1"); err != nil {
			panic(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(config, "finish-test-candidate")); err == nil {
				os.Exit(0)
			}
			time.Sleep(50 * time.Millisecond)
		}
		os.Exit(48)
	}
	os.Exit(m.Run())
}

func TestNativeHelperReplacementAndStartupRollback(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess replacement test")
	}
	t.Setenv("SMARTSTAGE_SKIP_FIREWALL", "1")
	for _, mode := range []string{"ready", "fail"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(root, "config")
			if err := os.Mkdir(config, 0700); err != nil {
				t.Fatal(err)
			}
			work, err := os.MkdirTemp(root, ".smartstage-update-")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(work, "unpacked"), 0700); err != nil {
				t.Fatal(err)
			}
			name := "smartstage"
			helperName := "update-helper"
			if runtime.GOOS == "windows" {
				name += ".exe"
				helperName += ".exe"
			}
			target := Target{Path: filepath.Join(root, name), Kind: "binary", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{target.Path, filepath.Join(work, "unpacked", name), filepath.Join(work, helperName)} {
				if err := copyFile(context.Background(), self, path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before, err := snapshotFileIdentity(target.Path)
			if err != nil {
				t.Fatal(err)
			}
			hash, err := hashFile(context.Background(), target.Path)
			if err != nil {
				t.Fatal(err)
			}
			p := applyPlan{Target: target, Version: "v0.0.0-preview.1", Work: work, Payload: filepath.Join(work, "unpacked", name), Backup: filepath.Join(work, "previous"), Helper: filepath.Join(work, helperName), ConfigDir: config, Cwd: root, Args: []string{"--updater-test-candidate=" + mode, "--config-dir", config}, ParentPID: os.Getpid(), OldHash: hash, Nonce: strings.Repeat("a", 64)}
			p.Lock = filepath.Join(root, ".smartstage-install.lock")
			if err := acquireInstallLock(p.Lock, p.Nonce, os.Getpid()); err != nil {
				t.Fatal(err)
			}
			planPath := filepath.Join(work, "plan.json")
			if err := writeJSON(planPath, p); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			parent := exec.CommandContext(ctx, target.Path, "--updater-test-parent", planPath)
			if output, err := parent.CombinedOutput(); err != nil {
				t.Fatalf("old app failed to hand off: %v: %s", err, output)
			}
			t.Cleanup(func() {
				_ = os.WriteFile(filepath.Join(config, "finish-test-candidate"), nil, 0600)
				for _, pidFile := range []string{"candidate-pid", "previous-restarted"} {
					data, _ := os.ReadFile(filepath.Join(config, pidFile))
					pid, _ := strconv.Atoi(string(data))
					deadline := time.Now().Add(5 * time.Second)
					for processIsRunning(pid) && time.Now().Before(deadline) {
						time.Sleep(50 * time.Millisecond)
					}
					if processIsRunning(pid) {
						_ = stopCandidate(pid, target.Path)
					}
				}
			})
			wanted := "updated"
			if mode == "fail" {
				wanted = "rolled_back"
			}
			var outcome Outcome
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				outcome, err = ReadOutcome(config)
				if err == nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if err != nil || outcome.Status != wanted {
				log, _ := os.ReadFile(filepath.Join(config, "update.log"))
				t.Fatalf("outcome=%+v err=%v log=%s", outcome, err, log)
			}
			if outcome.Version != p.Version {
				t.Fatalf("wrong outcome version: %+v", outcome)
			}
			after, err := snapshotFileIdentity(target.Path)
			if err != nil {
				t.Fatal(err)
			}
			if os.SameFile(before, after) != (mode == "fail") {
				t.Fatalf("expected replacement or restoration did not occur: mode=%s", mode)
			}
			if _, err := os.Stat(p.Backup); !os.IsNotExist(err) {
				t.Fatalf("backup remains after confirmed %s: %v", mode, err)
			}
			if mode == "fail" {
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(filepath.Join(config, "previous-restarted")); err == nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				if _, err := os.Stat(filepath.Join(config, "previous-restarted")); err != nil {
					t.Fatal("previous version was not restarted after rollback")
				}
			} else {
				_ = os.WriteFile(filepath.Join(config, "finish-test-candidate"), nil, 0600)
			}
			deadline = time.Now().Add(3 * time.Second)
			for processIsRunning(outcome.HelperPID) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if err := CleanupResidue(config); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(p.Lock); !os.IsNotExist(err) {
				t.Fatalf("installation lock was not released: %v", err)
			}
		})
	}
}

func snapshotFileIdentity(path string) (os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// Windows os.Stat defers file-ID lookup until SameFile is called. A handle
	// stat snapshots identity now, before the path can be replaced by the helper.
	return file.Stat()
}

func TestStartupReceiptRejectsWrongVersionAndExecutable(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp(root, ".smartstage-update-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "startup.json")
	request := startupRequest{Version: "v1.2.3", Nonce: strings.Repeat("a", 64), Target: Target{Path: filepath.Join(root, "other"), Kind: "binary"}}
	if err := writeJSON(path, request); err != nil {
		t.Fatal(err)
	}
	if err := RegisterStartup(path, "v1.2.4"); err == nil {
		t.Fatal("accepted wrong version")
	}
	if err := RegisterStartup(path, "v1.2.3"); err == nil {
		t.Fatal("accepted receipt for another executable")
	}
	if _, err := os.Stat(path + ".started"); !os.IsNotExist(err) {
		t.Fatal("invalid receipt produced a startup registration")
	}
}
