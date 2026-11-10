package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
)

type imageOptions struct {
	Paper, Orientation string
	Margin             float64
}

func parseImageOptions(paper, orientation, margin string) (imageOptions, error) {
	if paper == "" {
		paper = "a4"
	}
	if orientation == "" {
		orientation = "auto"
	}
	if margin == "" {
		margin = "10"
	}
	if paper != "a4" && paper != "letter" && paper != "original" {
		return imageOptions{}, errors.New("纸张选项不正确")
	}
	if orientation != "auto" && orientation != "portrait" && orientation != "landscape" {
		return imageOptions{}, errors.New("页面方向不正确")
	}
	m, err := strconv.ParseFloat(margin, 64)
	if err != nil || math.IsNaN(m) || math.IsInf(m, 0) || m < 0 || m > 40 {
		return imageOptions{}, errors.New("页边距应为 0 至 40 毫米")
	}
	return imageOptions{paper, orientation, m * 72 / 25.4}, nil
}

func pageLayout(width, height int, options imageOptions) (pw, ph, x, y, iw, ih float64) {
	pw, ph = 595.2756, 841.8898
	if options.Paper == "letter" {
		pw, ph = 612, 792
	}
	if options.Paper == "original" {
		pw, ph = float64(width)*0.75, float64(height)*0.75
		scale := math.Min(1, 14000/math.Max(pw, ph))
		pw *= scale
		ph *= scale
		pw += options.Margin * 2
		ph += options.Margin * 2
	} else if options.Orientation == "landscape" || (options.Orientation == "auto" && width > height) {
		pw, ph = ph, pw
	}
	scale := math.Min((pw-2*options.Margin)/float64(width), (ph-2*options.Margin)/float64(height))
	iw, ih = float64(width)*scale, float64(height)*scale
	x, y = (pw-iw)/2, (ph-ih)/2
	return
}

type pdfWriter struct {
	out   io.Writer
	count int64
	err   error
}

func (p *pdfWriter) write(data []byte) {
	if p.err != nil {
		return
	}
	n, err := p.out.Write(data)
	p.count += int64(n)
	p.err = err
}
func (p *pdfWriter) printf(format string, args ...interface{}) {
	p.write([]byte(fmt.Sprintf(format, args...)))
}

// Each input is one page. All dimensions are PDF points; images retain aspect ratio.
func imagesPDF(ctx context.Context, paths []string, output string, options imageOptions) (err error) {
	if len(paths) == 0 {
		return errors.New("没有图片")
	}
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(output)
		}
	}()
	buffer := bufio.NewWriterSize(f, 64<<10)
	w := &pdfWriter{out: buffer}
	offsets := make([]int64, 3+3*len(paths))
	object := func(id int) { offsets[id] = w.count; w.printf("%d 0 obj\n", id) }
	w.write([]byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"))
	object(1)
	w.printf("<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	object(2)
	w.printf("<< /Type /Pages /Count %d /Kids [", len(paths))
	for i := range paths {
		w.printf("%d 0 R ", 3+i*3)
	}
	w.printf("] >>\nendobj\n")
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return errors.New("转换已取消")
		}
		img, err := readImage(path)
		if err != nil {
			return fmt.Errorf("第 %d 张图片：%w", i+1, err)
		}
		width, height := img.Bounds().Dx(), img.Bounds().Dy()
		var encoded bytes.Buffer
		if err = jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 94}); err != nil {
			return err
		}
		pw, ph, x, y, iw, ih := pageLayout(width, height, options)
		pageID := 3 + i*3
		object(pageID)
		w.printf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.4f %.4f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>\nendobj\n", pw, ph, pageID+1, pageID+2)
		object(pageID + 1)
		w.printf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", width, height, encoded.Len())
		w.write(encoded.Bytes())
		w.printf("\nendstream\nendobj\n")
		content := fmt.Sprintf("q\n%.4f 0 0 %.4f %.4f %.4f cm\n/Im0 Do\nQ\n", iw, ih, x, y)
		object(pageID + 2)
		w.printf("<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(content), content)
		if w.err != nil {
			return w.err
		}
	}
	xref := w.count
	w.printf("xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		w.printf("%010d 00000 n \n", offset)
	}
	w.printf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	if w.err != nil {
		return w.err
	}
	return buffer.Flush()
}

func readImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	config, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, errors.New("文件损坏或不是支持的图片格式")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 20000000 || config.Width > 20000 || config.Height > 20000 {
		return nil, errors.New("图片不能超过 2000 万像素，单边不能超过 20000 像素")
	}
	_, _ = f.Seek(0, 0)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, errors.New("图片数据不完整或已损坏")
	}
	flat := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(flat, flat.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
	orientation := 1
	_, _ = f.Seek(0, 0)
	header, _ := io.ReadAll(io.LimitReader(f, 1<<20))
	if format == "jpeg" {
		orientation = jpegOrientation(header)
	} else if format == "tiff" {
		orientation = tiffOrientation(header)
	}
	return orientImage(flat, orientation), nil
}

func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			break
		}
		marker := data[pos+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if marker == 0xff {
			pos++
			continue
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			pos += 2
			continue
		}
		length := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if length < 2 || pos+2+length > len(data) {
			break
		}
		payload := data[pos+4 : pos+2+length]
		if marker == 0xe1 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
			return tiffOrientation(payload[6:])
		}
		pos += length + 2
	}
	return 1
}

func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	if string(data[:2]) == "II" {
		order = binary.LittleEndian
	} else if string(data[:2]) == "MM" {
		order = binary.BigEndian
	} else {
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	pos := uint64(order.Uint32(data[4:8]))
	if pos+2 > uint64(len(data)) {
		return 1
	}
	count := int(order.Uint16(data[pos : pos+2]))
	pos += 2
	for i := 0; i < count && pos+12 <= uint64(len(data)); i++ {
		entry := data[pos : pos+12]
		if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			v := int(order.Uint16(entry[8:10]))
			if v >= 1 && v <= 8 {
				return v
			}
		}
		pos += 12
	}
	return 1
}

func orientImage(src *image.RGBA, orientation int) *image.RGBA {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.SetRGBA(dx, dy, src.RGBAAt(x, y))
		}
	}
	return dst
}

func pdfBaseName(name string) string {
	ext := strings.LastIndex(name, ".")
	if ext > 0 {
		name = name[:ext]
	}
	return safeName(name) + ".pdf"
}
