package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var assets embed.FS

const version = "1.0.3"
const maxUpload = 200 << 20

type result struct {
	Path    string
	Name    string
	Expires time.Time
}

type application struct {
	token, root, office string
	serverMode          bool
	mu                  sync.Mutex
	results             map[string]result
	busy                chan struct{}
	stop                context.CancelFunc
}

func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "监听地址，服务器示例 0.0.0.0:8080")
	serverMode := flag.Bool("server", false, "服务器模式，不自动打开浏览器")
	noBrowser := flag.Bool("no-browser", false, "不自动打开浏览器")
	office := flag.String("office", "", "LibreOffice soffice 可执行文件完整路径")
	showVersion := flag.Bool("version", false, "显示版本")
	setup := flag.Bool("setup-nginx", false, "自动配置已部署 PDF 网页的 Nginx 代理")
	nginxExe := flag.String("nginx", "", "Nginx 可执行文件")
	nginxPrefix := flag.String("nginx-prefix", "", "Nginx prefix")
	nginxConfig := flag.String("nginx-config", "", "Nginx 主配置文件")
	nginxTemplate := flag.String("nginx-template", "", "Word 代理配置模板")
	nginxBackups := flag.String("nginx-backups", "", "Nginx 备份目录")
	sitePath := flag.String("site-path", "", "可选：限定网页地址前缀")
	wordPort := flag.Int("word-port", 8787, "Word 内部服务端口")
	checkService := flag.Bool("check-service", false, "检查本机 Word 后台是否就绪")
	flag.Parse()
	if *checkService {
		if err := checkWordService(*listen, os.Getenv("PDF_TOOL_TOKEN")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Word service ready")
		return
	}
	if *setup {
		err := setupNginx(nginxOptions{executable: *nginxExe, prefix: *nginxPrefix, config: *nginxConfig, template: *nginxTemplate, token: os.Getenv("PDF_TOOL_TOKEN"), sitePath: *sitePath, port: *wordPort, backupDir: *nginxBackups})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *showVersion {
		fmt.Printf("PDF Tool %s %s/%s %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return
	}
	if err := run(*listen, *serverMode, *noBrowser, *office); err != nil {
		showError(err.Error())
		os.Exit(1)
	}
}

func run(address string, serverMode, noBrowser bool, office string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("监听地址格式不正确：%w", err)
	}
	ip := net.ParseIP(host)
	if !serverMode && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("对外提供服务请显式添加 --server 参数")
	}
	token := os.Getenv("PDF_TOOL_TOKEN")
	if token == "" {
		token = randomID()
	}
	if len(token) < 16 {
		return errors.New("PDF_TOOL_TOKEN 至少需要 16 个字符")
	}
	root, err := os.MkdirTemp("", "pdf-tool-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a := &application{token: token, root: root, office: findOffice(office), serverMode: serverMode, results: map[string]result{}, busy: make(chan struct{}, 1), stop: cancel}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("无法启动服务：%w", err)
	}
	srv := &http.Server{Handler: a.handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	urlHost := ln.Addr().String()
	if host == "0.0.0.0" || host == "" || host == "::" {
		_, port, _ := net.SplitHostPort(urlHost)
		urlHost = net.JoinHostPort("127.0.0.1", port)
	}
	launchURL := "http://" + urlHost + "/#token=" + url.QueryEscape(token)
	fmt.Printf("PDF Tool %s\n地址: %s\n远程访问请把 127.0.0.1 替换为服务器 IP。\n", version, launchURL)
	if !serverMode {
		// A visible fallback URL for desktop builds with no console.
		_ = os.WriteFile(filepath.Join(root, "打开地址.txt"), []byte(launchURL), 0600)
		if !noBrowser {
			if err := openBrowser(launchURL); err != nil {
				ln.Close()
				return fmt.Errorf("无法打开浏览器，请手动访问 %s：%w", launchURL, err)
			}
		}
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	go a.cleanup(ctx)
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
		}
	}
	return nil
}

func (a *application) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", a.auth(a.status))
	mux.HandleFunc("/api/convert", a.auth(a.convert))
	mux.HandleFunc("/api/download", a.auth(a.download))
	mux.HandleFunc("/api/exit", a.auth(func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, "POST") {
			return
		}
		if a.serverMode {
			fail(w, 403, "服务器模式不允许从页面关闭")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		time.AfterFunc(250*time.Millisecond, a.stop)
	}))
	web, _ := fs.Sub(assets, "web")
	files := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (a *application) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-PDF-Token")
		if token == "" && r.URL.Path == "/api/download" {
			token = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
			fail(w, 401, "访问口令不正确，请使用启动时显示的完整地址")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				fail(w, 403, "不允许跨站请求")
				return
			}
		}
		next(w, r)
	}
}

