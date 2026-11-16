package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveListingLimits(t *testing.T) {
	for _, text := range []string{
		"Path = ../1.png\nSize = 1", "Path = /1.png\nSize = 1", "Path = C:\\1.png\nSize = 1",
		"Path = 1.png\nSize = 52428801", "Path = 1.png\nSize = -1", "Path = 1.png\nSize = 1\nEncrypted = +",
		"Path = 1.png\nSize = 1\nSymbolic Link = /etc/passwd", "Path = 1.png\nSize = 1\n\nPath = 1.png\nSize = 1",
		"Path = 1.png\nSize = 1\nSize = 2", "Path = 1.png\ninjected\nSize = 1", "Path = 1.png\nSize = 1\nSplit After = +",
	} {
		if _, err := parseArchiveList([]byte(text)); err == nil {
			t.Fatalf("accepted: %s", text)
		}
	}
}
func TestArchiveRealEngineAndCleanup(t *testing.T) {
	if os.Getenv("PDF_TOOL_ARCHIVER") == "" {
		t.Skip("set PDF_TOOL_ARCHIVER to run real archive fixtures")
	}
	a := testApp(t)
	a.standalone = true
	for _, name := range []string{"photos-rar4.rar", "photos-rar5.rar", "photos.7z", "photos.zip", "encrypted.7z", "broken.rar"} {
		data := []byte("broken archive")
		if name != "broken.rar" {
			var err error
			data, err = os.ReadFile(filepath.Join("tests/container-smoke/fixtures", name))
			if err != nil {
				t.Fatal(err)
			}
		}
		var b bytes.Buffer
		m := multipart.NewWriter(&b)
		f, _ := m.CreateFormFile("files", name)
		f.Write(data)
		m.Close()
		r := httptest.NewRequest("POST", "/word-api/archive", &b)
		r.Header.Set("Content-Type", m.FormDataContentType())
		r.Header.Set("X-PDF-Request", "1")
		w := httptest.NewRecorder()
		a.handler().ServeHTTP(w, r)
		if strings.HasPrefix(name, "photos") {
			if w.Code != 200 {
				t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
			}
			z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if len(z.File) != 4 {
				t.Fatalf("%s: wrong count", name)
			}
			for _, entry := range z.File {
				reader, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(reader)
				reader.Close()
				if err != nil || !bytes.HasPrefix(raw, []byte{137, 80, 78, 71}) {
					t.Fatal("bad image or CRC", err)
				}
			}
		} else if w.Code != 400 {
			t.Fatalf("accepted invalid archive: %s %d", name, w.Code)
		}
		files, _ := os.ReadDir(a.root)
		if len(files) != 0 {
			t.Fatalf("retained archive files: %v", files)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	_, err := normalizeArchive(ctx, "unused", dir, "7z")
	if err == nil {
		t.Fatal("cancel ignored")
	}
}
