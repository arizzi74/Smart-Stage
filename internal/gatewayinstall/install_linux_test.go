package gatewayinstall

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransactionRollbackPreservesExistingFilesAndRemovesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nginx.conf")
	original := []byte("original virtual hosts\n")
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	tx := &fileTransaction{}
	if err := tx.write(path, []byte("new invalid config"), 0640, os.Getuid(), os.Getgid(), original); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(dir, "new-service")
	if err := tx.write(newPath, []byte("new unit"), 0644, os.Getuid(), os.Getgid(), nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.rollback(); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("rollback changed original: %q %v", actual, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("mode: %o", info.Mode().Perm())
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new file remains: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, ".smartstage-backup-nginx.conf-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups: %v %v", backups, err)
	}
	backup, _ := os.ReadFile(backups[0])
	if !bytes.Equal(backup, original) {
		t.Fatal("backup differs")
	}
}

func TestTransactionRefusesConcurrentEditsAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("edited by owner"), 0600)
	tx := &fileTransaction{}
	if err := tx.write(path, []byte("replacement"), 0600, os.Getuid(), os.Getgid(), []byte("stale inspection")); err == nil {
		t.Fatal("overwrote concurrent change")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := tx.write(link, []byte("replacement"), 0600, os.Getuid(), os.Getgid(), nil); err == nil {
		t.Fatal("replaced symlink")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != "edited by owner" {
		t.Fatal("changed owner's file")
	}
}

func TestConfigTokenCreatedOnceAndMigrationExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	first, err := loadOrCreateConfig(path, "https://show.example.com/smartstage")
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreateConfig(path, "https://show.example.com/smartstage")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Token) != 64 || first.Token == second.Token {
		t.Fatal("new tokens not independently random")
	}
	data := []byte(`{"listen":"127.0.0.1:8790","publicURL":"https://show.example.com/smartstage","token":"` + first.Token + `"}`)
	os.WriteFile(path, data, 0600)
	existing, err := loadOrCreateConfig(path, first.PublicURL)
	if err != nil || existing.Token != first.Token {
		t.Fatalf("existing token not retained: %v", err)
	}
	if _, err := loadOrCreateConfig(path, "https://other.example.com/smartstage"); err == nil {
		t.Fatal("silently moved existing gateway")
	}
	os.WriteFile(path, []byte(`{"listen":"0.0.0.0:8790"}`), 0600)
	if _, err := loadOrCreateConfig(path, first.PublicURL); err == nil {
		t.Fatal("accepted non-loopback existing configuration")
	}
}

func TestCaddyPreservesExistingSitesAndIsIdempotent(t *testing.T) {
	existing := []byte("# original\nwww.example.org {\n respond \"original site\"\n}\n")
	updated, err := caddyConfiguration(existing, "show.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(updated, existing) {
		t.Fatal("overwrote original Caddy sites")
	}
	if !bytes.Contains(updated, []byte("@smartstage path /smartstage /smartstage/*")) || bytes.Contains(updated, []byte("handle_path")) {
		t.Fatal("proxy must preserve complete prefix")
	}
	again, err := caddyConfiguration(updated, "show.example.com")
	if err != nil || !bytes.Equal(again, updated) {
		t.Fatalf("repeated installation changed config: %v", err)
	}
	if _, err := caddyConfiguration(updated, "other.example.com"); err == nil {
		t.Fatal("accepted conflicting managed Caddy domain")
	}
	if _, err := caddyConfiguration([]byte("show.example.com { respond 200 }"), "show.example.com"); err == nil {
		t.Fatal("accepted pre-existing site with same hostname")
	}
}

