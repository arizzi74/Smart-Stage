package gatewayinstall

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const configPath = "/etc/smartstage-gateway/config.json"
const binaryPath = "/usr/local/bin/smartstage-gateway"
const unitPath = "/etc/systemd/system/smartstage-gateway.service"

type gatewayConfig struct {
	Listen    string `json:"listen"`
	PublicURL string `json:"publicURL"`
	Token     string `json:"token"`
}
type installer struct {
	input  *bufio.Reader
	output io.Writer
	run    func(string, ...string) ([]byte, error)
}

// Run installs the already verified executable. Interactive reads use /dev/tty
// so curl | sh does not consume the shell program as prompt answers.
func Run(args []string) error {
	flags := flag.NewFlagSet("smartstage-gateway install", flag.ContinueOnError)
	list := flags.Bool("list-hosts", false, "list suitable existing nginx HTTPS virtual hosts without changing files")
	hostName := flags.String("host", "", "select an existing nginx HTTPS hostname")
	caddyDomain := flags.String("caddy-domain", "", "configure a dedicated Caddy HTTPS domain when nginx is absent")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected install arguments")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("run the installer with sudo (it writes systemd and reverse proxy configuration)")
	}
	i := &installer{output: os.Stdout, run: runCommand}
	if *list {
		hosts, err := i.nginxHosts()
		if err != nil {
			return err
		}
		i.printHosts(hosts)
		return nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemd is required: %w", err)
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return fmt.Errorf("systemd must be running on this Linux server")
	}
	lock, err := os.OpenFile("/run/lock/smartstage-gateway-install.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another gateway installation is running: %w", err)
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("an interactive terminal is required: %w", err)
	}
	defer tty.Close()
	i.input = bufio.NewReader(tty)
	var selected *nginxHost
	var domain string
	var installCaddy bool
	if _, err := exec.LookPath("nginx"); err == nil {
		if *caddyDomain != "" {
			return fmt.Errorf("nginx is present; choose an existing HTTPS host with --host")
		}
		hosts, err := i.nginxHosts()
		if err != nil {
			return err
		}
		i.printHosts(hosts)
		if len(hosts) == 0 {
			return fmt.Errorf("no unambiguous HTTPS virtual host is suitable; create a named TLS host on port 443 without a /smartstage location or server-level redirect, then retry")
		}
		for idx := range hosts {
			if hosts[idx].Name == *hostName {
				selected = &hosts[idx]
			}
		}
		if *hostName != "" && selected == nil {
			return fmt.Errorf("%q is not in the suitable host list", *hostName)
		}
		if selected == nil {
			answer, err := i.ask("Choose HTTPS host number: ")
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(answer)
			if err != nil || n < 1 || n > len(hosts) {
				return fmt.Errorf("invalid host selection")
			}
			selected = &hosts[n-1]
		}
		domain = selected.Name
	} else {
		fmt.Fprintln(i.output, "nginx was not found. Caddy can provide automatic HTTPS for a dedicated domain.")
		if _, err := exec.LookPath("caddy"); err != nil {
			installCaddy = true
		}
		domain = *caddyDomain
		if domain == "" {
			domain, err = i.ask("Public domain pointing to this server (for example stage.example.com): ")
			if err != nil {
				return err
			}
		}
		if !validHostname(domain) {
			return fmt.Errorf("enter a DNS hostname without a scheme, port, path or wildcard")
		}
		fmt.Fprintln(i.output, "Caddy requires public DNS pointing here and incoming TCP ports 80 and 443. Existing unrelated Caddy configuration will be preserved.")
	}
	publicURL := "https://" + domain + "/smartstage"
	config, err := loadOrCreateConfig(configPath, publicURL)
	if err != nil {
		return err
	}
	if !i.commandOK("systemctl", "is-active", "--quiet", "smartstage-gateway.service") {
		connection, dialErr := net.DialTimeout("tcp", "127.0.0.1:8790", time.Second)
		if dialErr == nil {
			connection.Close()
			return fmt.Errorf("127.0.0.1:8790 is already in use by another service")
		}
	}
	fmt.Fprintf(i.output, "\nInstall Smart Stage Gateway at %s\nService: %s\nConfig: %s\n", publicURL, unitPath, configPath)
	if selected != nil {
		fmt.Fprintf(i.output, "nginx file: %s (a timestamped backup will be kept)\n", selected.Source)
	} else if installCaddy {
		fmt.Fprintln(i.output, "Caddy will be installed from its official package repository.")
	}
	if err := i.confirm("Apply this configuration? [y/N]: "); err != nil {
		return err
	}
	if installCaddy {
		if err := i.installCaddy(); err != nil {
			return err
		}
	}
	var proxyPath string
	var proxyData []byte
	var proxyOriginal []byte
	if selected != nil {
		proxyPath, err = filepath.EvalSymlinks(selected.Source)
		if err != nil {
			return err
		}
		proxyData, err = insertNginx(*selected)
		if err != nil {
			return err
		}
		proxyOriginal = selected.Data
	} else {
		proxyPath = "/etc/caddy/Caddyfile"
		proxyOriginal, err = os.ReadFile(proxyPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		proxyData, err = caddyConfiguration(proxyOriginal, domain)
		if err != nil {
			return err
		}
		if len(proxyOriginal) > 0 && !bytes.Equal(proxyOriginal, proxyData) {
			fmt.Fprintf(i.output, "Caddy configuration will be appended to %s; its current sites remain intact.\n", proxyPath)
			if err := i.confirm("Append the Smart Stage site to this Caddyfile? [y/N]: "); err != nil {
				return err
			}
		}
	}
	// All interactive choices are complete before changing the service.
	if err := i.ensureUser(); err != nil {
		return err
	}
	serviceUser, err := user.Lookup("smartstage-gateway")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(serviceUser.Gid)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
		return err
	}
	if err := os.Chown(filepath.Dir(configPath), 0, gid); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(configPath), 0750); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	tx := &fileTransaction{}
	failed := true
	wasActive := i.commandOK("systemctl", "is-active", "--quiet", "smartstage-gateway.service")
	wasEnabled := i.commandOK("systemctl", "is-enabled", "--quiet", "smartstage-gateway.service")
	defer func() {
		if failed {
			if rollbackErr := tx.rollback(); rollbackErr != nil {
				fmt.Fprintf(i.output, "Rollback needs attention: %v\n", rollbackErr)
			}
			i.run("systemctl", "daemon-reload")
			if wasActive {
				i.run("systemctl", "restart", "smartstage-gateway.service")
			} else {
				i.run("systemctl", "stop", "smartstage-gateway.service")
			}
			if !wasEnabled {
				i.run("systemctl", "disable", "smartstage-gateway.service")
			}
		}
	}()
	if err := tx.write(binaryPath, binary, 0755, 0, 0, nil); err != nil {
		return err
	}
	if err := tx.write(configPath, encoded, 0640, 0, gid, nil); err != nil {
		return err
	}
	if err := tx.write(unitPath, []byte(systemdUnit), 0644, 0, 0, nil); err != nil {
		return err
	}
	if _, err := i.run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := i.run("systemctl", "enable", "smartstage-gateway.service"); err != nil {
		return err
	}
	if _, err := i.run("systemctl", "restart", "smartstage-gateway.service"); err != nil {
		return err
	}
	if err := waitDaemon(); err != nil {
		return err
	}
	if proxyData != nil {
		if err := tx.write(proxyPath, proxyData, 0644, -1, -1, proxyOriginal); err != nil {
			return err
		}
	}
	if selected != nil {
		if _, err := i.run("nginx", "-t"); err != nil {
			return err
		}
		if _, err := i.run("systemctl", "reload", "nginx"); err != nil {
			rollbackErr := tx.rollback()
			_, validationErr := i.run("nginx", "-t")
			_, reloadErr := i.run("systemctl", "reload", "nginx")
			return errors.Join(err, rollbackErr, validationErr, reloadErr)
		}
	} else {
		if _, err := i.run("caddy", "validate", "--config", proxyPath, "--adapter", "caddyfile"); err != nil {
			return err
		}
		if _, err := i.run("systemctl", "enable", "caddy"); err != nil {
			return err
		}
		if _, err := i.run("systemctl", "reload-or-restart", "caddy"); err != nil {
			rollbackErr := tx.rollback()
			_, reloadErr := i.run("systemctl", "reload-or-restart", "caddy")
			return errors.Join(err, rollbackErr, reloadErr)
		}
	}
	failed = false
	fmt.Fprintf(i.output, "\nGateway is running. In Smart Stage Admin choose Public gateway and enter:\nURL: %s\nToken: %s\n\nKeep this token private. It is stored in %s.\nService status: sudo systemctl status smartstage-gateway\n", config.PublicURL, config.Token, configPath)
	if selected == nil {
		fmt.Fprintln(i.output, "Caddy obtains the HTTPS certificate automatically. DNS and port reachability must be correct before remote connections can succeed.")
	}
	return nil
}

func runCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}
func (i *installer) commandOK(name string, args ...string) bool {
	_, err := i.run(name, args...)
	return err == nil
}
func (i *installer) ask(prompt string) (string, error) {
	fmt.Fprint(i.output, prompt)
	line, err := i.input.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read installer answer: %w", err)
	}
	return strings.TrimSpace(line), nil
}
func (i *installer) confirm(prompt string) error {
	answer, err := i.ask(prompt)
	if err != nil {
		return err
	}
	if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		return fmt.Errorf("installation canceled")
	}
	return nil
}
func (i *installer) printHosts(hosts []nginxHost) {
	fmt.Fprintln(i.output, "Suitable nginx HTTPS virtual hosts:")
	for n, host := range hosts {
		suffix := ""
		if host.Managed {
			suffix = " (already configured)"
		}
		fmt.Fprintf(i.output, "  %d. https://%s/smartstage — %s%s\n", n+1, host.Name, host.Source, suffix)
	}
}
func (i *installer) nginxHosts() ([]nginxHost, error) {
	dump, err := i.run("nginx", "-T")
	if err != nil {
		return nil, err
	}
	paths, err := nginxSources(string(dump))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		files[path] = data
	}
	return suitableHosts(paths[0], files)
}
func (i *installer) ensureUser() error {
	if existing, err := user.Lookup("smartstage-gateway"); err == nil {
		uid, err := strconv.Atoi(existing.Uid)
		if err != nil || uid == 0 {
			return fmt.Errorf("smartstage-gateway must be an unprivileged system account")
		}
		return nil
	}
	shell := "/usr/sbin/nologin"
	if _, err := os.Stat(shell); err != nil {
		shell = "/sbin/nologin"
	}
	_, err := i.run("useradd", "--system", "--user-group", "--no-create-home", "--home-dir", "/nonexistent", "--shell", shell, "smartstage-gateway")
	return err
}

