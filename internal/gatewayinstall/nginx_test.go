//go:build linux

package gatewayinstall

import (
	"bytes"
	"strings"
	"testing"
)

func TestNginxIncludesQuotedSyntaxAndTargetedInsertion(t *testing.T) {
	root := "/etc/nginx/nginx.conf"
	site := "/etc/nginx/sites-enabled/show.conf"
	original := []byte(`server {
    listen 80;
    server_name show.example.com;
    return 301 https://$host$request_uri;
}
server {
    include snippets/tls.conf;
    server_name "show.example.com" tablet.example.com;
    # A comment must not close the server: } server { ;
    set $quoted "a # } ; { ";
    set $variable ${request_uri};
    location / { add_header X-Example '}'; proxy_pass http://127.0.0.1:8000; }
}
server { listen 443 ssl; server_name unrelated.example.com; }
`)
	files := map[string][]byte{root: []byte(`events {} http { include /etc/nginx/sites-enabled/*; }`), site: original, "/etc/nginx/snippets/tls.conf": []byte(`listen 443 ssl; ssl_certificate /cert; ssl_certificate_key /key;`)}
	hosts, err := suitableHosts(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Fatalf("hosts: %#v", hosts)
	}
	var selected nginxHost
	for _, host := range hosts {
		if host.Name == "show.example.com" {
			selected = host
		}
	}
	if selected.Source != site {
		t.Fatalf("wrong source %s", selected.Source)
	}
	updated, err := insertNginx(selected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.Replace(updated, []byte(nginxProxy), nil, 1), original) {
		t.Fatal("unrelated nginx bytes changed")
	}
	files[site] = updated
	hosts, err = suitableHosts(root, files)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range hosts {
		if host.Name == "show.example.com" {
			if !host.Managed {
				t.Fatal("managed location was not recognized")
			}
			again, err := insertNginx(host)
			if err != nil || again != nil {
				t.Fatalf("repeat install changed nginx: %v", err)
			}
		}
	}
	for _, required := range []string{"proxy_pass http://127.0.0.1:8790;", "proxy_set_header Upgrade $http_upgrade;", "proxy_set_header Connection \"upgrade\";", "proxy_buffering off;", "proxy_read_timeout 75s;", "access_log off;"} {
		if !bytes.Contains(updated, []byte(required)) {
			t.Errorf("missing proxy requirement %s", required)
		}
	}
}

func TestNginxRejectsUnsafeOrAmbiguousHosts(t *testing.T) {
	for name, config := range map[string]string{
		"plain http":                `listen 80; server_name show.example.com;`,
		"no TLS":                    `listen 443; server_name show.example.com;`,
		"loopback":                  `listen 127.0.0.1:443 ssl; server_name show.example.com;`,
		"wildcard":                  `listen 443 ssl; server_name *.example.com;`,
		"regex":                     `listen 443 ssl; server_name ~^www[.]example[.]com$;`,
		"redirect before locations": `listen 443 ssl; server_name show.example.com; return 301 https://other.example.com;`,
		"blocked TLS":               `listen 443 ssl; server_name show.example.com; ssl_reject_handshake on;`,
		"existing location":         `listen 443 ssl; server_name show.example.com; location /smartstage/ { return 200; }`,
		"nested existing location":  `listen 443 ssl; server_name show.example.com; location / { location /smartstage/ { return 200; } }`,
		"unclosed marker":           "listen 443 ssl; server_name show.example.com;\n" + beginMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			hosts, err := suitableHosts("/nginx.conf", map[string][]byte{"/nginx.conf": []byte("http { server { " + config + " } }")})
			if err != nil {
				t.Fatal(err)
			}
			if len(hosts) != 0 {
				t.Fatalf("offered unsafe host: %#v", hosts)
			}
		})
	}
	duplicate := `http { server { listen 443 ssl; server_name same.example.com; } server { listen 443 ssl; server_name same.example.com; } }`
	hosts, err := suitableHosts("/nginx.conf", map[string][]byte{"/nginx.conf": []byte(duplicate)})
	if err != nil || len(hosts) != 0 {
		t.Fatalf("duplicate host not refused: %#v %v", hosts, err)
	}
}

func TestNginxIncludedLocationConflictAndRecursiveInclude(t *testing.T) {
	files := map[string][]byte{"/nginx.conf": []byte(`http { server { listen 443 ssl; server_name show.example.com; include /location.conf; } }`), "/location.conf": []byte(`location /smartstage { return 200; }`)}
	hosts, err := suitableHosts("/nginx.conf", files)
	if err != nil || len(hosts) != 0 {
		t.Fatalf("included conflict: %#v %v", hosts, err)
	}
	files["/location.conf"] = []byte(`include /nginx.conf;`)
	if _, err := suitableHosts("/nginx.conf", files); err == nil {
		t.Fatal("recursive include accepted")
	}
	delete(files, "/location.conf")
	if _, err := suitableHosts("/nginx.conf", files); err == nil {
		t.Fatal("unresolved include accepted")
	}
}

func TestNginxSyntaxRefusesIncompleteSources(t *testing.T) {
	for _, source := range []string{`http {`, `http { server_name "broken; }`, `server_name x`, `}`, `foo \`, `set $x ${unfinished;`} {
		if _, err := parseNginx("fixture", []byte(source)); err == nil {
			t.Errorf("accepted malformed source %q", source)
		}
	}
	paths, err := nginxSources("nginx: config successful\n# configuration file /etc/nginx/nginx.conf:\nhttp {}\n# configuration file /etc/nginx/sites-enabled/site:\n")
	if err != nil || len(paths) != 2 || paths[0] != "/etc/nginx/nginx.conf" {
		t.Fatalf("sources: %v %v", paths, err)
	}
	if _, err := nginxSources("# configuration file relative:"); err == nil {
		t.Fatal("accepted relative source")
	}
}

func TestValidHostname(t *testing.T) {
	for _, name := range []string{"example.com", "stage.example.com", "xn--caf-dma.example"} {
		if !validHostname(name) {
			t.Errorf("rejected %s", name)
		}
	}
	for _, name := range []string{"example.com;evil", "example.com\nhttp://evil", "localhost", "127.0.0.1", "https://example.com", "*.example.com", "_", "-bad.example", "bad-.example", "bad..example", strings.Repeat("a", 64) + ".example"} {
		if validHostname(name) {
			t.Errorf("accepted %s", name)
		}
	}
}
