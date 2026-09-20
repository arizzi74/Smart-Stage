package update

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const maxExpandedSize int64 = 1024 * 1024 * 1024
const maxEntrySize int64 = 512 * 1024 * 1024
const maxArchiveEntries = 4096

type applyPlan struct {
	Target    Target   `json:"target"`
	Version   string   `json:"version"`
	Work      string   `json:"work"`
	Payload   string   `json:"payload"`
	Backup    string   `json:"backup"`
	Helper    string   `json:"helper"`
	ConfigDir string   `json:"configDir"`
	Cwd       string   `json:"cwd"`
	Args      []string `json:"args"`
	ParentPID int      `json:"parentPID"`
	OldHash   string   `json:"oldHash"`
	Nonce     string   `json:"nonce"`
	Lock      string   `json:"lock"`
}

type Prepared struct {
	mu       sync.Mutex
	plan     applyPlan
	launched bool
}

// Stage copies a checksum-verified archive into a private directory on the same
// volume as the installation. No replacement or application shutdown happens here.
func Stage(ctx context.Context, target Target, archivePath, version string, args []string, configDir string) (_ *Prepared, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkTarget(target); err != nil {
		return nil, err
	}
	configDir, err = filepath.Abs(configDir)
	if err != nil {
		return nil, err
	}
	configDir = canonicalSystemPath(configDir)
	if err := noSymlinks(configDir); err != nil {
		return nil, fmt.Errorf("update configuration directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cwd = canonicalSystemPath(cwd)
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	nonceText := hex.EncodeToString(nonce)
	lock := filepath.Join(filepath.Dir(target.Path), ".smartstage-install.lock")
	if err = acquireInstallLock(lock, nonceText, os.Getpid()); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = releaseInstallLock(lock, nonceText)
		}
	}()
	if err = checkTarget(target); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(filepath.Dir(target.Path), ".smartstage-update-")
	if err != nil {
		return nil, fmt.Errorf("Smart Stage cannot write its installation folder; install it in your user Applications folder or another writable folder: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(work)
		}
	}()
	if err = os.Chmod(work, 0700); err != nil {
		return nil, err
	}
	payloadDir := filepath.Join(work, "unpacked")
	if err = os.Mkdir(payloadDir, 0700); err != nil {
		return nil, err
	}
	if err = extractArchive(ctx, archivePath, payloadDir, target); err != nil {
		return nil, err
	}
	name := "smartstage"
	if target.GOOS == "windows" {
		name += ".exe"
	}
	if target.Kind == "bundle" {
		name = "Smart Stage.app"
	}
	payload := filepath.Join(payloadDir, name)
	if err = validatePayload(ctx, target, payload, version); err != nil {
		return nil, err
	}
	helper := filepath.Join(work, "update-helper")
	if target.GOOS == "windows" {
		helper += ".exe"
	}
	if err = copyFile(ctx, corePath(target), helper, 0700); err != nil {
		return nil, err
	}
	oldHash, err := hashFile(ctx, corePath(target))
	if err != nil {
		return nil, err
	}
	helperHash, err := hashFile(ctx, helper)
	if err != nil {
		return nil, err
	}
	if oldHash != helperHash {
		return nil, errors.New("the installed executable changed while preparing its update")
	}
	plan := applyPlan{Target: target, Version: version, Work: work, Payload: payload, Backup: filepath.Join(work, "previous"), Helper: helper, ConfigDir: configDir, Cwd: cwd, Args: restartArgs(args, configDir), ParentPID: os.Getpid(), OldHash: oldHash, Nonce: nonceText, Lock: lock}
	if err = writeJSON(filepath.Join(work, "plan.json"), plan); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &Prepared{plan: plan}, nil
}

func (p *Prepared) Abort() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.launched {
		return errors.New("the update helper has already started")
	}
	err := os.RemoveAll(p.plan.Work)
	lockErr := releaseInstallLock(p.plan.Lock, p.plan.Nonce)
	if err != nil {
		return err
	}
	return lockErr
}

type installLockOwner struct {
	Nonce string `json:"nonce"`
	PID   int    `json:"pid"`
}