func loadOrCreateConfig(path, publicURL string) (gatewayConfig, error) {
	c := gatewayConfig{Listen: "127.0.0.1:8790", PublicURL: publicURL}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &c); err != nil {
			return c, fmt.Errorf("existing gateway configuration: %w", err)
		}
		if c.Listen != "127.0.0.1:8790" {
			return c, fmt.Errorf("existing listener differs from installer-managed 127.0.0.1:8790")
		}
		token, err := hex.DecodeString(c.Token)
		if err != nil || len(token) != 32 {
			return c, fmt.Errorf("existing gateway token is invalid; refusing to replace it")
		}
		if c.PublicURL != publicURL {
			return c, fmt.Errorf("gateway is already installed at %s; changing the public URL requires an explicit manual migration", c.PublicURL)
		}
		return c, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return c, err
	}
	c.Token = hex.EncodeToString(token[:])
	return c, nil
}

func waitDaemon() error {
	client := &http.Client{Timeout: time.Second}
	for n := 0; n < 30; n++ {
		response, err := client.Get("http://127.0.0.1:8790/smartstage/health")
		if err == nil {
			io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("gateway did not become healthy; inspect journalctl -u smartstage-gateway")
}

const systemdUnit = `[Unit]
Description=Smart Stage public remote-control gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=smartstage-gateway
Group=smartstage-gateway
ExecStart=/usr/local/bin/smartstage-gateway serve --config /etc/smartstage-gateway/config.json
Restart=on-failure
RestartSec=3
TimeoutStopSec=15
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictSUIDSGID=true
LockPersonality=true
CapabilityBoundingSet=
AmbientCapabilities=
UMask=0077

[Install]
WantedBy=multi-user.target
`

// A transaction keeps on-disk backups and restores files atomically if config
// validation or service reload fails. Existing ownership and mode are retained.
type fileSnapshot struct {
	path     string
	data     []byte
	mode     os.FileMode
	uid, gid int
	existed  bool
}
type fileTransaction struct{ files []fileSnapshot }

func (tx *fileTransaction) write(path string, data []byte, mode os.FileMode, uid, gid int, expected []byte) error {
	old := fileSnapshot{path: path, mode: mode, uid: uid, gid: gid}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file %s", path)
		}
		old.existed = true
		old.data, err = os.ReadFile(path)
		if err != nil {
			return err
		}
		old.mode = info.Mode().Perm()
		stat := info.Sys().(*syscall.Stat_t)
		old.uid = int(stat.Uid)
		old.gid = int(stat.Gid)
		if uid < 0 && gid < 0 {
			mode = old.mode
		}
		if expected != nil && !bytes.Equal(expected, old.data) {
			return fmt.Errorf("%s changed since inspection; retry installation", path)
		}
		if bytes.Equal(old.data, data) && old.mode == mode && (uid < 0 || old.uid == uid) && (gid < 0 || old.gid == gid) {
			return nil
		}
		// Hidden backup names stay outside ordinary nginx include globs such as
		// sites-enabled/*; a visible sibling could be loaded as a duplicate site.
		backup := filepath.Join(filepath.Dir(path), fmt.Sprintf(".smartstage-backup-%s-%d", filepath.Base(path), time.Now().UnixNano()))
		if err := os.WriteFile(backup, old.data, old.mode); err != nil {
			return err
		}
		if err := os.Chown(backup, old.uid, old.gid); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if expected != nil && len(expected) != 0 {
		return fmt.Errorf("%s disappeared since inspection", path)
	}
	if uid < 0 {
		uid = old.uid
	}
	if gid < 0 {
		gid = old.gid
	}
	if uid < 0 {
		uid = 0
	}
	if gid < 0 {
		gid = 0
	}
	if err := atomicFile(path, data, mode, uid, gid); err != nil {
		return err
	}
	tx.files = append(tx.files, old)
	return nil
}
func (tx *fileTransaction) rollback() error {
	var failures []error
	for n := len(tx.files) - 1; n >= 0; n-- {
		old := tx.files[n]
		var err error
		if old.existed {
			err = atomicFile(old.path, old.data, old.mode, old.uid, old.gid)
		} else {
			err = os.Remove(old.path)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	tx.files = nil
	return errors.Join(failures...)
}
func atomicFile(path string, data []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".smartstage-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		err = f.Chown(uid, gid)
	}
	if err == nil {
		_, err = f.Write(data)
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
	return os.Rename(tmp, path)
}
