// Independent test client; not included in released images.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed fixtures/*
var archiveFixtures embed.FS

const base = "http://127.0.0.1:8080"

var client = &http.Client{Timeout: 150 * time.Second, Transport: &http.Transport{Proxy: nil}}

func must(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func get(path string) (int, []byte) {
	r, e := client.Get(base + path)
	if e != nil {
		return 0, []byte(e.Error())
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		panic(e)
	}
	return r.StatusCode, b
}
func docx() []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	entries := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Offline verification / 中文转换测试</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`,
	}
	for name, text := range entries {
		w, e := z.Create(name)
		if e != nil {
			panic(e)
		}
		w.Write([]byte(text))
	}
	if e := z.Close(); e != nil {
		panic(e)
	}
	return b.Bytes()
}
func body(doc []byte, name string) ([]byte, string) {
	var b bytes.Buffer
	f := multipart.NewWriter(&b)
	f.WriteField("kind", "word")
	w, _ := f.CreateFormFile("files", name)
	w.Write(doc)
	f.Close()
	return b.Bytes(), f.FormDataContentType()
}
func jobs() []string {
	d, _ := filepath.Glob(filepath.Join(os.TempDir(), "pdf-tool-*", "job-*"))
	return d
}
func clean() {
	for i := 0; i < 100; i++ {
		if len(jobs()) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic("retained job files")
}
func render(doc []byte, name string) (int, []byte) {
	payload, typ := body(doc, name)
	req, _ := http.NewRequest("POST", base+"/word-api/render", bytes.NewReader(payload))
	req.Header.Set("Content-Type", typ)
	req.Header.Set("X-PDF-Request", "1")
	r, e := client.Do(req)
	if e != nil {
		panic(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		panic(e)
	}
	return r.StatusCode, b
}
func archiveChecks() {
	for _, name := range []string{"photos-rar4.rar", "photos-rar5.rar", "photos.7z", "photos.zip", "encrypted.7z"} {
		raw, err := archiveFixtures.ReadFile("fixtures/" + name)
		must(err == nil, "fixture missing")
		var b bytes.Buffer
		m := multipart.NewWriter(&b)
		f, _ := m.CreateFormFile("files", name)
		f.Write(raw)
		m.Close()
		req, _ := http.NewRequest("POST", base+"/word-api/archive", &b)
		req.Header.Set("Content-Type", m.FormDataContentType())
		req.Header.Set("X-PDF-Request", "1")
		resp, err := client.Do(req)
		must(err == nil, fmt.Sprint(err))
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		must(err == nil, "archive response failed")
		if name == "encrypted.7z" {
			must(resp.StatusCode == 400, "encrypted archive accepted")
			continue
		}
		if resp.StatusCode != 200 {
			dir, err := os.MkdirTemp("", "archive-diagnostic-")
			must(err == nil, "diagnostic temp")
			defer os.RemoveAll(dir)
			input := filepath.Join(dir, "fixture.rar")
			os.WriteFile(input, raw, 0600)
			for _, limited := range []bool{false, true} {
				for _, mode := range []string{"l", "x"} {
					command := "/opt/pdf-tool/7zzs"
					args := []string{}
					if limited {
						command = "/opt/pdf-tool/pdf-tool"
						args = append(args, "--archive-worker")
					}
					args = append(args, mode, "-trar", "-sccUTF-8", "-p-", "-y", "-mmt=1")
					if mode == "x" {
						args = append(args, "-so", "-spd", "-ssc", "-bb0", "-bd")
					} else {
						args = append(args, "-slt", "-ba")
					}
					args = append(args, "--", input)
					if mode == "x" {
						args = append(args, "10.png")
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					cmd := exec.CommandContext(ctx, command, args...)
					cmd.Dir = dir
					var output, diagnostic bytes.Buffer
					cmd.Stdout = &output
					cmd.Stderr = &diagnostic
					err := cmd.Run()
					cancel()
					fmt.Printf("ARCHIVE DIAGNOSTIC limited=%v mode=%s error=%v bytes=%d stderr=%q\n", limited, mode, err, output.Len(), diagnostic.String())
					if mode == "l" {
						fmt.Println(output.String())
					}
				}
			}
		}
		must(resp.StatusCode == 200, fmt.Sprintf("archive %s HTTP %d: %s", name, resp.StatusCode, data))
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		must(err == nil, "invalid archive output")
		must(len(z.File) == 4, "wrong image count")
		wanted := map[string]bool{"1.png": true, "2.png": true, "3.png": true, "10.png": true}
		for _, item := range z.File {
			n := path.Base(item.Name)
			must(wanted[n], "unexpected/duplicate name")
			delete(wanted, n)
			r, err := item.Open()
			must(err == nil, "bad ZIP entry")
			image, err := io.ReadAll(r)
			r.Close()
			must(err == nil && bytes.HasPrefix(image, []byte{137, 80, 78, 71}), "image bytes/CRC failure")
		}
		for attempt := 0; attempt < 100; attempt++ {
			dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), "pdf-tool-*", "archive-*"))
			if len(dirs) == 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), "pdf-tool-*", "archive-*"))
		must(len(dirs) == 0, "archive state retained")
	}
	s, b := get("/")
	must(s == 200 && !strings.Contains(string(b), "YOUR FILES") && !strings.Contains(string(b), "processingBadge"), "old UI elements retained")
}

// Diagnostic comparison only: do not treat these counters as release acceptance.
func archiveDiagnostics() {
	dir, err := os.MkdirTemp("", "archive-diagnostics-")
	must(err == nil, "temp")
	defer os.RemoveAll(dir)
	raw, err := archiveFixtures.ReadFile("fixtures/photos-rar4.rar")
	must(err == nil, "fixture")
	input := filepath.Join(dir, "test.rar")
	os.WriteFile(input, raw, 0600)
	for _, launcher := range []string{"go-worker", "bounded-shell"} {
		failures := 0
		for attempt := 0; attempt < 100; attempt++ {
			args := []string{"x", "-so", "-spd", "-ssc", "-bb0", "-bd", "-trar", "-sccUTF-8", "-p-", "-y", "-mmt=1", "--", input, "10.png"}
			command := "/opt/pdf-tool/pdf-tool"
			if launcher == "go-worker" {
				args = append([]string{"--archive-worker"}, args...)
			} else {
				command = "/bin/sh"
				args = append([]string{"-c", `ulimit -v 786432 && ulimit -t 120 && exec "$@"`, "pdf-archive", "/opt/pdf-tool/7zzs"}, args...)
			}
			ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Dir = dir
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			stop()
			if err != nil || stdout.Len() != 3084 {
				failures++
				if failures < 4 {
					fmt.Printf("DECODER FAILURE launcher=%s attempt=%d err=%v bytes=%d stderr=%q\n", launcher, attempt, err, stdout.Len(), stderr.String())
				}
			}
		}
		fmt.Printf("DECODER COMPARISON launcher=%s attempts=100 failures=%d\n", launcher, failures)
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--archive-diagnostics" {
		archiveDiagnostics()
		return
	}
	ready := false
	for i := 0; i < 120; i++ {
		s, _ := get("/readyz")
		if s == 200 {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		dir, err := os.MkdirTemp("", "engine-diagnostic-")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(dir)
		input := filepath.Join(dir, "probe.docx")
		if err := os.WriteFile(input, docx(), 0600); err != nil {
			panic(err)
		}
		commands := [][]string{
			{"--headless", "--version"},
			{"-env:UserInstallation=file://" + filepath.Join(dir, "profile"), "--headless", "--nologo", "--nodefault", "--norestore", "--convert-to", "pdf:writer_pdf_Export", "--outdir", dir, input},
		}
		for _, args := range commands {
			ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			output, err := exec.CommandContext(ctx, "/usr/lib/libreoffice/program/oosplash", args...).CombinedOutput()
			stop()
			fmt.Printf("ENGINE DIAGNOSTIC %v: %v\n%s\n", args, err, output)
		}
	}
	must(ready, "startup conversion failed")
	s, b := get("/")
	must(s == 200 && bytes.Contains(b, []byte("压缩包转 PDF")) && bytes.Contains(b, []byte("Word 转 PDF")), "web missing")
	s, b = get("/assets/app.js")
	must(s == 200 && bytes.Contains(b, []byte("directDownload")), "frontend missing")
	s, b = get("/word-api/status")
	var status map[string]interface{}
	json.Unmarshal(b, &status)
	must(s == 200 && status["wordReady"] == true && status["directDownload"] == true, "status mismatch")
	archiveChecks()
	doc := docx()
	s, pdf := render(doc, "probe.docx")
	must(s == 200 && bytes.HasPrefix(pdf, []byte("%PDF-")) && len(pdf) > 1000, fmt.Sprintf("conversion: HTTP %d %s", s, pdf))
	clean()
	// Generate an actual OLE Word 97 document, then exercise the public DOC path.
	dir, e := os.MkdirTemp("", "legacy-doc-test-")
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "legacy.docx")
	if e = os.WriteFile(input, doc, 0600); e != nil {
		panic(e)
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	output, err := exec.CommandContext(ctx, "/usr/lib/libreoffice/program/oosplash",
		"-env:UserInstallation=file://"+filepath.Join(dir, "profile"), "--headless",
		"--convert-to", "doc:MS Word 97", "--outdir", dir, input).CombinedOutput()
	stop()
	must(err == nil, fmt.Sprintf("DOC fixture generation: %v %s", err, output))
	legacy, err := os.ReadFile(filepath.Join(dir, "legacy.doc"))
	must(err == nil && bytes.HasPrefix(legacy, []byte{0xd0, 0xcf, 0x11, 0xe0}), "DOC fixture invalid")
	s, legacyPDF := render(legacy, "legacy.doc")
	must(s == 200 && bytes.HasPrefix(legacyPDF, []byte("%PDF-")) && len(legacyPDF) > 1000, "legacy DOC conversion failed")
	clean()
	s, _ = get("/word-api/download?id=invalid")
	must(s == 404, "stateful download exposed")
	s, _ = get("/api/status")
	must(s == 404, "legacy API exposed")
	s, _ = render([]byte("invalid"), "probe.docx")
	must(s == 400, "invalid Word accepted")
	clean()
	payload, typ := body(doc, "probe.docx")
	conn, e := net.DialTimeout("tcp", "127.0.0.1:8080", 5*time.Second)
	if e != nil {
		panic(e)
	}
	fmt.Fprintf(conn, "POST /word-api/render HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nX-PDF-Request: 1\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n", typ, len(payload))
	conn.Write(payload)
	for i := 0; i < 200 && len(jobs()) == 0; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	must(len(jobs()) > 0, "cancel test did not reach conversion")
	conn.Close()
	clean()
	roots, _ := filepath.Glob(filepath.Join(os.TempDir(), "pdf-tool-*"))
	for _, root := range roots {
		entries, e := os.ReadDir(root)
		if e != nil {
			panic(e)
		}
		must(len(entries) == 0, "application retained files")
	}
	s, _ = get("/healthz")
	must(s == 200, "liveness failed")
	result := map[string]interface{}{"archives_zip_rar4_rar5_7z": true, "pdf_bytes": len(pdf), "direct_response": true, "legacy_doc": true, "temporary_files_cleaned": true, "cancel_cleanup": true, "offline_container": true, "version": status["version"], "architecture": status["architecture"]}
	text, _ := json.Marshal(result)
	fmt.Println("PASS:", string(text))
}
