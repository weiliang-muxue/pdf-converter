"""Assemble and independently verify the offline Alibaba Cloud Linux 3 release."""
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import sys
import tarfile

project = Path(sys.argv[1]).resolve()
if len(sys.argv) != 3 or sys.argv[2] not in ['amd64', 'arm64']:
    raise SystemExit('Usage: build_alinux_release.py PROJECT amd64|arm64')
architecture = sys.argv[2]
machine_name = {'amd64':'x86_64', 'arm64':'aarch64'}[architecture]
archive_stem = '阿里云Linux3-' + machine_name + '离线部署包'
output = project / '成果'
work = project / '临时文件' / 'alinux3-release-20261110'
output.mkdir(exist_ok=True)
work.mkdir(parents=True, exist_ok=True)
version = re.search(r'const version = "([^"]+)"', (project/'源码/main.go').read_text(encoding='utf-8')).group(1)
if version != '1.0.3':
    raise RuntimeError('Review the release instructions before packaging another version')

sources = {
    'html/index.html': 'html/index.html',
    'html/start-linux.sh': 'html/start-linux.sh',
    'html/部署配置/启动Word服务-Linux.sh': 'html/部署配置/启动Word服务-Linux.sh',
    'html/部署配置/nginx-word.conf.template': 'html/部署配置/nginx-word.conf.template',
    'html/阿里云Linux3验证说明.txt': 'html/阿里云Linux3验证说明.txt',
    '第三方许可/Go.txt': '源码/third_party/GO_LICENSE.txt',
    '第三方许可/x-image.txt': '源码/vendor/golang.org/x/image/LICENSE',
    '第三方许可/JSZip.txt': 'html/网站文件/assets/JSZIP-LICENSE.txt',
}
for name in ['start.sh', 'start-offline.sh']:
    relative = 'exe/信创Linux/' + name
    sources[relative] = relative
for arch in [architecture]:
    relative = 'exe/信创Linux/pdf-tool-linux-' + arch
    sources[relative] = relative
    for suffix in ['tar.gz', 'sha256', 'manifest.json']:
        relative = f'exe/信创Linux/离线运行环境/runtime-{arch}.{suffix}'
        sources[relative] = relative

expected = {}
for folder in ['html', 'exe']:
    for line in (project/folder/'SHA256SUMS.txt').read_text(encoding='utf-8-sig').splitlines():
        if line.strip():
            digest, relative = line.split('  ', 1)
            expected[folder + '/' + relative.replace('\\', '/')] = digest

compression_dir = project/'临时文件'/'compression-20261110'
compression_results = json.loads((compression_dir/'compression-results.json').read_text(encoding='utf-8'))
compression = next(item for item in compression_results if item['architecture'] == architecture)
if compression['lossless_verification'] != 'PASS':
    raise RuntimeError('Runtime recompression was not validated')
archive_relative = f'exe/信创Linux/离线运行环境/runtime-{architecture}.tar.gz'
if compression['original_sha256'] != expected[archive_relative]:
    raise RuntimeError('Compressed runtime belongs to another source release')
if compression['compressed_bytes'] >= compression['original_bytes']:
    raise RuntimeError('Optimized runtime is not smaller than the source')
expected[archive_relative] = compression['compressed_sha256']
overrides = {}
for suffix in ['tar.gz', 'sha256', 'manifest.json']:
    relative = f'exe/信创Linux/离线运行环境/runtime-{architecture}.{suffix}'
    overrides[relative] = compression_dir/f'runtime-{architecture}.{suffix}'

def release_source(relative):
    return overrides.get(relative, project/relative)

for destination, relative in sources.items():
    source = release_source(relative).resolve()
    source.relative_to(project)
    if not source.is_file():
        raise RuntimeError('Missing release input: ' + relative)
    if source.suffix == '.sh' and b'\r' in source.read_bytes():
        raise RuntimeError('Shell script needs LF line endings: ' + relative)
    if '/离线运行环境/' in relative and relative.endswith('.tar.gz'):
        metadata = json.loads(source.with_name(source.name.replace('.tar.gz', '.manifest.json')).read_text(encoding='utf-8'))
        if metadata['runtime_sha256'] != expected[relative]:
            raise RuntimeError('Runtime manifest does not match delivery checksum')

