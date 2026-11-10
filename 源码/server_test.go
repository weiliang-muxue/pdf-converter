package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T) *application {
	return &application{token: "unit-test-token-123456", root: t.TempDir(), results: map[string]result{}, busy: make(chan struct{}, 1), stop: func() {}}
}

func TestAuthAndRequestIsolation(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct {
		method, path, token, origin string
		status                      int
	}{
		{"GET", "/api/status", "", "", 401}, {"GET", "/api/status", a.token, "", 200},
		{"GET", "/api/status", a.token, "https://evil.example", 403}, {"POST", "/api/status", a.token, "", 405},
		{"GET", "/api/download?id=../../etc/passwd&token=" + a.token, "", "", 404}, {"GET", "/private.txt", "", "", 404},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-PDF-Token", tc.token)
		req.Header.Set("Origin", tc.origin)
		rec := httptest.NewRecorder()
		a.handler().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s: got %d want %d", tc.path, rec.Code, tc.status)
		}
	}
}

func TestHostDetectionStatus(t *testing.T) {
	t.Setenv("PDF_HOST_OS", "openEuler 24.03 LTS SP2")
	t.Setenv("PDF_HOST_ARCH", "aarch64")
	t.Setenv("PDF_ENGINE_VARIANT", "Linux ARM64")
	a := testApp(t)
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("X-PDF-Token", a.token)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)
	var data map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["system"] != "openEuler 24.03 LTS SP2" || data["architecture"] != "aarch64" || data["engineTarget"] != "Linux ARM64" {
		t.Fatal(data)
	}
	if strings.Contains(rec.Body.String(), a.token) {
		t.Fatal("status exposed private token")
	}
	t.Setenv("PDF_HOST_OS", "")
	if hostLabel("PDF_HOST_OS", "linux") != "linux" {
		t.Fatal("missing metadata fallback")
	}
}

func imageRequest(t *testing.T, a *application, valid bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("kind", "images")
	form.WriteField("paper", "a4")
	form.WriteField("orientation", "auto")
	form.WriteField("margin", "10")
	part, _ := form.CreateFormFile("files", "../../照片.png")
	if valid {
		path := pngFixture(t, t.TempDir())
		f, _ := os.Open(path)
		io.Copy(part, f)
		f.Close()
	} else {
		part.Write([]byte("invalid image"))
	}
	form.Close()
	req := httptest.NewRequest("POST", "/api/convert", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-PDF-Token", a.token)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)
	return rec
}

func TestUploadDownloadAndCleanup(t *testing.T) {
	a := testApp(t)
	rec := imageRequest(t, a, true)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	item, ok := a.results[response.ID]
	if !ok {
		t.Fatal("no result")
	}
	entries, _ := os.ReadDir(filepath.Dir(item.Path))
	if len(entries) != 1 || entries[0].Name() != "result.pdf" {
		t.Fatal("inputs not cleaned")
	}
	req := httptest.NewRequest("GET", "/api/download?id="+response.ID+"&token="+a.token, nil)
	download := httptest.NewRecorder()
	a.handler().ServeHTTP(download, req)
	if download.Code != http.StatusOK || !strings.HasPrefix(download.Body.String(), "%PDF-") || !strings.Contains(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("invalid download")
	}
	// Nginx injects the private token as a header; the browser never receives it.
	req = httptest.NewRequest("GET", "/api/download?id="+response.ID, nil)
	req.Header.Set("X-PDF-Token", a.token)
	req.Host = "intranet.example:8080"
	req.Header.Set("Origin", "http://intranet.example:8080")
	download = httptest.NewRecorder()
	a.handler().ServeHTTP(download, req)
	if download.Code != http.StatusOK || !strings.HasPrefix(download.Body.String(), "%PDF-") {
		t.Fatal("reverse proxy header authentication failed")
	}
	req = httptest.NewRequest("GET", "/api/download?id="+response.ID+"&token="+a.token, nil)
	req.Header.Set("X-PDF-Token", "wrong-header")
	download = httptest.NewRecorder()
	a.handler().ServeHTTP(download, req)
	if download.Code != http.StatusUnauthorized {
		t.Fatal("invalid supplied header must not fall back to a query token")
	}
	before, _ := os.ReadDir(a.root)
	rec = imageRequest(t, a, false)
	after, _ := os.ReadDir(a.root)
	if rec.Code != 400 || len(before) != len(after) {
		t.Fatal("failed upload leaked files")
	}
	a.busy <- struct{}{}
	rec = imageRequest(t, a, true)
	<-a.busy
	if rec.Code != 429 {
		t.Fatal("missing concurrency limit")
	}
}

func TestWordValidationAndServerExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.docx")
	os.WriteFile(path, []byte("not zip"), 0600)
	if validateWord(path) == nil {
		t.Fatal("invalid word accepted")
	}
	path = filepath.Join(t.TempDir(), "fake.doc")
	os.WriteFile(path, []byte("bad document"), 0600)
	if validateWord(path) == nil {
		t.Fatal("invalid doc accepted")
	}
	a := testApp(t)
	a.serverMode = true
	req := httptest.NewRequest("POST", "/api/exit", nil)
	req.Header.Set("X-PDF-Token", a.token)
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("server exit must be refused")
	}
}
