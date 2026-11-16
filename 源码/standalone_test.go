package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandaloneRoutesAndIsolation(t *testing.T) {
	a := testApp(t)
	a.standalone = true
	a.serverMode = true
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"GET", "/assets/app.js", 200}, {"GET", "/healthz", 200}, {"GET", "/readyz", 503},
		{"GET", "/api/status", 404}, {"GET", "/word-api/exit", 404}, {"GET", "/word-api/download?id=anything", 404},
		{"GET", "/etc/passwd", 404}, {"POST", "/", 405}, {"POST", "/healthz", 405},
	} {
		rec := httptest.NewRecorder()
		a.handler().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.status {
			t.Fatalf("%s %s: got %d want %d", tc.method, tc.path, rec.Code, tc.status)
		}
		if strings.Contains(rec.Body.String(), a.token) {
			t.Fatal("private token exposed")
		}
	}
	a.standaloneReady = true
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/word-api/status", nil))
	var status struct{ WordReady, DirectDownload bool }
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || !status.WordReady || !status.DirectDownload {
		t.Fatalf("status: %s", rec.Body.String())
	}
	a.draining = true
	rec = httptest.NewRecorder()
	a.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatal("draining pod still ready")
	}
}

func TestStandaloneCrossSiteRequests(t *testing.T) {
	a := testApp(t)
	a.standalone = true
	a.standaloneReady = true
	for _, tc := range []struct {
		method, origin, fetch, header string
		want                          int
	}{
		{"GET", "", "", "", 200}, {"GET", "http://example.com", "", "", 200}, {"GET", "https://example.com", "", "", 200},
		{"GET", "http://evil.example", "", "", 403}, {"GET", "null", "", "", 403},
		{"GET", "http://example.com/path", "", "", 403}, {"GET", "", "cross-site", "", 403},
		{"POST", "http://example.com", "", "", 403}, {"POST", "http://example.com", "", "1", 405},
		{"OPTIONS", "http://evil.example", "", "", 403},
	} {
		r := httptest.NewRequest(tc.method, "http://example.com/word-api/status", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.fetch)
		r.Header.Set("X-PDF-Request", tc.header)
		rec := httptest.NewRecorder()
		a.handler().ServeHTTP(rec, r)
		if rec.Code != tc.want {
			t.Fatalf("%+v: got %d", tc, rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("unexpected CORS permission")
		}
	}
}

func TestStandaloneRejectsWrongKindWithoutRetainingFiles(t *testing.T) {
	a := testApp(t)
	a.standalone = true
	a.standaloneReady = true
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("kind", "images")
	part, _ := form.CreateFormFile("files", "image.png")
	part.Write([]byte("fake"))
	form.Close()
	r := httptest.NewRequest("POST", "/word-api/render", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-PDF-Request", "1")
	rec := httptest.NewRecorder()
	a.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatal(rec.Code)
	}
	entries, _ := os.ReadDir(a.root)
	if len(entries) != 0 || len(a.results) != 0 {
		t.Fatal("temporary files or result IDs retained")
	}
}

func TestStartupProbeDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.docx")
	if err := writeProbeWord(path); err != nil {
		t.Fatal(err)
	}
	if err := validateWord(path); err != nil {
		t.Fatal(err)
	}
}
