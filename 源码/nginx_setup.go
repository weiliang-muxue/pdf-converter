package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const nginxBegin = "# BEGIN PDF-TOOL AUTO CONFIG"
const nginxEnd = "# END PDF-TOOL AUTO CONFIG"
const htmlMarker = `name="pdf-tool" content="offline-v1"`

type nginxNode struct {
	name      string
	args      []string
	children  []*nginxNode
	open, end int
	file      string
}
type nginxToken struct {
	value       string
	start, end  int
	punctuation bool
}
type nginxRoot struct{ dir, prefix string }
type nginxAccessRule struct {
	parentPrefix string
	modifier     string
	pattern      string
}

func (rule nginxAccessRule) couldMatch(uri string) bool {
	if !strings.HasPrefix(uri, rule.parentPrefix) {
		return false
	}
	if strings.HasPrefix(rule.pattern, "@") {
		// Internal redirects need a full routing model, so preserve their guard.
		return true
	}
	switch rule.modifier {
	case "", "^~":
		return strings.HasPrefix(uri, rule.pattern)
	case "=":
		return uri == rule.pattern
	case "~", "~*":
		pattern := rule.pattern
		if rule.modifier == "~*" {
			pattern = "(?i)" + pattern
		}
		expression, err := regexp.Compile(pattern)
		if err != nil {
			// Preserve controls for PCRE syntax unsupported by Go's regexp.
			return true
		}
		return expression.MatchString(uri)
	default:
		return true
	}
}

type nginxSite struct {
	node        *nginxNode
	roots       []nginxRoot
	prefixes    map[string]bool
	accessRules []nginxAccessRule
}
type nginxEdit struct {
	at, end int
	content string
}
type nginxOptions struct {
	executable, prefix, config, template, token, sitePath, backupDir string
	port                                                             int
}

