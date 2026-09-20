package gatewayinstall

import (
	"bytes"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
)

const beginMarker = "# BEGIN SMART STAGE GATEWAY (managed)"
const endMarker = "# END SMART STAGE GATEWAY (managed)"

type nginxToken struct {
	value       string
	start, end  int
	punctuation bool
}
type nginxNode struct {
	words        []string
	children     []*nginxNode
	source       string
	start, close int
}
type nginxHost struct {
	Name, Source string
	Close        int
	Data         []byte
	Managed      bool
}

// nginxTokens retains byte offsets, while honoring quoted strings, escapes,
// comments and ${variable} expressions. A malformed source is never edited.
func nginxTokens(data []byte) ([]nginxToken, error) {
	var out []nginxToken
	for i := 0; i < len(data); {
		if strings.ContainsRune(" \t\r\n", rune(data[i])) {
			i++
			continue
		}
		if data[i] == '#' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		if strings.ContainsRune("{};", rune(data[i])) {
			out = append(out, nginxToken{string(data[i]), i, i + 1, true})
			i++
			continue
		}
		var word strings.Builder
		var quote byte
		for i < len(data) {
			c := data[i]
			if c == '\\' {
				if i+1 == len(data) {
					return nil, fmt.Errorf("trailing escape at byte %d", i)
				}
				word.WriteByte(data[i+1])
				i += 2
				continue
			}
			if quote != 0 {
				if c == quote {
					quote = 0
				} else {
					word.WriteByte(c)
				}
				i++
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				i++
				continue
			}
			if c == '$' && i+1 < len(data) && data[i+1] == '{' {
				end := bytes.IndexByte(data[i+2:], '}')
				if end < 0 {
					return nil, fmt.Errorf("unterminated variable at byte %d", i)
				}
				word.Write(data[i : i+end+3])
				i += end + 3
				continue
			}
			if strings.ContainsRune(" \t\r\n{};#", rune(c)) {
				break
			}
			word.WriteByte(c)
			i++
		}
		if quote != 0 {
			return nil, fmt.Errorf("unterminated quote at byte %d", start)
		}
		out = append(out, nginxToken{word.String(), start, i, false})
	}
	return out, nil
}

