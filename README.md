# PDF 文件工坊

离线 PDF 转换工具，保留三种交付方式，当前主要交付是 K8S 独立镜像。
v1.1.2 已通过 amd64 / ARM64 原生 Linux 最终验收。v1.1.0 和 v1.1.1 为失败候选，请勿部署。

## 下载

[统一下载页：镜像、EXE、Nginx 离线包](https://github.com/weiliang-muxue/pdf-converter/releases/tag/v1.1.2)

- K8S：pdf-converter-k8s-amd64-v1.1.2.tar.gz 或 pdf-converter-k8s-arm64-v1.1.2.tar.gz。
- Windows EXE：pdf-tool-windows-v1.0.3.zip，包含 Win7、Win10 程序和完整共用 Word 引擎。
- HTML/Nginx：pdf-converter-nginx-x86_64-v1.1.2.tar.gz 或 pdf-converter-nginx-aarch64-v1.1.2.tar.gz。
- 原生龙芯：pdf-tool-linux-loong64-v1.1.2.tar.gz；上游 LoongArch64 ABI，实机未验收。

每份归档提供同名 .sha256 校验文件。大体积程序和依赖在 Release 附件，维护源码在本仓库。
Nginx 和龙芯包是对原 1.0.3 程序的离线配置修订，程序本身仍显示 1.0.3。
原 EXE 支持多图排序和 Word 转换；ZIP 图片入口在 HTML/Nginx 和镜像版。

## K8S 镜像

镜像内置完整网页、转换程序、LibreOffice、中文字体和运行库，运行期间无需联网下载依赖。
容器端口 8080，通过现有 Service/SLB 暴露后访问 IP:端口，无需宿主机 Nginx。

- x86_64 节点选择 pdf-converter:1.1.2-amd64；aarch64 节点选择 pdf-converter:1.1.2-arm64。
- 两种镜像功能相同，只对应服务器 CPU；Win7、Win10、信创客户端访问同一个网址。
- 不配置外部 volumeMounts、hostPath、PVC 或共享目录。临时文件使用 Pod 自身可写层，不启用 readOnlyRootFilesystem。
- 镜像上传到所有候选节点可访问的内网镜像仓库；扩容不需要往各台 ECS 上传文件。
- SLB/K8S 负责流量分发，应用不加入轮询、共享结果状态或会话粘滞。

Word PDF 在同一次上传请求中直接返回，浏览器下载使用本地 Blob，不再请求原 Pod。
上传文件、转换结果和引擎临时配置在请求完成、失败或取消后清理。
每个 Pod 同时执行一个 Word 转换，繁忙时返回明确提示，可按负载扩容。

健康接口为 /healthz 和 /readyz；启动时真实转换测试 Word 后才就绪。
收到 SIGTERM 时撤销就绪并等待在途请求，建议 terminationGracePeriodSeconds 为 400，代理超时至少 360 秒。
强制杀死 Pod 的在途请求仍需重试；已经返回浏览器的 PDF 不受影响。

[完整镜像接入参数](源码/container/镜像说明.txt) · [原生 Linux 验收](https://github.com/weiliang-muxue/pdf-converter/actions/workflows/container-smoke.yml)

## 网页功能

ZIP 内图片按数字名称排列，例如 1、2、3、10；多图支持拖动、上下移动和修改页码。
PDF 按当前列表顺序生成。图片和 ZIP 在浏览器处理，不上传服务器。
DOC/DOCX 通过内网服务器转换为 PDF。页面及脚本均在包内，不使用 CDN 或在线转换服务。

## 原 EXE 和 Nginx 方式

EXE 压缩包需完整解压，保留共用 LibreOffice 目录。Win7 SP1 离线补丁也在包内。
Nginx 包按服务器 uname -m 选择架构，完整解压后放置 index.html，并运行随包启动脚本。
原 Nginx 方案需要已有 Nginx 及 root/sudo 权限；其原下载方式不用于 K8S 多副本交付。

[原 Nginx 部署说明](html/使用说明.txt) · [阿里云 Linux 3 验证范围](html/阿里云Linux3验证说明.txt)

## 源码和验证范围

Go 服务、构建和测试脚本位于“源码”；网页可编辑源位于“html/网站文件”；Linux 入口和依赖清单位于“exe/信创Linux”。

已验证浏览器 ZIP 数字顺序、多图排序、DOC/DOCX、断开后下载、取消清理及无外部网络请求。
当前镜像已修复 LibreOffice 配置激活和官方启动器入口；默认配置在禁网、非 root、零外部挂载条件下通过真实 DOC/DOCX 转换、取消清理和正常退出验收。

[最终验收运行记录](https://github.com/weiliang-muxue/pdf-converter/actions/runs/43838873267) · [完整验收证据](源码/测试证据/容器-v1.1.2/validation.json)
实际 Alibaba Cloud Linux 3/K8S/SLB、Win7 和龙芯硬件未连接验收，不把静态兼容性检查当作实机认证。

[构建与验证说明](源码/README.md) · [第三方组件说明](源码/THIRD_PARTY_NOTICES.txt)