func TestGeneratedNginxConfigWithRealValidator(t *testing.T) {
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx is unavailable; CI installs it for this validation")
	}
	dir := t.TempDir()
	cert, key := testCertificate(t)
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, cert, 0600)
	os.WriteFile(keyPath, key, 0600)
	root := filepath.Join(dir, "nginx.conf")
	original := []byte("error_log " + filepath.Join(dir, "error.log") + "; pid " + filepath.Join(dir, "nginx.pid") + "; events {} http { access_log off; server { listen 443 ssl; server_name show.example.com; ssl_certificate " + certPath + "; ssl_certificate_key " + keyPath + "; location / { return 200; } } }")
	hosts, err := suitableHosts(root, map[string][]byte{root: original})
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts: %v %v", hosts, err)
	}
	updated, err := insertNginx(hosts[0])
	if err != nil {
		t.Fatal(err)
	}
	// nginx 1.18 also checks binding during -t. Use an ephemeral loopback
	// listener in this isolated validation; never bind a public privileged port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	updated = bytes.Replace(updated, []byte("listen 443 ssl;"), []byte("listen "+address+" ssl;"), 1)
	os.WriteFile(root, updated, 0600)
	command := exec.Command(nginx, "-t", "-p", dir, "-c", root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("nginx rejected generated configuration: %v\n%s", err, output)
	}
}

func TestGeneratedCaddyConfigWithRealValidator(t *testing.T) {
	caddy, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("caddy is unavailable; CI installs it for this validation")
	}
	data, err := caddyConfiguration([]byte("{\n admin off\n}\n"), "show.example.com")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Caddyfile")
	os.WriteFile(path, data, 0600)
	command := exec.Command(caddy, "adapt", "--config", path, "--adapter", "caddyfile", "--validate")
	command.Env = append(os.Environ(), "XDG_DATA_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("caddy rejected generated configuration: %v\n%s", err, output)
	}
}

func testCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "show.example.com"}, DNSNames: []string{"show.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestSystemdIsUnprivilegedAndSecretNotInArgs(t *testing.T) {
	for _, value := range []string{"User=smartstage-gateway", "Group=smartstage-gateway", "NoNewPrivileges=true", "ProtectSystem=strict", "--config /etc/smartstage-gateway/config.json"} {
		if !strings.Contains(systemdUnit, value) {
			t.Errorf("missing %s", value)
		}
	}
	if strings.Contains(systemdUnit, "--token") {
		t.Fatal("secret passed on process command line")
	}
}

func TestBootstrapChecksDownloadedBytesBeforeExecution(t *testing.T) {
	bootstrap, err := filepath.Abs(filepath.Join("..", "..", "install-gateway.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupt=%v", corrupt), func(t *testing.T) {
			dir := t.TempDir()
			payload := []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SMARTSTAGE_BOOTSTRAP_MARKER\"\n")
			binary := filepath.Join(dir, "payload")
			checksum := filepath.Join(dir, "checksum")
			marker := filepath.Join(dir, "executed")
			if err := os.WriteFile(binary, payload, 0700); err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(payload))
			if corrupt {
				hash = strings.Repeat("0", 64)
			}
			if err := os.WriteFile(checksum, []byte(hash+"  payload\n"), 0600); err != nil {
				t.Fatal(err)
			}
			curl := `#!/bin/sh
set -eu
url= output=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) shift; output=$1 ;;
        https://*) url=$1 ;;
    esac
    shift
done
case "$url" in
    *.sha256) cp "$SMARTSTAGE_BOOTSTRAP_CHECKSUM" "$output" ;;
    *) cp "$SMARTSTAGE_BOOTSTRAP_BINARY" "$output" ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(curl), 0700); err != nil {
				t.Fatal(err)
			}
			// No elevation occurs: the fixture simply runs the verified no-op
			// payload, and never invokes the real installer or sudo.
			if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\nexec \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("/bin/sh", bootstrap, "--list-hosts")
			command.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "SMARTSTAGE_BOOTSTRAP_MARKER="+marker, "SMARTSTAGE_BOOTSTRAP_CHECKSUM="+checksum, "SMARTSTAGE_BOOTSTRAP_BINARY="+binary)
			output, err := command.CombinedOutput()
			if corrupt {
				if err == nil || !bytes.Contains(output, []byte("failed SHA-256 verification")) {
					t.Fatalf("corrupt download accepted: %v %s", err, output)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("corrupt download was executed")
				}
			} else {
				if err != nil {
					t.Fatalf("valid bootstrap: %v %s", err, output)
				}
				got, err := os.ReadFile(marker)
				if err != nil || string(got) != "install\n--list-hosts\n" {
					t.Fatalf("arguments were not forwarded: %q %v", got, err)
				}
			}
		})
	}
}