func (a *application) status(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	writeJSON(w, 200, map[string]interface{}{"version": version, "wordReady": a.office != "", "server": a.serverMode, "platform": runtime.GOOS + "/" + runtime.GOARCH, "maxMB": 200,
		"system": hostLabel("PDF_HOST_OS", runtime.GOOS), "architecture": hostLabel("PDF_HOST_ARCH", runtime.GOARCH), "engineTarget": hostLabel("PDF_ENGINE_VARIANT", "")})
}

// Supplied by the local launcher, never by upload data. Describes the host outside chroot.
func hostLabel(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" || len(value) > 512 {
		return fallback
	}
	return value
}

func (a *application) convert(w http.ResponseWriter, r *http.Request) {
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
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		fail(w, 400, "无法读取上传内容，请确认文件总量不超过 200 MB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > 200 {
		fail(w, 400, "请选择 1 至 200 个文件")
		return
	}
	var total int64
	for _, f := range files {
		total += f.Size
		if f.Size > 50<<20 || f.Size == 0 {
			fail(w, 400, "单个文件必须大于 0 且不超过 50 MB")
			return
		}
	}
	if total > maxUpload {
		fail(w, 400, "文件总量不能超过 200 MB")
		return
	}
	dir, err := os.MkdirTemp(a.root, "job-")
	if err != nil {
		fail(w, 500, "无法创建临时目录")
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	paths := make([]string, 0, len(files))
	names := make([]string, 0, len(files))
	kind := r.FormValue("kind")
	if kind != "images" && kind != "word" {
		fail(w, 400, "请选择图片或 Word 转换")
		return
	}
	if kind == "word" && len(files) > 20 {
		fail(w, 400, "Word 每批最多 20 个文件")
		return
	}
	if kind == "word" && a.office == "" {
		fail(w, 400, "未检测到 LibreOffice。请把随附 LibreOffice 文件夹放在 EXE 旁，或按说明安装后重新启动。")
		return
	}
	for i, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Filename))
		allowed := ".jpg .jpeg .png .bmp .gif .tif .tiff"
		if kind == "word" {
			allowed = ".doc .docx"
		}
		if ext == "" || !strings.Contains(" "+allowed+" ", " "+ext+" ") {
			fail(w, 400, "不支持的文件格式："+safeName(f.Filename))
			return
		}
		p := filepath.Join(dir, fmt.Sprintf("input-%03d%s", i+1, ext))
		src, err := f.Open()
		if err != nil {
			fail(w, 400, "无法读取文件")
			return
		}
		dst, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			src.Close()
			fail(w, 500, "无法保存临时文件")
			return
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil || closeErr != nil {
			fail(w, 500, "临时磁盘写入失败")
			return
		}
		paths = append(paths, p)
		names = append(names, safeName(f.Filename))
	}
	output := filepath.Join(dir, "result.pdf")
	name := "图片合并.pdf"
	if kind == "images" {
		options, parseErr := parseImageOptions(r.FormValue("paper"), r.FormValue("orientation"), r.FormValue("margin"))
		if parseErr != nil {
			fail(w, 400, parseErr.Error())
			return
		}
		err = imagesPDF(r.Context(), paths, output, options)
	} else {
		output, name, err = wordPDF(r.Context(), a.office, paths, names, dir)
	}
	if err != nil {
		log.Printf("conversion failed: %v", err)
		fail(w, 400, err.Error())
		return
	}
	if r.Context().Err() != nil {
		return
	}
	// Input documents and the office profile are no longer needed after conversion.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if filepath.Join(dir, entry.Name()) != output {
			_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
		}
	}
	id := randomID()
	a.mu.Lock()
	a.results[id] = result{Path: output, Name: name, Expires: time.Now().Add(time.Hour)}
	a.mu.Unlock()
	keep = true
	writeJSON(w, 200, map[string]interface{}{"id": id, "name": name, "count": len(paths), "expiresMinutes": 60})
}

func (a *application) download(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "GET") {
		return
	}
	a.mu.Lock()
	item, ok := a.results[r.URL.Query().Get("id")]
	a.mu.Unlock()
	if !ok || time.Now().After(item.Expires) {
		fail(w, 404, "文件已过期，请重新转换")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name}))
	w.Header().Set("Content-Type", "application/pdf")
	if strings.HasSuffix(item.Path, ".zip") {
		w.Header().Set("Content-Type", "application/zip")
	}
	http.ServeFile(w, r, item.Path)
}

func (a *application) cleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.mu.Lock()
			for id, item := range a.results {
				if now.After(item.Expires) {
					_ = os.RemoveAll(filepath.Dir(item.Path))
					delete(a.results, id)
				}
			}
			a.mu.Unlock()
		}
	}
}

func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	r := []rune(name)
	if len(r) > 120 {
		name = string(r[:120])
	}
	name = strings.Trim(name, ". ")
	if name == "" {
		return "文件"
	}
	return name
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	fail(w, 405, "请求方法不支持")
	return false
}
func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
