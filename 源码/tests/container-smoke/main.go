// Independent test client; not included in released images.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

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
func main() {
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
	result := map[string]interface{}{"pdf_bytes": len(pdf), "direct_response": true, "legacy_doc": true, "temporary_files_cleaned": true, "cancel_cleanup": true, "offline_container": true, "version": status["version"], "architecture": status["architecture"]}
	text, _ := json.Marshal(result)
	fmt.Println("PASS:", string(text))
}
