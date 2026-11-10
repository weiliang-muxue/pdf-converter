package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "透明图片.png")
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	img.SetNRGBA(10, 10, color.NRGBA{R: 255, A: 128})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestImagePageLayout(t *testing.T) {
	options, err := parseImageOptions("a4", "auto", "10")
	if err != nil {
		t.Fatal(err)
	}
	pw, ph, x, y, w, h := pageLayout(400, 200, options)
	if pw < ph || math.Abs(w/h-2) > 0.001 || x < options.Margin-.01 || y < options.Margin-.01 {
		t.Fatalf("bad landscape layout: %v", []float64{pw, ph, x, y, w, h})
	}
	options.Orientation = "portrait"
	pw, ph, _, _, _, _ = pageLayout(400, 200, options)
	if pw >= ph {
		t.Fatal("portrait ignored")
	}
	for _, margin := range []string{"NaN", "Inf", "-1", "41", "oops"} {
		if _, err := parseImageOptions("a4", "auto", margin); err == nil {
			t.Fatalf("accepted %s", margin)
		}
	}
}

func TestOrientationAllEight(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 3))
	src.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	positions := [][2]int{{0, 0}, {1, 0}, {1, 2}, {0, 2}, {0, 0}, {2, 0}, {2, 1}, {0, 1}}
	for orientation := 1; orientation <= 8; orientation++ {
		dst := orientImage(src, orientation)
		want := positions[orientation-1]
		if dst.RGBAAt(want[0], want[1]).R != 255 {
			t.Fatalf("orientation %d wrong", orientation)
		}
		if orientation >= 5 && dst.Bounds().Dx() != 3 {
			t.Fatal("missing dimension swap")
		}
	}
}

func TestImagesPDFAndTransparency(t *testing.T) {
	dir := t.TempDir()
	input := pngFixture(t, dir)
	img, err := readImage(input)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r != 65535 || g != 65535 || b != 65535 {
		t.Fatal("transparent background should be white")
	}
	output := filepath.Join(dir, "output.pdf")
	options, _ := parseImageOptions("a4", "auto", "10")
	if err := imagesPDF(context.Background(), []string{input, input}, output, options); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(output)
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) || !bytes.Contains(data, []byte("/Count 2")) || bytes.Count(data, []byte("/Type /Page /")) != 2 {
		t.Fatal("invalid PDF page structure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := filepath.Join(dir, "cancel.pdf")
	if imagesPDF(ctx, []string{input}, canceled, options) == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := os.Stat(canceled); !os.IsNotExist(err) {
		t.Fatal("partial PDF left behind")
	}
}

func TestMalformedImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.png")
	os.WriteFile(path, []byte("not a png"), 0600)
	if _, err := readImage(path); err == nil {
		t.Fatal("accepted corrupt image")
	}
	for _, data := range [][]byte{nil, {0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}, []byte("Exif\x00\x00"), []byte("II*\x00\xff\xff\xff\xff")} {
		_ = jpegOrientation(data)
		_ = tiffOrientation(data)
	}
}

func TestSafeFileName(t *testing.T) {
	for _, name := range []string{"../../a.docx", `C:\temp\a.docx`, "a\r\n.docx"} {
		got := safeName(name)
		if strings.ContainsAny(got, "/\\\r\n") {
			t.Fatal(got)
		}
	}
	if pdfBaseName("合同.最终.docx") != "合同.最终.pdf" {
		t.Fatal("bad filename")
	}
}
