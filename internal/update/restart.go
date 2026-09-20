package update

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Restart uses a detached copy of the same process to wait for native cleanup
// and the configuration lock to close before reopening the installed app.
// It never replaces the executable or downloads code.
type Restart struct {
	args                  []string
	configDir, executable string
}

func PrepareRestart(args []string, configDir string) (*Restart, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(configDir) {
		return nil, errors.New("restart requires an absolute configuration directory")
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if _, err := DetectTarget(executable); err != nil {
			return nil, err
		}
	}
	return &Restart{args: append([]string(nil), args...), configDir: configDir, executable: executable}, nil
}
func (r *Restart) Abort() error { return nil }
func (r *Restart) Launch() error {
	f, err := os.OpenFile(filepath.Join(r.configDir, "restart.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	args := append([]string{"--smartstage-restart", strconv.Itoa(os.Getpid()), r.configDir, "--"}, r.args...)
	cmd := exec.Command(r.executable, args...)
	cmd.Env = cleanEnvironment()
	cmd.Stdout = f
	cmd.Stderr = f
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// RunRestartHelper is invoked before any native UI, HTTP server or storage is
// initialized. Parent waiting prevents relaunch from racing the old process.
func RunRestartHelper(pid int, configDir string, args []string) error {
	if pid <= 1 || !filepath.IsAbs(configDir) {
		return errors.New("invalid restart request")
	}
	if processIsRunning(pid) {
		parent, err := openParent(pid)
		if err != nil {
			if processIsRunning(pid) {
				return err
			}
		} else {
			defer parent.Close()
			if err := parent.Wait(45 * time.Second); err != nil {
				return err
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	target := Target{Path: executable, Kind: "binary", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		target, err = DetectTarget(executable)
		if err != nil {
			return err
		}
	}
	warning := refreshFirewall(target, configDir, log.Default())
	// Keep a cancelled approval visible in Admin after the app comes back.
	if err := os.WriteFile(filepath.Join(configDir, "lan-firewall-warning.txt"), []byte(warning), 0600); err != nil {
		return err
	}
	if warning != "" {
		log.Print(warning)
	}
	cmd := restartCommand(target, args)
	cmd.Env = cleanEnvironment()
	configureDetached(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("reopen Smart Stage: %w", err)
	}
	return cmd.Process.Release()
}