func acquireInstallLock(path, nonce string, pid int) error {
	if err := os.Mkdir(path, 0700); err != nil {
		return fmt.Errorf("another installation may be in progress (lock: %s); if an earlier installer was interrupted, remove that directory only after confirming it is no longer running: %w", path, err)
	}
	if err := writeJSON(filepath.Join(path, "owner.json"), installLockOwner{Nonce: nonce, PID: pid}); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := os.WriteFile(filepath.Join(path, "pid"), []byte(fmt.Sprintln(pid)), 0600); err != nil {
		_ = releaseInstallLock(path, nonce)
		return err
	}
	return nil
}
func checkInstallLock(path, nonce string) error {
	if err := noSymlinks(path); err != nil {
		return err
	}
	var owner installLockOwner
	if err := readJSON(filepath.Join(path, "owner.json"), &owner); err != nil {
		return err
	}
	if owner.Nonce != nonce || nonce == "" {
		return errors.New("installation lock belongs to another updater")
	}
	return nil
}
func transferInstallLock(path, nonce string, pid int) error {
	if err := checkInstallLock(path, nonce); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(path, "owner.json"), installLockOwner{Nonce: nonce, PID: pid}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, "pid"), []byte(fmt.Sprintln(pid)), 0600)
}
func releaseInstallLock(path, nonce string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := checkInstallLock(path, nonce); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(path, "owner.json")); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(path, "pid")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(path)
}

func restartArgs(args []string, configDir string) []string {
	result := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if name == "update-receipt" || name == "smartstage-apply-update" || name == "config-dir" {
			if !strings.Contains(a, "=") && i+1 < len(args) {
				i++
			}
			continue
		}
		result = append(result, a)
	}
	return append(result, "--config-dir", configDir)
}

func extractArchive(ctx context.Context, archive, destination string, target Target) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open update ZIP: %w", err)
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > maxArchiveEntries {
		return errors.New("update ZIP has an invalid number of entries")
	}
	seen := map[string]bool{}
	var expanded int64
	for _, entry := range z.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") {
			return fmt.Errorf("unsafe update ZIP entry %q", entry.Name)
		}
		for _, component := range strings.Split(name, "/") {
			base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
			if component == ".." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9') {
				return fmt.Errorf("unsafe update ZIP path component %q", component)
			}
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate update ZIP entry %q", name)
		}
		seen[key] = true
		mode := entry.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return fmt.Errorf("update ZIP contains a link or special file: %s", name)
		}
		if entry.UncompressedSize64 > uint64(maxEntrySize) {
			return errors.New("update ZIP entry exceeds its size limit")
		}
		expanded += int64(entry.UncompressedSize64)
		if expanded > maxExpandedSize {
			return errors.New("update ZIP exceeds its expanded size limit")
		}
		if target.Kind == "bundle" {
			if name == "__MACOSX" || strings.HasPrefix(name, "__MACOSX/") {
				continue
			}
			if name != "Smart Stage.app" && !strings.HasPrefix(name, "Smart Stage.app/") {
				return errors.New("update ZIP contains a file outside Smart Stage.app")
			}
		} else {
			expected := "smartstage"
			if target.GOOS == "windows" {
				expected += ".exe"
			}
			if name != expected || mode.IsDir() {
				return errors.New("portable update ZIP must contain exactly its executable")
			}
		}
		out := filepath.Join(destination, filepath.FromSlash(name))
		if mode.IsDir() {
			if err := os.MkdirAll(out, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		permissions := os.FileMode(0600)
		if mode&0111 != 0 {
			permissions = 0700
		}
		in, err := entry.Open()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
		if err != nil {
			in.Close()
			return err
		}
		count, copyErr := io.Copy(file, &contextReader{ctx: ctx, r: io.LimitReader(in, maxEntrySize+1)})
		closeErr := file.Close()
		inErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if inErr != nil {
			return inErr
		}
		if count != int64(entry.UncompressedSize64) || count > maxEntrySize {
			return errors.New("update ZIP expanded entry size is inconsistent")
		}
	}
	return nil
}

func validatePayload(ctx context.Context, target Target, payload, version string) error {
	core := payload
	if target.Kind == "bundle" {
		if err := checkBundleIdentity(payload, version); err != nil {
			return err
		}
		core = filepath.Join(payload, "Contents", "MacOS", "smartstage")
		if err := checkArchitecture(filepath.Join(payload, "Contents", "MacOS", "SmartStageLauncher"), target.GOOS, target.GOARCH); err != nil {
			return err
		}
		icon, err := os.Stat(filepath.Join(payload, "Contents", "Resources", "smartstage.icns"))
		if err != nil || !icon.Mode().IsRegular() || icon.Size() < 8 {
			return errors.New("update bundle is missing its icon")
		}
	}
	if err := checkArchitecture(core, target.GOOS, target.GOARCH); err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(core)
	if err != nil {
		return fmt.Errorf("update is not a readable Go application: %w", err)
	}
	if err := validateGoBuild(info, target, version); err != nil {
		return err
	}
	if target.GOOS != "windows" {
		st, err := os.Stat(core)
		if err != nil {
			return err
		}
		if st.Mode()&0111 == 0 {
			return errors.New("update executable has no execution permission")
		}
	}
	return verifyNativeSignature(ctx, target, payload)
}

