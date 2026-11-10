# PDF 工具箱

中文离线文件转换工具：多图合并 PDF，DOC/DOCX 转 PDF。Windows 双击 EXE 打开本机浏览器；Linux 作为私有 HTTP 服务使用。界面完全打包进主程序，无 CDN、在线字体、Node/Python/Java 运行依赖。

## 目录

- `main.go`：HTTP 服务、访问口令、上传校验、下载、过期清理和退出。
- `images.go`：图片解码、EXIF 方向、透明背景、页面排版与 PDF 写入。
- `word.go`：独立 LibreOffice 配置、DOC/DOCX 校验、进程超时及 ZIP 输出。
- `platform_*.go`：系统进程与浏览器调用。
- `web/`：HTML、CSS、ES5 JavaScript 界面。
- `vendor/`：已固定的 Go 图片解码依赖，离线构建可用。
- `tests/`：真实程序集成测试与独立浏览器测试。
- `deploy/`：Linux systemd 示例。

## 构建

Windows 7 兼容 EXE **固定使用 Go 1.20.14**，目标为 `windows/386`，适配 32/64 位 Windows 7 SP1。Windows 10 独立版使用 Go 1.27.3，目标为 `windows/amd64`。32 位 Windows 10 使用兼容版。不得将 Win7 编译器直接替换为新版 Go。

Linux 交付版本使用 Go 1.27.3，三个架构关闭 CGO。构建脚本不下载、不安装编译器；维护者需提前准备编译器。应用运行和内网部署不下载任何依赖。

```powershell
.\build.ps1 -LegacyGo 'C:\SDK\go1.20.14\bin\go.exe' -ModernGo 'C:\SDK\go1.27.3\bin\go.exe'
```