readme = r'''阿里云 Linux 3 离线部署包 v1.0.3
2026-11-10

服务器使用 Alibaba Cloud Linux 3，包内包含 x86_64 和 ARM64 两种离线引擎。
Win7、Win10、信创电脑通过浏览器访问同一个网址。图片在客户端浏览器处理，Word 在服务器处理。
部署过程使用包内依赖。服务器需要已有正常运行的 Nginx，以及 root/sudo 权限。

首次部署示例使用 /opt/pdf 作为程序目录，/pdf/ 作为网页路径。
把压缩包和同名 .sha256 文件上传到服务器 /opt，然后执行下面的命令。

cd /opt
sudo sha256sum -c '阿里云Linux3离线部署包.tar.gz.sha256'
sudo tar -xzf '阿里云Linux3离线部署包.tar.gz' -C /opt
cd /opt/pdf
sudo sha256sum -c SHA256SUMS.txt

第一步，放好网页。
如果当前 Nginx 网站 root 是 /usr/share/nginx/html，执行下面两行。
实际网站目录不同的时候，将这两行的目标目录替换成那个目录。

sudo install -d -m 755 /usr/share/nginx/html/pdf
sudo install -m 644 /opt/pdf/html/index.html /usr/share/nginx/html/pdf/index.html

第二步，启动后台。

sudo sh -c 'umask 077; nohup sh /opt/pdf/html/start-linux.sh > /opt/pdf/html/部署配置/start-linux.log 2>&1 < /dev/null &'
sudo tail -n 50 /opt/pdf/html/部署配置/start-linux.log

脚本自动选择架构、解包运行环境、检查后台，并配置和重载 Nginx。
首次解包可能需要几分钟。成功后日志会显示 Word service is ready。
启动日志保存在程序目录内，包含服务信息和访问口令，供服务器管理员查看。
此方式在 SSH 断开后继续运行，服务器重启后需要再次执行启动命令。

浏览器验证
在 Win7、Win10、信创电脑上分别打开以下地址，替换成服务器实际 IP、域名和端口。

http://服务器内网IP/pdf/index.html
http://服务器内网IP/pdf/word-api/status

状态响应应包含 version 为 1.0.3，wordReady 为 true，server 为 true。
网页的 Word 转 PDF 标签应显示服务已就绪。

依次完成四项检查。
1. 把 1.jpg、2.jpg、3.jpg、10.jpg 压成 ZIP，生成并下载 PDF，确认四页内容按 1、2、3、10 排列。
2. 上传三张图片，把顺序调整成 3、1、2，下载 PDF 后核对实际页序。
3. 上传带中文、表格、图片和两页内容的 DOCX，核对下载结果的文字、表格、图片、页数。
4. 单独上传一个 DOC，确认旧格式也能转换和下载。
三种客户端各做一遍。使用支持现代 HTML5 的浏览器，双内核浏览器选择极速模式。
Win7 可使用已准备好的 Chrome 109 或对应兼容浏览器，Win10 和信创客户端使用其系统上可运行的现代浏览器。

可选的服务器预检查命令只检查系统识别和文件是否存在。
sudo sh /opt/pdf/html/start-linux.sh --check

判断部署结果时，以网页可访问、状态正常、实际文件转换成功为依据。
如果启动失败，先查看 html/部署配置/start-linux.log 和 service-check.log。
若提示已有实例，先核对原进程状态，再决定是否停止原服务重启。

验证范围
本包已完成内容校验、源码回归、Linux 启动流程模拟及 Windows 本机 HTTP 就绪检查。
阿里云 Linux 3、Win7、信创实机的完整转换验收仍需按上述步骤执行。
详细记录见 html/阿里云Linux3验证说明.txt。
'''
notices = '''部署包第三方组件许可\n\nGo runtime 和 golang.org/x/image 使用 BSD 类许可，许可文本在本目录。\n网页内嵌 JSZip 3.10.1，许可文本在本目录。\nLinux 运行环境包含 LibreOffice、Noto CJK / Liberation 字体及运行库，来自 Debian 官方包。\n组件名称、版本和包文件 SHA-256 记录在 runtime-架构.manifest.json。\n原始组件许可保留在运行环境的 usr/share/doc/包名/copyright。\n组件源包可按 Source 和 Version 在 https://sources.debian.org/ 或 https://snapshot.debian.org/ 查阅。\n本项目维护源码保留在原项目的“源码”目录。\n'''
readme = readme.replace('阿里云Linux3离线部署包', archive_stem)
readme = readme.replace('包内包含 x86_64 和 ARM64 两种离线引擎。', '包内提供 ' + machine_name + ' 架构对应的完整离线引擎。')
readme = readme.replace('首次部署示例使用', '在服务器执行 uname -m，确认输出为 ' + machine_name + ' 后使用本包。\n\n首次部署示例使用')
generated = {'部署与验证.txt': readme.encode('utf-8'), '第三方许可/说明.txt': notices.encode('utf-8')}
archive_name = archive_stem + '.tar.gz'
partial = work/(archive_name+'.part')
final = output/archive_name
input_size = sum(release_source(relative).stat().st_size for relative in sources.values())
if shutil.disk_usage(output).free < input_size + 100*1024*1024:
    raise RuntimeError('Insufficient E-drive space to create verified release')

