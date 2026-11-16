package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Archives are never unpacked to filesystem paths supplied by the archive.
// Selected image bytes go through bounded stdout pipes into one temporary ZIP.
// The response contains all images; subsequent sorting/downloads need no Pod state.
type archiveEntry struct {
	name string
	size int64
}

func archiveEngine() string {
	if configured := os.Getenv("PDF_TOOL_ARCHIVER"); configured != "" {
		return configured
	}
	return "/opt/pdf-tool/7zzs"
}

type archiveLimitWriter struct {
	out    io.Writer
	left   int64
	cancel context.CancelFunc
}

func (w *archiveLimitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.left {
		w.cancel()
		return 0, errors.New("解压数据超过大小限制")
	}
	n, e := w.out.Write(p)
	w.left -= int64(n)
	return n, e
}
func runArchive(ctx context.Context, dir string, out io.Writer, limit int64, args ...string) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	command, err := archiveCommand(ctx, args...)
	if err != nil {
		return 0, err
	}
	command.Dir = dir
	command.Env = append(os.Environ(), "TMPDIR="+dir, "TEMP="+dir, "TMP="+dir, "HOME="+dir)
	writer := &archiveLimitWriter{out: out, left: limit, cancel: cancel}
	command.Stdout = writer
	command.Stderr = &archiveLimitWriter{out: io.Discard, left: 65536, cancel: cancel}
	configureCommand(command)
	// Go 1.20 supports cancellation of the entire subprocess group.
	command.Cancel = func() error {
		if command.Process != nil {
			killCommand(command)
		}
		return nil
	}
	command.WaitDelay = 2 * time.Second
	err = command.Run()
	return limit - writer.left, err
}
func parseArchiveList(data []byte) ([]archiveEntry, error) {
	bad := errors.New("压缩包目录无效、含不安全路径或不支持的文件类型")
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	blocks := strings.Split(strings.Trim(text, "\n"), "\n\n")
	if len(blocks) > 10000 {
		return nil, errors.New("压缩包目录过多")
	}
	seen := map[string]bool{}
	items := []archiveEntry{}
	var total int64
	for _, block := range blocks {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			k, v, ok := strings.Cut(line, " = ")
			if !ok || k == "" {
				return nil, bad
			}
			if _, exists := fields[k]; exists {
				return nil, bad
			}
			fields[k] = v
		}
		name := fields["Path"]
		normalized := strings.ReplaceAll(name, "\\", "/")
		if name == "" || len(name) > 1024 || strings.HasPrefix(normalized, "/") || strings.ContainsAny(name, ":*?\x00") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, bad
		}
		for _, part := range strings.Split(normalized, "/") {
			if part == ".." || part == "." {
				return nil, bad
			}
		}
		if fields["Split Before"] == "+" || fields["Split After"] == "+" {
			return nil, errors.New("暂不支持分卷压缩包，请合并为单个压缩包")
		}
		if fields["Encrypted"] == "+" {
			return nil, errors.New("暂不支持带密码的压缩包，请移除密码后重试")
		}
		if fields["Symbolic Link"] != "" || fields["Hard Link"] != "" || strings.Contains(fields["Attributes"], "l") {
			return nil, bad
		}
		if fields["Folder"] == "+" || strings.HasSuffix(normalized, "/") || strings.HasPrefix(fields["Attributes"], "D") {
			continue
		}
		key := strings.ToLower(normalized)
		if seen[key] {
			return nil, errors.New("压缩包含重复文件名")
		}
		seen[key] = true
		ext := strings.ToLower(path.Ext(normalized))
		if strings.HasPrefix(normalized, "__MACOSX/") || strings.HasPrefix(path.Base(normalized), "._") {
			continue
		}
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".gif":
		default:
			continue
		}
		size, err := strconv.ParseInt(fields["Size"], 10, 64)
		if err != nil || size <= 0 || size > 50<<20 {
			return nil, errors.New("压缩包中单张图片必须大于 0 且不超过 50 MB")
		}
		total += size
		if total > maxUpload || len(items) >= 200 {
			return nil, errors.New("解压后最多 200 张图片、合计 200 MB")
		}
		items = append(items, archiveEntry{name: name, size: size})
	}
	if len(items) == 0 {
		return nil, errors.New("压缩包中没有支持的图片")
	}
	return items, nil
}
func normalizeArchive(ctx context.Context, input, dir, format string) (string, error) {
	if format == "rar" {
		f, err := os.Open(input)
		if err != nil {
			return "", err
		}
		signature := make([]byte, 8)
		_, _ = io.ReadFull(f, signature)
		f.Close()
		if bytes.Equal(signature, []byte{'R', 'a', 'r', '!', 0x1a, 7, 1, 0}) {
			format = "rar5"
		}
	}
	var listing bytes.Buffer
	common := []string{"-t" + format, "-sccUTF-8", "-p-", "-y", "-mmt=1"}
	args := append([]string{"l", "-slt", "-ba"}, common...)
	args = append(args, "--", input)
	if _, err := runArchive(ctx, dir, &listing, 2<<20, args...); err != nil {
		return "", errors.New("无法读取压缩包，请检查文件是否损坏、加密或为分卷压缩包")
	}
	items, err := parseArchiveList(listing.Bytes())
	if err != nil {
		return "", err
	}
	output := filepath.Join(dir, "images.zip")
	f, err := os.Create(output)
	if err != nil {
		return "", err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	for _, item := range items {
		h := &zip.FileHeader{Name: strings.ReplaceAll(item.name, "\\", "/"), Method: zip.Store}
		entry, e := z.CreateHeader(h)
		if e != nil {
			return "", e
		}
		args = append([]string{"x", "-so", "-spd", "-ssc", "-bb0", "-bd"}, common...)
		args = append(args, "--", input, item.name)
		n, e := runArchive(ctx, dir, entry, item.size, args...)
		if e != nil || n != item.size {
			return "", errors.New("解压失败：文件损坏、数据校验失败或超出资源限制")
		}
	}
	if err = z.Close(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	return output, nil
}
func (a *application) importArchive(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "POST") {
		return
	}
	select {
	case a.busy <- struct{}{}:
		defer func() { <-a.busy }()
	default:
		fail(w, 429, "正在处理其他任务，请稍后重试")
		return
	}
	if _, err := os.Stat(archiveEngine()); err != nil {
		fail(w, 503, "当前版本未包含解压组件，请更新完整镜像")
		return
	}
	dir, err := os.MkdirTemp(a.root, "archive-")
	if err != nil {
		fail(w, 500, "无法创建处理目录")
		return
	}
	defer os.RemoveAll(dir)
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		fail(w, 400, "请选择一个压缩包")
		return
	}
	input := filepath.Join(dir, "upload.archive")
	format := ""
	count := 0
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			fail(w, 400, "无法读取压缩包，请确认文件不超过 200 MB")
			return
		}
		count++
		if count != 1 || part.FormName() != "files" {
			part.Close()
			fail(w, 400, "每次请选择一个压缩包")
			return
		}
		switch strings.ToLower(filepath.Ext(part.FileName())) {
		case ".rar":
			format = "rar"
		case ".7z":
			format = "7z"
		case ".zip":
			format = "zip"
		default:
			part.Close()
			fail(w, 400, "仅支持 ZIP、RAR、7Z")
			return
		}
		f, e := os.Create(input)
		if e != nil {
			part.Close()
			fail(w, 500, "无法处理压缩包")
			return
		}
		n, e := io.Copy(f, io.LimitReader(part, maxUpload+1))
		closeErr := f.Close()
		part.Close()
		if e != nil || closeErr != nil || n == 0 || n > maxUpload {
			fail(w, 400, "压缩包必须大于 0 且不超过 200 MB")
			return
		}
	}
	if count != 1 {
		fail(w, 400, "请选择一个压缩包")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	output, err := normalizeArchive(ctx, input, dir, format)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	f, err := os.Open(output)
	if err != nil {
		fail(w, 500, "无法读取解压结果")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fail(w, 500, "无法读取解压结果")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="images.zip"`)
	w.Header().Set("Content-Length", fmt.Sprint(st.Size()))
	_, _ = io.Copy(w, f)
}