Linux 单独构建示例：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -mod=vendor -trimpath -ldflags='-s -w' -o pdf-tool-linux-arm64 .
```

将 `GOARCH` 换为 `amd64` 或 `loong64` 即可构建其他版本。项目仅实现 Windows 和 Linux 的平台适配。

## 运行

```text
exe/Windows7/PDF工具箱-Win7.exe
exe/Windows10/PDF工具箱-Win10.exe
sudo sh exe/信创Linux/start.sh
```

默认访问口令随机生成，通过启动 URL 的 fragment 传到浏览器，API 通过请求头鉴权；下载 URL 使用同一口令参数。服务不记录访问 URL，部署时也应避免反向代理记录下载参数。可用 `PDF_TOOL_TOKEN` 指定至少 16 字符的固定口令。

LibreOffice 查找顺序：`--office` → `PDF_TOOL_OFFICE` → EXE 旁 `LibreOffice/program/soffice.com` → 标准安装位置 / PATH。主程序不执行 shell 拼接命令，上传文件以固定安全名称写入临时目录。

### 独立 HTML / Nginx 网页

`../html/index.html` 是单文件离线网页，ZIP 解压、数字排序及多图 PDF 在浏览器本地完成。Word 通过同源 `word-api/` 路由调用本服务，部署模板和完整说明在 `../html/部署配置`、`../html/使用说明.txt`。

服务端 1.0.1 的下载接口优先读取 `X-PDF-Token` 请求头，仅在没有该头时使用旧的查询参数口令。这样 Nginx 可以在服务器内部注入口令，网页及下载链接不必携带它。回归测试覆盖请求头下载、带端口的 Origin 和无效请求头不能降级到有效查询口令的情况。使用此代理方案时，主程序只监听本机 127.0.0.1，客户端访问 Nginx。

当前交付版本为 **1.0.3**。`../html/start-windows.cmd` 和 `../html/start-linux.sh` 自动识别宿主系统及 CPU，选择引擎、持久化口令，并调用主程序的 `--setup-nginx` 完成站点发现、配置备份、检查和重载。部署用户只需复制 `index.html`、运行对应启动脚本。服务器须已有正常运行的 Nginx；整个离线运行包须保持相对目录完整。

`nginx_setup.go` 读取实际 `nginx -T` 文件清单，解析普通 `root`/`alias` 和 `include`，仅配置实际包含本工具 HTML 标记的网站。配置保留原文件身份、权限和安全标签，写入前保存备份，验证失败恢复原内容。已有位置级访问控制、未知实例或无法定位网页时会停止，不尝试绕过站点策略。主程序 `--site-path` 及启动脚本高级参数用于限定非常规部署。备份保存在程序的私有状态目录，避免进入 Nginx 的通配 include。

状态接口提供 `system`、`architecture`、`engineTarget`；Linux 启动器在进入私有运行环境之前识别宿主并传递信息，所以网页显示欧拉/麒麟/统信宿主，而非私有 rootfs 的发行版。模拟识别测试不能代替对应操作系统实际转换验收。

Word 单个文档超时 2 分钟，整个批次超时 5 分钟。超时 / 取消会清理转换进程树。无转换引擎时图片功能仍可正常使用，Word 页面提示安装引擎。就绪检查确认可执行文件存在；真实运行是否成功仍以转换结果为准。

## 信创离线运行环境

`exe/信创Linux/离线运行环境` 已包含三个架构的 LibreOffice、运行库、Noto CJK 字体、SHA-256 和来源清单。`start.sh` 调用离线启动脚本，在本地解包并建立私有 chroot，应用以 UID/GID 65534 运行。此方案不替换宿主系统库，不向欧拉/麒麟/统信安装 DEB 或 RPM；需要管理员启动、常规系统命令及内核允许的 proc 挂载。Linux 包尚未实机验证。

`tools/build_linux_offline.py` 仅供维护者在制作发布包时使用，会访问官方 Debian 源；内网用户无需运行该脚本。`tests/check_linux_runtime.py` 独立检查 ELF 解释器、依赖闭包及中文字体，不能代替目标系统实际运行验收。具体部署步骤见信创包内说明。

## 验证

```powershell
go test -v ./...
go vet ./...
python tests/integration.py --exe ../exe/Windows7/PDF工具箱-Win7.exe --out ./test-output
```

集成测试会生成合成样例，启动实际 EXE，经 HTTP 上传和下载，检查七种图片场景、中文 DOC/DOCX、PDF 页数/文字、ZIP、无效输入及鉴权。`--keep-server` 会在通过后保留服务供 UI 检查，URL 与 PID 写入测试输出目录的 `browser-session.json`，检查结束后应调用页面“退出程序”。

独立 UI 测试需 Node、Playwright 和 Edge，通过参数传入测试输出目录和本机工具 URL：

```text
node tests/ui.cjs ./test-output "http://127.0.0.1:8080/#token=测试口令"
```

测试依赖（Pillow、python-docx、pypdf、Playwright）仅开发环境需要，终端用户不需要安装。PDF 使用独立解析器检查并渲染抽查版式。结果见 `测试报告.txt` 和 `测试证据/`。

## 已知范围

图片是高质量 JPEG 光栅页，不进行 OCR；GIF/TIFF 取首帧/首页。每进程串行转换，适合个人与受控内部网络。未实现账号隔离、任务持久化或集群队列。复杂 Word 版式取决于 LibreOffice 与系统字体，不能承诺与 Microsoft Word 像素级一致。

程序界面按 ES5 和 HTML5 文件 API 编写；目标旧浏览器仍应在实际系统复核。正常退出清理临时文件；崩溃或断电不保证即时清理。Windows 7 和信创实机不在当前测试环境内。

## 官方参考

- [Go 1.20 Windows 支持](https://go.dev/doc/go1.20)
- [Go Linux 架构与内核要求](https://go.dev/wiki/Linux)
- [LibreOffice 25.2 Windows 7 SP1 支持](https://blog.documentfoundation.org/blog/2025/02/06/libreoffice-25-2/)
- [LibreOffice 命令行转换](https://help.libreoffice.org/latest/en-US/text/shared/guide/start_parameters.html)

2026-11-09 的 1.0.3 修复了 Linux 私有根目录在 umask 077 下无法被降权进程遍历的问题。Linux 入口先启动后台并通过 --check-service 检查，再发布 Nginx 配置。Nginx 访问规则只对与 PDF 路径相交的 location 做冲突判断。阿里云 Linux 3 的验证范围见 ../html/阿里云Linux3验证说明.txt。


## 重新制作离线部署包

原始离线环境保存在 ../exe/信创Linux/离线运行环境。临时压缩文件和 Go 缓存可重新生成。在源码目录中按顺序执行以下命令。

    python -B tools/optimize_runtime_compression.py ..
    python -B tools/build_alinux_release.py .. amd64
    python -B tools/build_alinux_release.py .. arm64

输出写入项目的成果目录，中间文件写入临时文件。关键校验记录位于 测试证据/离线交付校验。
项目本机清理记录和运行组件位于日志目录。