class HashReader:
    def __init__(self, source):
        self.source = source
        self.digest = hashlib.sha256()
    def read(self, size=-1):
        data = self.source.read(size)
        self.digest.update(data)
        return data

checksums = {}
package_files = sorted(set(sources) | set(generated) | {'SHA256SUMS.txt', '发布信息.json'})
directories = set()
for relative in package_files:
    parent = PurePosixPath('pdf')/relative
    for directory in parent.parents:
        if str(directory) != '.':
            directories.add(str(directory))

def file_info(relative, size):
    info = tarfile.TarInfo('pdf/'+relative)
    info.size = size
    info.mode = 0o755 if relative.endswith('.sh') or '/pdf-tool-linux-' in relative else 0o644
    info.uid = info.gid = 0
    return info

print('Building complete offline release for '+machine_name, flush=True)
with tarfile.open(partial, 'w:gz', compresslevel=9, format=tarfile.PAX_FORMAT) as archive:
    for directory in sorted(directories, key=lambda value: (value.count('/'), value)):
        info = tarfile.TarInfo(directory)
        info.type = tarfile.DIRTYPE
        info.mode = 0o755
        archive.addfile(info)
    for destination, relative in sorted(sources.items()):
        source = release_source(relative)
        with source.open('rb') as handle:
            reader = HashReader(handle)
            archive.addfile(file_info(destination, source.stat().st_size), reader)
            digest = reader.digest.hexdigest()
        if relative in expected and digest != expected[relative]:
            raise RuntimeError('Source checksum mismatch: '+relative)
        checksums[destination] = digest
        if relative.endswith('.tar.gz'):
            print('Included verified runtime: '+source.name, flush=True)
    generated['发布信息.json'] = json.dumps({'version':version, 'date':'2026-11-10', 'server':'Alibaba Cloud Linux 3', 'architectures':[architecture], 'clientAccess':'browser', 'entry':'html/start-linux.sh', 'runtimeTarSHA256':compression['tar_sha256'], 'compression':'gzip level 9, original runtime tar bytes preserved', 'machineValidation':'Alibaba Cloud Linux 3 deployment has not been machine-tested'}, ensure_ascii=False, indent=2).encode('utf-8')
    for relative, data in sorted(generated.items()):
        archive.addfile(file_info(relative, len(data)), io.BytesIO(data))
        checksums[relative] = hashlib.sha256(data).hexdigest()
    manifest = ''.join(digest+'  '+name+'\n' for name,digest in sorted(checksums.items())).encode('utf-8')
    archive.addfile(file_info('SHA256SUMS.txt',len(manifest)),io.BytesIO(manifest))

print('Verifying archive paths, contents, executable modes and checksums', flush=True)
actual = {}
manifest_bytes = None
with tarfile.open(partial,'r|gz') as archive:
    for member in archive:
        name = PurePosixPath(member.name)
        if name.is_absolute() or '..' in name.parts or not member.name.startswith('pdf/') and member.name != 'pdf':
            raise RuntimeError('Invalid archive path')
        if member.isdir():
            if member.mode != 0o755:
                raise RuntimeError('Incorrect directory mode')
            continue
        if not member.isfile():
            raise RuntimeError('Unexpected non-regular archive member')
        relative = str(name.relative_to('pdf'))
        if relative in actual:
            raise RuntimeError('Duplicate archive member')
        handle = archive.extractfile(member)
        if relative == 'SHA256SUMS.txt':
            manifest_bytes = handle.read()
            continue
        digest = hashlib.sha256()
        for block in iter(lambda:handle.read(1024*1024), b''):
            digest.update(block)
        actual[relative] = digest.hexdigest()
        if (relative.endswith('.sh') or '/pdf-tool-linux-' in relative) and member.mode != 0o755:
            raise RuntimeError('Missing executable mode')
if actual != checksums or manifest_bytes != manifest:
    raise RuntimeError('Archive content validation failed')
os.replace(partial, final)
with final.open('rb') as handle:
    digest = hashlib.file_digest(handle, 'sha256').hexdigest()
(output/(archive_name+'.sha256')).write_text(digest+'  '+archive_name+'\n',encoding='utf-8',newline='\n')
result = {'result':'PASS','version':version,'archive':str(final),'bytes':final.stat().st_size,'sha256':digest,'files_verified':len(actual),'architectures':[architecture],'validation':'archive content, source checksum, permissions and layout only'}
(work/('package-verification-'+architecture+'.json')).write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps(result,ensure_ascii=False),flush=True)