// Parse only Nginx syntax, without executing config content or shell expressions.
func nginxTokens(src []byte) ([]nginxToken, error) {
	var out []nginxToken
	for i := 0; i < len(src); {
		if strings.ContainsRune(" \r\n\t", rune(src[i])) {
			i++
			continue
		}
		if src[i] == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		if strings.ContainsRune("{};", rune(src[i])) {
			out = append(out, nginxToken{string(src[i]), i, i + 1, true})
			i++
			continue
		}
		var b strings.Builder
		quote := byte(0)
		for i < len(src) {
			c := src[i]
			if quote == 0 && (strings.ContainsRune(" \r\n\t{};", rune(c)) || c == '#') {
				break
			}
			if c == '\\' && i+1 < len(src) {
				next := src[i+1]
				switch next {
				case '\\', '"', '\'':
					b.WriteByte(next)
				case 't':
					b.WriteByte('\t')
				case 'r':
					b.WriteByte('\r')
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte('\\')
					b.WriteByte(next)
				}
				i += 2
				continue
			}
			if c == '\'' || c == '"' {
				if quote == 0 {
					quote = c
					i++
					continue
				}
				if quote == c {
					quote = 0
					i++
					continue
				}
			}
			b.WriteByte(c)
			i++
		}
		if quote != 0 {
			return nil, errors.New("Nginx 配置引号不完整")
		}
		out = append(out, nginxToken{b.String(), start, i, false})
	}
	return out, nil
}
func parseNginx(src []byte, file string) ([]*nginxNode, error) {
	tokens, err := nginxTokens(src)
	if err != nil {
		return nil, err
	}
	pos := 0
	var parse func(bool) ([]*nginxNode, error)
	parse = func(nested bool) ([]*nginxNode, error) {
		var nodes []*nginxNode
		for pos < len(tokens) {
			if tokens[pos].punctuation && tokens[pos].value == "}" {
				if !nested {
					return nil, errors.New("unexpected Nginx closing brace")
				}
				pos++
				return nodes, nil
			}
			t := tokens[pos]
			pos++
			if t.punctuation {
				return nil, errors.New("invalid Nginx directive")
			}
			n := &nginxNode{name: t.value, file: file, open: -1}
			for pos < len(tokens) && !tokens[pos].punctuation {
				n.args = append(n.args, tokens[pos].value)
				pos++
			}
			if pos == len(tokens) {
				return nil, errors.New("unterminated Nginx directive")
			}
			end := tokens[pos]
			pos++
			if end.value == "{" {
				n.open = end.end
				n.children, err = parse(true)
				if err != nil {
					return nil, err
				}
				n.end = tokens[pos-1].start
			} else if end.value != ";" {
				return nil, errors.New("invalid Nginx directive ending")
			}
			nodes = append(nodes, n)
		}
		if nested {
			return nil, errors.New("unterminated Nginx block")
		}
		return nodes, nil
	}
	return parse(false)
}
func nginxCommand(o nginxOptions, tail ...string) ([]byte, error) {
	args := []string{}
	if o.prefix != "" {
		args = append(args, "-p", o.prefix)
	}
	if o.config != "" {
		args = append(args, "-c", o.config)
	}
	args = append(args, tail...)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.executable, args...)
	configureCommand(cmd)
	return cmd.CombinedOutput()
}
func absoluteNginx(p, base string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	p, _ = filepath.Abs(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}
func nginxFiles(dump []byte) []string {
	re := regexp.MustCompile(`(?m)^# configuration file (.+):\r?$`)
	var files []string
	for _, m := range re.FindAllSubmatch(dump, -1) {
		files = append(files, string(m[1]))
	}
	return files
}
func findHTML(dir string) ([]string, error) {
	var found []string
	seen := 0
	var walk func(string, int) error
	walk = func(current string, depth int) error {
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		} // Avoid searching unrelated trees through links.
		if !info.IsDir() {
			return nil
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			seen++
			if seen > 3000 {
				return errors.New("网站目录文件过多，请通过 --site-path 指定工具所在网址前缀")
			}
			if entry.Name() == "index.html" && !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				file := filepath.Join(current, entry.Name())
				stat, _ := entry.Info()
				if stat != nil && stat.Size() < 2<<20 {
					data, err := os.ReadFile(file)
					if err != nil {
						return err
					}
					if bytes.Contains(data, []byte(htmlMarker)) {
						found = append(found, current)
					}
				}
			}
			if depth < 5 && entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				if err = walk(filepath.Join(current, entry.Name()), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(dir, 0)
	return found, err
}
func discoverNginxSites(files []string, prefix, sitePath string) ([]*nginxSite, map[string][]byte, error) {
	if len(files) == 0 {
		return nil, nil, errors.New("nginx -T 没有返回配置文件列表")
	}
	sources := map[string][]byte{}
	parsed := map[string][]*nginxNode{}
	for _, file := range files {
		key := absoluteNginx(file, prefix)
		data, err := os.ReadFile(key)
		if err != nil {
			return nil, nil, err
		}
		nodes, err := parseNginx(data, key)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", key, err)
		}
		sources[key] = data
		parsed[key] = nodes
	}
	mainFile := absoluteNginx(files[0], prefix)
	confBase := filepath.Dir(mainFile)
	sites := []*nginxSite{}
	httpRoot := filepath.Join(prefix, "html")
	var visit func([]*nginxNode, bool, *nginxSite, string, int) error
	visit = func(nodes []*nginxNode, http bool, site *nginxSite, urlPrefix string, depth int) error {
		if depth > 30 {
			return errors.New("Nginx include 嵌套过深")
		}
		for _, n := range nodes {
			if n.name == "include" && len(n.args) == 1 {
				pattern := absoluteNginx(n.args[0], confBase)
				matches, _ := filepath.Glob(pattern)
				for _, name := range matches {
					if sub, ok := parsed[absoluteNginx(name, confBase)]; ok {
						if err := visit(sub, http, site, urlPrefix, depth+1); err != nil {
							return err
						}
					}
				}
				continue
			}
			if n.name == "http" && !http {
				if err := visit(n.children, true, nil, "/", depth+1); err != nil {
					return err
				}
				continue
			}
			if !http {
				continue
			}
			if n.name == "server" && site == nil {
				s := &nginxSite{node: n, prefixes: map[string]bool{}}
				if err := visit(n.children, true, s, "/", depth+1); err != nil {
					return err
				}
				if len(s.roots) == 0 {
					s.roots = append(s.roots, nginxRoot{httpRoot, "/"})
				}
				sites = append(sites, s)
				continue
			}
			if n.name == "location" && site != nil {
				before := string(sources[n.file][:n.open])
				managedBegin, managedEnd := strings.LastIndex(before, nginxBegin), strings.LastIndex(before, nginxEnd)
				managed := managedBegin >= 0 && managedBegin > managedEnd
				rule := nginxAccessRule{parentPrefix: urlPrefix}
				switch len(n.args) {
				case 1:
					rule.pattern = n.args[0]
				case 2:
					rule.modifier = n.args[0]
					rule.pattern = n.args[1]
				}
				inspectNestedLocations := rule.modifier == "~" || rule.modifier == "~*"

				var accessControls func([]*nginxNode, int) bool
				accessControls = func(nodes []*nginxNode, level int) bool {
					if level > 30 {
						return true
					}
					for _, item := range nodes {
						// Literal child locations are inspected by visit below.
						if item.name == "location" && !inspectNestedLocations {
							continue
						}
						switch item.name {
						case "auth_basic", "auth_request", "allow", "deny", "satisfy", "limit_except", "secure_link", "rewrite", "return", "if":
							return true
						}
						if strings.Contains(item.name, "_lua") {
							return true
						}
						if item.name == "include" && len(item.args) == 1 {
							matches, _ := filepath.Glob(absoluteNginx(item.args[0], confBase))
							for _, file := range matches {
								if accessControls(parsed[absoluteNginx(file, confBase)], level+1) {
									return true
								}
							}
						}
						if accessControls(item.children, level+1) {
							return true
						}
					}
					return false
				}
				if !managed && accessControls(n.children, 0) {
					site.accessRules = append(site.accessRules, rule)
				}
				location := ""
				if len(n.args) > 0 {
					location = n.args[0]
				}
				if location == "^~" && len(n.args) > 1 {
					location = n.args[1]
				}
				if strings.HasPrefix(location, "/") && !strings.Contains(location, "$") {
					if err := visit(n.children, true, site, location, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			if (n.name == "root" || n.name == "alias") && len(n.args) == 1 && !strings.Contains(n.args[0], "$") {
				dir := absoluteNginx(n.args[0], prefix)
				if site == nil && n.name == "root" {
					httpRoot = dir
					continue
				}
				if site != nil {
					if n.name == "root" {
						dir = filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(urlPrefix, "/")))
					}
					site.roots = append(site.roots, nginxRoot{dir, urlPrefix})
				}
			}
		}
		return nil
	}
	if err := visit(parsed[mainFile], false, nil, "/", 0); err != nil {
		return nil, nil, err
	}
	var selected []*nginxSite
	seenSites := map[string]bool{}
	for _, site := range sites {
		for _, r := range site.roots {
			dir := r.dir
			urlPrefix := r.prefix
			if sitePath != "" {
				if !strings.HasPrefix(sitePath, urlPrefix) {
					continue
				}
				dir = filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(sitePath, urlPrefix)))
				urlPrefix = sitePath
			}
			matches, err := findHTML(dir)
			if err != nil {
				return nil, nil, err
			}
			for _, match := range matches {
				relative, _ := filepath.Rel(dir, match)
				u := path.Join("/", urlPrefix, filepath.ToSlash(relative))
				if u != "/" {
					u += "/"
				}
				if sitePath == "" || u == sitePath {
					site.prefixes[u] = true
				}
			}
		}
		key := fmt.Sprintf("%s:%d", site.node.file, site.node.open)
		if len(site.prefixes) > 0 && !seenSites[key] {
			for prefix := range site.prefixes {
				targets := []string{
					prefix,
					prefix + "index.html",
					prefix + "word-api/status",
					prefix + "word-api/convert",
					prefix + "word-api/download",
				}
				for _, rule := range site.accessRules {
					for _, target := range targets {
						if rule.couldMatch(target) {
							return nil, nil, fmt.Errorf("网页路径 %s 涉及已有 location 访问控制或重写规则，自动配置已停止以保留现有规则", target)
						}
					}
				}
			}
			selected = append(selected, site)
			seenSites[key] = true
		}
	}
	if len(selected) == 0 {
		return nil, nil, errors.New("没有找到已部署的 PDF 网页。请先把本包 index.html 复制到 Nginx 网站目录，再重新运行启动脚本")
	}
	return selected, sources, nil
}
func nginxManagedEdits(sites []*nginxSite, sources map[string][]byte, template string, o nginxOptions) (map[string][]byte, error) {
	edits := map[string][]nginxEdit{}
	for _, site := range sites {
		n := site.node
		body := string(sources[n.file][n.open:n.end])
		begin, end := strings.Index(body, nginxBegin), strings.Index(body, nginxEnd)
		if (begin >= 0) != (end >= 0) || (begin >= 0 && end < begin) {
			return nil, errors.New("已有 PDF 自动配置标记不完整，请检查原文件")
		}
		clean := body
		if begin >= 0 {
			clean = body[:begin] + body[end+len(nginxEnd):]
			edits[n.file] = append(edits[n.file], nginxEdit{n.open + begin, n.open + end + len(nginxEnd), ""})
		}
		prefixes := []string{}
		for p := range site.prefixes {
			prefixes = append(prefixes, p)
		}
		sort.Strings(prefixes)
		var block strings.Builder
		block.WriteString("\n    " + nginxBegin + "\n")
		for _, p := range prefixes {
			if strings.Contains(clean, p+"word-api/") {
				return nil, fmt.Errorf("站点已有手动配置的 %sword-api/，为避免覆盖请先移除旧代理配置", p)
			}
			content := strings.ReplaceAll(template, "__WORD_TOKEN__", o.token)
			content = strings.ReplaceAll(content, "/word-api/", p+"word-api/")
			content = strings.ReplaceAll(content, "127.0.0.1:8787", fmt.Sprintf("127.0.0.1:%d", o.port))
			block.WriteString(content)
			block.WriteByte('\n')
		}
		block.WriteString("    " + nginxEnd + "\n")
		edits[n.file] = append(edits[n.file], nginxEdit{n.open, n.open, block.String()})
	}
	output := map[string][]byte{}
	for file, list := range edits {
		sort.Slice(list, func(i, j int) bool { return list[i].at > list[j].at })
		data := append([]byte(nil), sources[file]...)
		for _, edit := range list {
			data = append(append(append([]byte(nil), data[:edit.at]...), []byte(edit.content)...), data[edit.end:]...)
		}
		output[file] = data
	}
	return output, nil
}
func writeNginxFile(file string, data []byte) error {
	// Keep the original inode, ownership, Windows ACL and Linux SELinux labels.
	// A complete backup exists before any write; reload only happens after sync and -t.
	f, err := os.OpenFile(file, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteAt(data, 0)
	if err == nil {
		err = f.Truncate(int64(len(data)))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}
func setupNginx(o nginxOptions) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{24,128}$`).MatchString(o.token) {
		return errors.New("无效服务口令")
	}
	if o.port < 1024 || o.port > 65535 {
		return errors.New("无效服务端口")
	}
	if err := checkWordService(fmt.Sprintf("127.0.0.1:%d", o.port), o.token); err != nil {
		return fmt.Errorf("后台尚未就绪，Nginx 配置未发布: %w", err)
	}
	if o.sitePath != "" && !regexp.MustCompile(`^/(?:[A-Za-z0-9_-]+/)*$`).MatchString(o.sitePath) {
		return errors.New("无效网址前缀")
	}
	if o.executable == "" {
		binary, prefix, config, err := detectNginxProcess()
		if err != nil {
			return err
		}
		o.executable = binary
		if o.prefix == "" {
			o.prefix = prefix
		}
		if o.config == "" {
			o.config = config
		}
		if o.executable == "" {
			o.executable = "nginx"
		}
	}
	executable, err := exec.LookPath(o.executable)
	if err != nil {
		return errors.New("没有找到 Nginx；请确认 Nginx 已安装并运行，或指定 --nginx 的完整路径")
	}
	o.executable = executable
	dump, err := nginxCommand(o, "-T")
	if err != nil {
		return fmt.Errorf("原 Nginx 配置检查失败，未修改：%s", dump)
	}
	prefix := o.prefix
	if prefix == "" {
		v, _ := nginxCommand(o, "-V")
		m := regexp.MustCompile(`--prefix=([^\s]+)`).FindSubmatch(v)
		if len(m) > 1 {
			prefix = strings.Trim(string(m[1]), "'\"")
		} else {
			prefix = filepath.Dir(o.executable)
		}
	}
	prefix = absoluteNginx(prefix, filepath.Dir(o.executable))
	sites, sources, err := discoverNginxSites(nginxFiles(dump), prefix, o.sitePath)
	if err != nil {
		return err
	}
	template, err := os.ReadFile(o.template)
	if err != nil {
		return err
	}
	updated, err := nginxManagedEdits(sites, sources, string(template), o)
	if err != nil {
		return err
	}
	// Verify files did not change while discovery was running; preserve a complete original backup.
	if o.backupDir == "" {
		o.backupDir = filepath.Join(filepath.Dir(o.template), "nginx-backups")
	}
	if err = os.MkdirAll(o.backupDir, 0700); err != nil {
		return err
	}
	for file := range updated {
		current, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, sources[file]) {
			return errors.New("Nginx 配置同时被其他程序修改，请重试")
		}
		backup := filepath.Join(o.backupDir, filepath.Base(file)+"."+time.Now().Format("20060102-150405")+"-"+randomID()[:8]+".bak")
		if err = os.WriteFile(backup, current, 0600); err != nil {
			return err
		}
		fmt.Println("已备份：", backup)
	}
	changed := []string{}
	rollback := func() error {
		var failures []string
		for _, file := range changed {
			if current, e := os.ReadFile(file); e == nil && bytes.Equal(current, sources[file]) {
				continue
			}
			if e := writeNginxFile(file, sources[file]); e != nil {
				failures = append(failures, file+": "+e.Error())
			}
		}
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	}
	fail := func(reason error) error {
		if restoreErr := rollback(); restoreErr != nil {
			return fmt.Errorf("%v；恢复原配置失败：%v，请使用备份恢复", reason, restoreErr)
		}
		return fmt.Errorf("%v；已恢复原配置", reason)
	}
	for file, data := range updated {
		changed = append(changed, file)
		if err = writeNginxFile(file, data); err != nil {
			return fail(err)
		}
	}
	if output, e := nginxCommand(o, "-t"); e != nil {
		return fail(fmt.Errorf("新配置检查失败：%s", output))
	}
	if output, e := nginxCommand(o, "-s", "reload"); e != nil {
		return fail(fmt.Errorf("Nginx 重载失败：%s", output))
	}
	for _, site := range sites {
		for p := range site.prefixes {
			fmt.Println("已自动配置网页路径：", p+"index.html")
		}
	}
	fmt.Println("Nginx 已检查并重新加载，无需手动编辑配置。")
	return nil
}