func validateGoBuild(info *buildinfo.BuildInfo, target Target, version string) error {
	if info.Path != "smartstage/cmd/smartstage" || info.Main.Path != "smartstage" || info.Main.Replace != nil {
		return errors.New("update executable is not Smart Stage")
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		if _, duplicate := settings[s.Key]; duplicate {
			return errors.New("update executable has duplicate build metadata")
		}
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != target.GOOS || settings["GOARCH"] != target.GOARCH {
		return errors.New("update Go platform does not match the installation")
	}
	if settings["CGO_ENABLED"] != "1" {
		return errors.New("update executable does not contain the required native playback backend")
	}
	// Go deliberately omits -ldflags from build info when -trimpath is used.
	// Official builds instead carry the exact Git tag as their main module
	// version. Require a clean tagged source build without executing the payload.
	// Startup receipts additionally check the running main.version before the
	// helper deletes its rollback copy.
	if info.Main.Version != version {
		return errors.New("update executable version does not match the selected release")
	}
	if settings["vcs"] != "git" || settings["vcs.modified"] != "false" {
		return errors.New("update executable was not built from a clean Git revision")
	}
	revision := settings["vcs.revision"]
	if _, err := hex.DecodeString(revision); err != nil || len(revision) != 40 {
		return errors.New("update executable has invalid Git revision metadata")
	}
	return nil
}

func checkArchitecture(file, goos, goarch string) error {
	if goos == "darwin" {
		f, err := macho.Open(file)
		if err != nil {
			return fmt.Errorf("invalid macOS executable: %w", err)
		}
		defer f.Close()
		wanted := macho.CpuAmd64
		if goarch == "arm64" {
			wanted = macho.CpuArm64
		}
		if f.Cpu != wanted || f.Type != macho.TypeExec {
			return errors.New("macOS executable architecture or file type does not match")
		}
		return nil
	}
	if goos == "windows" {
		f, err := pe.Open(file)
		if err != nil {
			return fmt.Errorf("invalid Windows executable: %w", err)
		}
		defer f.Close()
		wanted := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if goarch == "arm64" {
			wanted = pe.IMAGE_FILE_MACHINE_ARM64
		}
		if f.Machine != wanted || f.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || f.Characteristics&pe.IMAGE_FILE_DLL != 0 {
			return errors.New("Windows executable architecture or file type does not match")
		}
		return nil
	}
	return errors.New("unsupported update executable platform")
}

func checkBundleIdentity(bundle, version string) error {
	file, err := os.Open(filepath.Join(bundle, "Contents", "Info.plist"))
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := xml.NewDecoder(io.LimitReader(file, 1024*1024))
	values := map[string]string{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var key string
		if err := decoder.DecodeElement(&key, &start); err != nil {
			return err
		}
		for {
			token, err = decoder.Token()
			if err != nil {
				return err
			}
			if next, ok := token.(xml.StartElement); ok {
				if next.Name.Local == "string" {
					var value string
					if err := decoder.DecodeElement(&value, &next); err != nil {
						return err
					}
					if _, duplicate := values[key]; duplicate {
						return errors.New("duplicate bundle metadata key")
					}
					values[key] = value
				} else {
					if err := decoder.Skip(); err != nil {
						return err
					}
				}
				break
			}
		}
	}
	if values["CFBundleIdentifier"] != "com.github.arizzi74.smartstage" || values["CFBundleExecutable"] != "SmartStageLauncher" {
		return errors.New("update bundle identity or launcher is invalid")
	}
	if version != "" && values["SmartStageVersion"] != version {
		return errors.New("update bundle version does not match the selected release")
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func copyFile(ctx context.Context, source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, &contextReader{ctx: ctx, r: in})
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func hashFile(ctx context.Context, file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, &contextReader{ctx: ctx, r: f})
	return hex.EncodeToString(h.Sum(nil)), err
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".update-json-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