func parseNginx(source string, data []byte) ([]*nginxNode, error) {
	tokens, err := nginxTokens(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	i := 0
	var parse func(bool) ([]*nginxNode, int, error)
	parse = func(nested bool) ([]*nginxNode, int, error) {
		var nodes []*nginxNode
		for i < len(tokens) {
			if tokens[i].punctuation && tokens[i].value == "}" {
				if !nested {
					return nil, 0, fmt.Errorf("unexpected closing brace")
				}
				close := tokens[i].start
				i++
				return nodes, close, nil
			}
			n := &nginxNode{source: source, start: tokens[i].start, close: -1}
			for i < len(tokens) && !tokens[i].punctuation {
				n.words = append(n.words, tokens[i].value)
				i++
			}
			if len(n.words) == 0 || i == len(tokens) || tokens[i].value == "}" {
				return nil, 0, fmt.Errorf("unfinished directive near byte %d", n.start)
			}
			if tokens[i].punctuation && tokens[i].value == "{" {
				i++
				n.children, n.close, err = parse(true)
				if err != nil {
					return nil, 0, err
				}
			} else {
				i++
			}
			nodes = append(nodes, n)
		}
		if nested {
			return nil, 0, fmt.Errorf("unclosed block")
		}
		return nodes, 0, nil
	}
	nodes, _, err := parse(false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return nodes, nil
}

// nginx -T lists every effective source once. Read the actual source bytes for
// offsets; never try to rewrite its diagnostic/configuration dump.
func nginxSources(dump string) ([]string, error) {
	var paths []string
	seen := map[string]bool{}
	for _, line := range strings.Split(dump, "\n") {
		if !strings.HasPrefix(line, "# configuration file ") || !strings.HasSuffix(line, ":") {
			continue
		}
		path := strings.TrimSuffix(strings.TrimPrefix(line, "# configuration file "), ":")
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return nil, fmt.Errorf("unsupported nginx configuration source %q", path)
		}
		path = filepath.Clean(path)
		if !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("nginx -T did not report configuration sources")
	}
	return paths, nil
}

func suitableHosts(root string, files map[string][]byte) ([]nginxHost, error) {
	parsed := map[string][]*nginxNode{}
	for name, data := range files {
		nodes, err := parseNginx(name, data)
		if err != nil {
			return nil, err
		}
		parsed[name] = nodes
	}
	var expand func([]*nginxNode, map[string]bool, int) ([]*nginxNode, error)
	expand = func(nodes []*nginxNode, stack map[string]bool, depth int) ([]*nginxNode, error) {
		if depth > 64 {
			return nil, fmt.Errorf("nginx includes exceed safe nesting limit")
		}
		var result []*nginxNode
		for _, node := range nodes {
			if node.words[0] != "include" {
				copy := *node
				var err error
				copy.children, err = expand(node.children, stack, depth+1)
				if err != nil {
					return nil, err
				}
				result = append(result, &copy)
				continue
			}
			if len(node.words) != 2 || strings.Contains(node.words[1], "$") {
				return nil, fmt.Errorf("unsupported include in %s", node.source)
			}
			pattern := node.words[1]
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(filepath.Dir(root), pattern)
			}
			var matches []string
			for name := range files {
				ok, err := filepath.Match(pattern, name)
				if err != nil {
					return nil, err
				}
				if ok {
					matches = append(matches, name)
				}
			}
			sort.Strings(matches)
			if len(matches) == 0 && !strings.ContainsAny(pattern, "*?[") {
				return nil, fmt.Errorf("include %s is absent from nginx -T; refusing an ambiguous edit", pattern)
			}
			for _, name := range matches {
				if stack[name] {
					return nil, fmt.Errorf("recursive nginx include %s", name)
				}
				stack[name] = true
				expanded, err := expand(parsed[name], stack, depth+1)
				delete(stack, name)
				if err != nil {
					return nil, err
				}
				result = append(result, expanded...)
			}
		}
		return result, nil
	}
	nodes, err := expand(parsed[root], map[string]bool{root: true}, 0)
	if err != nil {
		return nil, err
	}
	var hosts []nginxHost
	for _, http := range nodes {
		if http.words[0] != "http" {
			continue
		}
		for _, server := range http.children {
			if server.words[0] != "server" || server.close < 0 {
				continue
			}
			var tls bool
			var blocked bool
			var names []string
			for _, child := range server.children {
				switch child.words[0] {
				case "listen":
					if len(child.words) < 3 {
						continue
					}
					addr := child.words[1]
					ssl := false
					for _, word := range child.words[2:] {
						if word == "ssl" {
							ssl = true
						}
					}
					if ssl && (addr == "443" || strings.HasSuffix(addr, ":443")) && !strings.Contains(addr, "127.") && !strings.HasPrefix(addr, "[::1]") {
						tls = true
					}
				case "server_name":
					names = append(names, child.words[1:]...)
				case "return", "rewrite":
					blocked = true // Executes before location selection.
				case "ssl_reject_handshake":
					if len(child.words) > 1 && child.words[1] == "on" {
						blocked = true
					}
				}
			}
			if !tls || blocked {
				continue
			}
			data := files[server.source]
			managed, conflict := locationConflict(server, data)
			if conflict {
				continue
			}
			for _, name := range names {
				if validHostname(name) {
					hosts = append(hosts, nginxHost{name, server.source, server.close, data, managed})
				}
			}
		}
	}
	counts := map[string]int{}
	for _, host := range hosts {
		counts[host.Name]++
	}
	var unique []nginxHost
	for _, host := range hosts {
		if counts[host.Name] == 1 {
			unique = append(unique, host)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].Name < unique[j].Name })
	return unique, nil
}

func locationConflict(server *nginxNode, data []byte) (managed, conflict bool) {
	block := data[server.start:server.close]
	begin, end := bytes.Index(block, []byte(beginMarker)), bytes.Index(block, []byte(endMarker))
	if begin >= 0 || end >= 0 {
		if begin < 0 || end < begin || bytes.Count(block, []byte(beginMarker)) != 1 || bytes.Count(block, []byte(endMarker)) != 1 {
			return false, true
		}
		managed = true
		begin += server.start
		end += server.start + len(endMarker)
	}
	var walk func([]*nginxNode)
	walk = func(nodes []*nginxNode) {
		for _, child := range nodes {
			if child.words[0] == "location" {
				for _, word := range child.words[1:] {
					if strings.Contains(word, "smartstage") && !(managed && child.source == server.source && child.start >= begin && child.start < end) {
						conflict = true
					}
				}
			}
			walk(child.children)
		}
	}
	walk(server.children)
	return managed, conflict
}

func validHostname(name string) bool {
	if len(name) > 253 || !strings.Contains(name, ".") || net.ParseIP(name) != nil || strings.ContainsAny(name, "/:*~$_\\ \t\r\n") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

const nginxProxy = `
    # BEGIN SMART STAGE GATEWAY (managed)
    location = /smartstage { return 308 /smartstage/; }
    location ^~ /smartstage/ {
        proxy_pass http://127.0.0.1:8790;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_cache off;
        proxy_read_timeout 75s;
        proxy_send_timeout 75s;
        client_max_body_size 2m;
        access_log off;
    }
    # END SMART STAGE GATEWAY (managed)
`

func insertNginx(host nginxHost) ([]byte, error) {
	if host.Close < 0 || host.Close >= len(host.Data) || host.Data[host.Close] != '}' {
		return nil, fmt.Errorf("nginx source changed")
	}
	if host.Managed {
		return nil, nil
	}
	result := append([]byte{}, host.Data[:host.Close]...)
	result = append(result, nginxProxy...)
	result = append(result, host.Data[host.Close:]...)
	return result, nil
}
