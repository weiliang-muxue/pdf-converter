package main

import (
	"archive/zip"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Generated from html/网站文件 by tools/sync_container_web.py.
//
//go:embed standaloneweb/*
var standaloneAssets embed.FS

func (a *application) wordReady() bool {
	if !a.standalone {
		return a.office != ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.standaloneReady
}

func (a *application) standaloneHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, "GET") {
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, "GET") {
			return
		}
		a.mu.Lock()
		draining := a.draining
		a.mu.Unlock()
		if draining || !a.wordReady() {
			fail(w, 503, "Word 引擎正在初始化、初始化失败或服务正在退出")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/word-api/status", a.sameOrigin(a.status))
	mux.HandleFunc("/word-api/archive", a.sameOrigin(a.importArchive))
	mux.HandleFunc("/word-api/render", a.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		if !a.wordReady() {
			fail(w, 503, "Word 引擎尚未就绪")
			return
		}
		a.convertWithDelivery(w, r, true)
	}))
	web, _ := fs.Sub(standaloneAssets, "standaloneweb")
	files := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html", "/assets/style.css", "/assets/app.js", "/assets/core.js", "/assets/jszip.min.js", "/assets/JSZIP-LICENSE.txt":
			if r.Method != "GET" && r.Method != "HEAD" {
				w.Header().Set("Allow", "GET, HEAD")
				fail(w, 405, "请求方法不支持")
				return
			}
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}

// Explicit intranet mode, without login. Cross-site POSTs require a preflight
// that this handler never grants. Legacy token APIs are not exposed here.
func (a *application) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, "不允许跨站请求")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !strings.EqualFold(u.Host, r.Host) {
				fail(w, 403, "不允许跨站请求")
				return
			}
		}
		if r.Method == "POST" && r.Header.Get("X-PDF-Request") != "1" {
			fail(w, 403, "请从本工具网页提交文件")
			return
		}
		next(w, r)
	}
}

func writeProbeWord(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	entries := []struct{ name, text string }{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>PDF converter readiness</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`},
	}
	for _, item := range entries {
		w, e := z.Create(item.name)
		if e == nil {
			_, e = w.Write([]byte(item.text))
		}
		if e != nil {
			z.Close()
			f.Close()
			return e
		}
	}
	zerr := z.Close()
	ferr := f.Close()
	if zerr != nil {
		return zerr
	}
	return ferr
}

func (a *application) initializeStandalone(ctx context.Context) {
	err := a.probeOffice(ctx)
	if err != nil {
		log.Printf("Word startup conversion failed: %v", err)
		return
	}
	a.mu.Lock()
	a.standaloneReady = true
	a.mu.Unlock()
	log.Print("Word startup conversion passed; ready on /readyz")
}

func (a *application) probeOffice(ctx context.Context) error {
	if a.office == "" {
		return fmt.Errorf("bundled Word engine not found")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp(a.root, "startup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "probe.docx")
	if err = writeProbeWord(path); err != nil {
		return err
	}
	_, _, err = wordPDF(ctx, a.office, []string{path}, []string{"probe.docx"}, dir)
	return err
}
