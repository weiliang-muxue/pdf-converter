# PDF 文件工坊

离线图片、ZIP 图片和 Word 转 PDF 工具，提供浏览器界面与 Go 后台。当前交付版本为 v1.0.3。

## 获取部署包

在服务器执行 `uname -m` 后，选择对应的 x86_64 或 aarch64 包及 SHA-256 文件。两个包分别包含完整的离线运行环境。

[下载 Release 部署包](https://github.com/weiliang-muxue/pdf-converter/releases/latest)


GitHub 附件名称与服务器架构对应。

- x86_64 使用 pdf-converter-linux-x86_64-v1.0.3.tar.gz。
- aarch64 使用 pdf-converter-linux-aarch64-v1.0.3.tar.gz。

同时下载同名 .sha256 文件。以 x86_64 包为例，校验并解压后执行下面的部署步骤。

```sh
sha256sum -c pdf-converter-linux-x86_64-v1.0.3.tar.gz.sha256
sudo tar -xzf pdf-converter-linux-x86_64-v1.0.3.tar.gz -C /opt
```

## 功能

- ZIP 内图片按数字名称排列，例如 1、2、3、10。
- 多图支持拖动、上下移动和修改页码，按列表顺序生成 PDF。
- DOC、DOCX 在内网服务器转换为 PDF。
- 图片和 ZIP 在访问者的浏览器中处理。
- Linux 启动器识别系统和 CPU，启动后台并自动配置现有 Nginx。
- 网页脚本、字体处理引擎和服务器运行库随离线包提供。

Win7、Win10、信创电脑通过兼容的现代浏览器访问同一个网址。服务器部署目标为 Alibaba Cloud Linux 3 x86_64 / ARM64。

## 部署

服务器需要已有正常运行的 Nginx，以及 root / sudo 权限。

1. 从 Release 下载对应架构的包和校验文件，校验后解压到 /opt，得到 /opt/pdf。
2. 将 /opt/pdf/html/index.html 放入现有 Nginx 网站的 pdf 子目录。
3. 执行离线包中的启动入口。

    sudo sh /opt/pdf/html/start-linux.sh

首次启动会解包私有运行环境。网页显示 Word 转换服务已就绪后，可上传文件验证。前台运行时保持该终端打开，后台启动命令见包内的“部署与验证.txt”。

[详细部署说明](html/使用说明.txt) · [阿里云 Linux 3 验证记录](html/阿里云Linux3验证说明.txt)

## 源码与构建

Go 服务、构建和测试脚本位于“源码”，网页源码位于“html/网站文件”。Linux 启动脚本和运行环境来源清单位于“exe/信创Linux”。

部署包通过 Release 分发。维护构建时准备相应的 Go 编译器和离线运行环境，具体版本和生成方法见源码说明。

[构建与验证说明](源码/README.md) · [第三方组件说明](源码/THIRD_PARTY_NOTICES.txt)

## 验证状态

已完成 Go 回归、Windows 本机转换及就绪检查、Nginx 配置验证、Linux 启动流程模拟、运行环境依赖检查和无损压缩校验。Alibaba Cloud Linux 3、Win7、信创实机的完整验收仍需在对应环境执行。
