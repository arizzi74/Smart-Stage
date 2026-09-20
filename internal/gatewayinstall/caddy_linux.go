package gatewayinstall

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func caddyConfiguration(existing []byte, domain string) ([]byte, error) {
	if !validHostname(domain) {
		return nil, fmt.Errorf("invalid Caddy hostname")
	}
	// Caddy flushes text/event-stream responses automatically. Do not set a
	// negative flush_interval: older Caddy versions then keep the upstream
	// request alive after the phone disconnects, leaking its stream slot.
	block := fmt.Sprintf(`%s
%s {
    @smartstage path /smartstage /smartstage/*
    handle @smartstage {
        reverse_proxy 127.0.0.1:8790 {
            transport http {
                read_timeout 75s
                write_timeout 75s
            }
        }
    }
    respond 404
}
%s
`, beginMarker, domain, endMarker)
	if bytes.Contains(existing, []byte(beginMarker)) || bytes.Contains(existing, []byte(endMarker)) {
		if bytes.Count(existing, []byte(beginMarker)) != 1 || bytes.Count(existing, []byte(endMarker)) != 1 || !bytes.Contains(existing, []byte(block)) {
			return nil, fmt.Errorf("the existing managed Caddy block differs; review /etc/caddy/Caddyfile manually before reinstalling")
		}
		return existing, nil
	}
	// A domain already mentioned may be a site label, matcher or an imported
	// snippet argument. Do not guess where an existing Caddy site should change.
	if bytes.Contains(bytes.ToLower(existing), []byte(strings.ToLower(domain))) {
		return nil, fmt.Errorf("Caddyfile already mentions %s; configure the Smart Stage reverse proxy inside that site manually", domain)
	}
	result := append([]byte{}, existing...)
	result = append(result, '\n')
	result = append(result, block...)
	return result, nil
}

func (i *installer) installCaddy() error {
	if _, err := exec.LookPath("apt-get"); err == nil {
		fmt.Fprintln(i.output, "Installing Caddy using its official Debian/Ubuntu package repository…")
		for _, args := range [][]string{{"update"}, {"install", "-y", "debian-keyring", "debian-archive-keyring", "apt-transport-https", "curl", "gnupg"}} {
			if _, err := i.run("apt-get", args...); err != nil {
				return err
			}
		}
		dir, err := os.MkdirTemp("", "smartstage-caddy-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		key := filepath.Join(dir, "caddy.key")
		list := filepath.Join(dir, "caddy.list")
		keyring := filepath.Join(dir, "caddy.gpg")
		for _, download := range []struct{ url, path string }{{"https://dl.cloudsmith.io/public/caddy/stable/gpg.key", key}, {"https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt", list}} {
			if _, err := i.run("curl", "--fail", "--show-error", "--silent", "--location", "--proto", "=https", "--tlsv1.2", download.url, "--output", download.path); err != nil {
				return err
			}
		}
		if _, err := i.run("gpg", "--batch", "--yes", "--dearmor", "--output", keyring, key); err != nil {
			return err
		}
		for _, file := range []struct{ source, dest string }{{keyring, "/usr/share/keyrings/caddy-stable-archive-keyring.gpg"}, {list, "/etc/apt/sources.list.d/caddy-stable.list"}} {
			data, err := os.ReadFile(file.source)
			if err != nil {
				return err
			}
			if err := atomicFile(file.dest, data, 0644, 0, 0); err != nil {
				return err
			}
		}
		if _, err := i.run("apt-get", "update"); err != nil {
			return err
		}
		_, err = i.run("apt-get", "install", "-y", "caddy")
		return err
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		plugin := "dnf-plugins-core"
		if data, err := os.ReadFile("/etc/os-release"); err == nil && (strings.Contains(string(data), "ID=fedora\n") || strings.Contains(string(data), "ID=\"fedora\"\n")) {
			plugin = "dnf5-plugins"
		}
		fmt.Fprintln(i.output, "Installing Caddy using its official Fedora/RHEL COPR package repository…")
		for _, args := range [][]string{{"install", "-y", plugin}, {"copr", "enable", "-y", "@caddy/caddy"}, {"install", "-y", "caddy"}} {
			if _, err := i.run("dnf", args...); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("automatic Caddy installation supports apt-get and dnf; install Caddy from https://caddyserver.com/docs/install then rerun this installer")
}
