package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func findOffice(explicit string) string {
	if explicit == "" {
		explicit = os.Getenv("PDF_TOOL_OFFICE")
	}
	if explicit != "" {
		if path, err := exec.LookPath(explicit); err == nil {
			return path
		}
		return ""
	}
	executable, _ := os.Executable()
	base := filepath.Dir(executable)
	var candidates []string
	if runtime.GOOS == "windows" {
		for _, root := range []string{base, filepath.Dir(base)} {
			candidates = append(candidates, filepath.Join(root, "LibreOffice", "program", "soffice.com"), filepath.Join(root, "LibreOffice", "program", "soffice.exe"))
		}
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
			if root := os.Getenv(env); root != "" {
				candidates = append(candidates, filepath.Join(root, "LibreOffice", "program", "soffice.com"))
			}
		}
		candidates = append(candidates, "soffice.com", "soffice.exe")
	} else {
		candidates = []string{"libreoffice", "soffice", "/usr/bin/libreoffice", "/usr/bin/soffice", "/opt/libreoffice/program/soffice"}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

func fileURL(path string) string {
	p, _ := filepath.Abs(path)
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func validateWord(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".docx") {
		z, err := zip.OpenReader(path)
		if err != nil {
			return errors.New("DOCX 文件损坏、加密或格式不正确")
		}
		defer z.Close()
		found := false
		var total uint64
		if len(z.File) > 10000 {
			return errors.New("DOCX 内容过多")
		}
		for _, f := range z.File {
			if f.UncompressedSize64 > 300<<20 || total > 300<<20 {
				return errors.New("DOCX 解压后的内容超过 300 MB")
			}
			total += f.UncompressedSize64
			if f.Name == "word/document.xml" {
				found = true
			}
		}
		if total > 300<<20 {
			return errors.New("DOCX 解压后的内容超过 300 MB")
		}
		if !found {
			return errors.New("文件不是有效的 Word DOCX 文档")
		}
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 8)
	_, err = io.ReadFull(f, header)
	if err != nil || !bytes.Equal(header, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return errors.New("文件不是有效的 Word DOC 文档，请在 Word 中另存为 DOCX 后重试")
	}
	return nil
}

func wordPDF(ctx context.Context, office string, paths, names []string, dir string) (output, name string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	profile := filepath.Join(dir, "profile")
	if err = os.MkdirAll(filepath.Join(profile, "user"), 0700); err != nil {
		return
	}
	// Isolated profile avoids altering the user's Office settings or executing document macros.
	config := `<?xml version="1.0" encoding="UTF-8"?><oor:items xmlns:oor="http://openoffice.org/2001/registry"><item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item><item oor:path="/org.openoffice.Office.Writer/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>0</value></prop></item><item oor:path="/org.openoffice.Office.Common/Misc"><prop oor:name="FirstRun" oor:op="fuse"><value>false</value></prop></item><item oor:path="/org.openoffice.Office.Jobs/Jobs/UpdateCheck/Arguments"><prop oor:name="AutoCheckEnabled" oor:op="fuse"><value>false</value></prop><prop oor:name="AutoDownloadEnabled" oor:op="fuse"><value>false</value></prop></item></oor:items>`
	if err = os.WriteFile(filepath.Join(profile, "user", "registrymodifications.xcu"), []byte(config), 0600); err != nil {
		return
	}
	outdir := filepath.Join(dir, "converted")
	if err = os.Mkdir(outdir, 0700); err != nil {
		return
	}
	converted := make([]string, 0, len(paths))
	for i, path := range paths {
		if err = validateWord(path); err != nil {
			err = fmt.Errorf("%s：%w", names[i], err)
			return
		}
		oneCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.Command(office, "-env:UserInstallation="+fileURL(profile), "--headless", "--nologo", "--nodefault", "--norestore", "--convert-to", "pdf:writer_pdf_Export", "--outdir", outdir, path)
		cmd.Env = append(os.Environ(), "SAL_USE_VCLPLUGIN=svp")
		configureCommand(cmd)
		var diagnostic bytes.Buffer
		cmd.Stdout = &diagnostic
		cmd.Stderr = &diagnostic
		err = runCommand(oneCtx, cmd)
		stop()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("Word 转换超时或已取消，请减少文件数量后重试")
			} else {
				err = fmt.Errorf("%s 转换失败，请检查 LibreOffice 是否可以运行，且文档未损坏或加密", names[i])
			}
			return
		}
		pdf := filepath.Join(outdir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".pdf")
		f, openErr := os.Open(pdf)
		if openErr != nil {
			err = fmt.Errorf("%s 未生成 PDF，文档可能损坏、加密，或转换引擎缺少组件", names[i])
			return
		}
		header := make([]byte, 5)
		_, readErr := io.ReadFull(f, header)
		f.Close()
		if readErr != nil || string(header) != "%PDF-" {
			err = errors.New("转换引擎未返回有效的 PDF")
			return
		}
		converted = append(converted, pdf)
	}
	if len(converted) == 1 {
		output = filepath.Join(dir, "result.pdf")
		name = pdfBaseName(names[0])
		err = os.Rename(converted[0], output)
		return
	}
	output = filepath.Join(dir, "result.zip")
	name = "Word批量转换.zip"
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", "", err
	}
	z := zip.NewWriter(f)
	for i, path := range converted {
		var entry io.Writer
		entry, err = z.Create(fmt.Sprintf("%02d_%s", i+1, pdfBaseName(names[i])))
		if err != nil {
			break
		}
		var src *os.File
		src, err = os.Open(path)
		if err != nil {
			break
		}
		_, err = io.Copy(entry, src)
		src.Close()
		if err != nil {
			break
		}
	}
	zipErr := z.Close()
	closeErr := f.Close()
	if err == nil {
		err = zipErr
	}
	if err == nil {
		err = closeErr
	}
	return
}

func runCommand(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killCommand(cmd)
		<-done
		return ctx.Err()
	}
}
