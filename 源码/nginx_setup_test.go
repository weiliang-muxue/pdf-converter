package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nginxFixture(t *testing.T) ([]string, string) {
	t.Helper()
	root := t.TempDir()
	web := filepath.Join(root, "website space")
	if err := os.MkdirAll(filepath.Join(web, "pdf"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "pdf", "index.html"), []byte("<meta "+htmlMarker+">"), 0644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "nginx.conf")
	site := filepath.Join(root, "site.conf")
	os.WriteFile(main, []byte("events {}\nhttp { include site.conf; }"), 0644)
	os.WriteFile(site, []byte("# braces in comments { }\nserver { listen 8080; location / { root \""+filepath.ToSlash(web)+"\"; } }"), 0644)
	return []string{main, site}, root
}
func TestNginxDiscoveryAndIdempotence(t *testing.T) {
	files, root := nginxFixture(t)
	sites, sources, err := discoverNginxSites(files, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || !sites[0].prefixes["/pdf/"] {
		t.Fatal(sites)
	}
	options := nginxOptions{token: "test-token-with-at-least-24-characters", port: 8787}
	template := `location = /word-api/status { proxy_set_header X-PDF-Token "__WORD_TOKEN__"; proxy_pass http://127.0.0.1:8787/api/status; }`
	changed, err := nginxManagedEdits(sites, sources, template, options)
	if err != nil {
		t.Fatal(err)
	}
	for file, data := range changed {
		if err = os.WriteFile(file, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	sites, sources, err = discoverNginxSites(files, root, "")
	if err != nil {
		t.Fatal(err)
	}
	changed, err = nginxManagedEdits(sites, sources, template, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range changed {
		if strings.Count(string(data), nginxBegin) != 1 || strings.Count(string(data), "location = /pdf/word-api/status") != 1 {
			t.Fatal(string(data))
		}
		if _, err = parseNginx(data, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = discoverNginxSites(files, root, "/missing/"); err == nil {
		t.Fatal("must refuse a missing deployed website")
	}
}
func TestNginxDoesNotOverwriteUnmanagedRoutes(t *testing.T) {
	files, root := nginxFixture(t)
	data, _ := os.ReadFile(files[1])
	data = []byte(strings.Replace(string(data), "listen 8080;", "listen 8080; location /pdf/word-api/ { return 403; }", 1))
	os.WriteFile(files[1], data, 0644)
	sites, sources, err := discoverNginxSites(files, root, "")
	if err == nil {
		_, err = nginxManagedEdits(sites, sources, "template", nginxOptions{})
	}
	if err == nil {
		t.Fatal("must not overwrite unmanaged API routes")
	}
	current, _ := os.ReadFile(files[1])
	if string(data) != string(current) {
		t.Fatal("discovery modified configuration")
	}
}
func TestNginxAliasAndDumpPaths(t *testing.T) {
	files, root := nginxFixture(t)
	source := `server { location /documents/ { alias "` + filepath.ToSlash(filepath.Join(root, "website space", "pdf")) + `/"; } }`
	os.WriteFile(files[1], []byte(source), 0644)
	sites, _, err := discoverNginxSites(files, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !sites[0].prefixes["/documents/"] {
		t.Fatal(sites)
	}
	dump := []byte("nginx config test success\r\n# configuration file E:/nginx/conf/nginx.conf:\r\n# comment\n# configuration file /etc/nginx/site.conf:\n")
	parsed := nginxFiles(dump)
	if len(parsed) != 2 || parsed[0] != "E:/nginx/conf/nginx.conf" {
		t.Fatal(parsed)
	}
}

func TestNginxWritePreservesFileIdentity(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nginx.conf")
	os.WriteFile(file, []byte("old config longer than replacement"), 0600)
	before, _ := os.Stat(file)
	if err := writeNginxFile(file, []byte("new")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(file)
	content, _ := os.ReadFile(file)
	if !os.SameFile(before, after) || before.Mode() != after.Mode() || string(content) != "new" {
		t.Fatal("configuration identity/permissions/truncation changed")
	}
}

func TestNginxRegexEscapes(t *testing.T) {
	nodes, err := parseNginx([]byte(`location ~ /\. { deny all; }`), "test.conf")
	if err != nil {
		t.Fatal(err)
	}
	if got := nodes[0].args[1]; got != `/\.` {
		t.Fatalf("regex changed: %q", got)
	}
}

func TestNginxLocationPolicyIntersection(t *testing.T) {
	tests := []struct {
		name, locations string
		blocked         bool
	}{
		{"unrelated health", `location /health { return 200; }`, false},
		{"unrelated dotfiles", `location ~ /\. { deny all; }`, false},
		{"unrelated admin", `location /admin/ { deny all; }`, false},
		{"nested health", `location / { location /health { return 200; } }`, false},
		{"PDF auth", `location /pdf/ { auth_basic "private"; }`, true},
		{"PDF deny", `location /pdf/ { deny all; }`, true},
		{"API deny", `location /pdf/word-api/ { deny all; }`, true},
		{"exact HTML auth", `location = /pdf/index.html { auth_basic "private"; }`, true},
		{"HTML regex", `location ~ \.html$ { deny all; }`, true},
		{"case insensitive", `location ~* ^/PDF/ { deny all; }`, true},
		{"root auth", `location / { auth_basic "private"; }`, true},
		{"unsupported PCRE", `location ~ ^/pdf/(?=index) { deny all; }`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files, root := nginxFixture(t)
			web := filepath.ToSlash(filepath.Join(root, "website space"))
			source := `server { listen 8080; root "` + web + `"; ` + test.locations + ` }`
			if err := os.WriteFile(files[1], []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
			sites, _, err := discoverNginxSites(files, root, "")
			if test.blocked {
				if err == nil || !strings.Contains(err.Error(), "访问控制") {
					t.Fatalf("expected policy conflict, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) != 1 || !sites[0].prefixes["/pdf/"] {
				t.Fatal("PDF website not found")
			}
		})
	}
}
