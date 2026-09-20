package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

type restartTestReceipt struct {
	PID           int    `json:"pid"`
	Argument      string `json:"argument"`
	ParentRunning bool   `json:"parentRunning"`
}

// Only the tiny process bodies are fixtures. PrepareRestart, detached helper
// launch, parent waiting, and the final argument handoff use production code.
func restartTestProcess() bool {
	if len(os.Args) >= 5 && os.Args[1] == "--smartstage-restart" && os.Args[4] == "--" {
		pid, err := strconv.Atoi(os.Args[2])
		if err == nil {
			err = RunRestartHelper(pid, os.Args[3], os.Args[5:])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) == 4 && os.Args[1] == "--restart-test-parent" {
		dir, argument := os.Args[2], os.Args[3]
		restart, err := PrepareRestart([]string{"--restart-test-candidate", dir, argument, strconv.Itoa(os.Getpid())}, dir)
		if err == nil {
			err = restart.Launch()
		}
		if err != nil {
			panic(err)
		}
		time.Sleep(200 * time.Millisecond)
		if _, err := os.Stat(filepath.Join(dir, "restart-test-result.json")); !os.IsNotExist(err) {
			panic("candidate started before its original parent exited")
		}
		os.Exit(0)
	}
	if len(os.Args) == 5 && os.Args[1] == "--restart-test-candidate" {
		pid, err := strconv.Atoi(os.Args[4])
		if err != nil {
			panic(err)
		}
		data, err := json.Marshal(restartTestReceipt{PID: os.Getpid(), Argument: os.Args[3], ParentRunning: processIsRunning(pid)})
		if err == nil {
			err = os.WriteFile(filepath.Join(os.Args[2], "restart-test-result.json"), data, 0600)
		}
		if err != nil {
			panic(err)
		}
		os.Exit(0)
	}
	return false
}

func TestRestartHelperWaitsAndPreservesArguments(t *testing.T) {
	if testing.Short() {
		t.Skip("real detached restart process test")
	}
	t.Setenv("SMARTSTAGE_SKIP_FIREWALL", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Café's config with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.json"), []byte(`{"mode":"lan"}`), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argument := "Café's --stage $(literal) with spaces"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	parent := exec.CommandContext(ctx, executable, "--restart-test-parent", dir, argument)
	if output, err := parent.CombinedOutput(); err != nil {
		t.Fatalf("original process restart handoff failed: %v: %s", err, output)
	}
	var receipt restartTestReceipt
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(dir, "restart-test-result.json"))
		if err == nil && json.Unmarshal(data, &receipt) == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if receipt.PID <= 1 || receipt.PID == parent.Process.Pid || receipt.Argument != argument || receipt.ParentRunning {
		log, _ := os.ReadFile(filepath.Join(dir, "restart.log"))
		t.Fatalf("incorrect restart handoff: %+v; log=%s", receipt, log)
	}
	if _, err := os.Stat(filepath.Join(dir, "lan-firewall-warning.txt")); err != nil {
		t.Fatal("restart did not run the mode-aware firewall step:", err)
	}
	if runtime.GOOS == "windows" {
		// Windows cannot remove the temporary log until the candidate has closed
		// its inherited handle (race builds may briefly delay process exit).
		deadline := time.Now().Add(5 * time.Second)
		for processIsRunning(receipt.PID) && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		if processIsRunning(receipt.PID) {
			_ = stopCandidate(receipt.PID, executable)
			t.Fatal("restart candidate did not finish")
		}
	}
}
