"""Build offline docker-save archives from the verified bundled Linux runtime.
No daemon, base-image pull, package installation, or deployment-time download.
"""
from pathlib import Path
import argparse
import copy
import xml.etree.ElementTree as ET
import gzip
import hashlib
import io
import json
import shutil
import tarfile

VERSION = '1.1.1'
CREATED = '2026-11-16T00:00:00Z'
PROJECT = Path(__file__).resolve().parents[2]
WORK = PROJECT / '临时文件' / ('container-v' + VERSION)
OUT = PROJECT / '成果' / ('K8S镜像-v' + VERSION)

def sha(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def json_bytes(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()

def add_bytes(tar, name, data, mode=0o644):
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = mode
    info.mtime = 1794787200
    tar.addfile(info, io.BytesIO(data))

def add_path(tar, name, source, mode=0o644):
    info = tarfile.TarInfo(name)
    info.size = source.stat().st_size
    info.mode = mode
    info.mtime = 1794787200
    with source.open('rb') as stream:
        tar.addfile(info, stream)

def build(arch):
    root = WORK / arch
    root.mkdir(parents=True, exist_ok=True)
    OUT.mkdir(parents=True, exist_ok=True)
    runtime = PROJECT / 'exe' / '信创Linux' / '离线运行环境' / ('runtime-' + arch + '.tar.gz')
    manifest_file = runtime.with_name('runtime-' + arch + '.manifest.json')
    metadata = json.loads(manifest_file.read_text(encoding='utf-8'))
    if sha(runtime) != metadata['runtime_sha256']:
        raise ValueError('Offline runtime source hash mismatch')
    binary = WORK / 'bin' / ('pdf-tool-linux-' + arch)
    header = binary.read_bytes()[:64]
    if header[:4] != b'\x7fELF' or int.from_bytes(header[18:20], 'little') != {'amd64':62, 'arm64':183}[arch]:
        raise ValueError('Wrong executable architecture')
    base = root / 'runtime-layer.tar'
    original = root / 'original-runtime.tar'
    with gzip.open(runtime, 'rb') as src, original.open('wb') as dst:
        shutil.copyfileobj(src, dst, 1024 * 1024)
    # Package concatenation repeats directory headers. OCI/Docker layers must
    # contain one final entry per path, preserving the last tar entry's state.
    with tarfile.open(original, 'r:') as source, tarfile.open(base, 'w', format=tarfile.PAX_FORMAT) as target:
        final = {}
        for member in source:
            key = member.name.removeprefix('./').rstrip('/')
            if key and key != '.':
                final[key] = member
        # Debian postinst normally copies these defaults into /etc. A raw
        # offline rootfs has not run it. Activate the complete registry now.
        registry_prefix = 'usr/lib/libreoffice/share/.registry/'
        copied = 0
        for key, member in list(final.items()):
            if key.startswith(registry_prefix) and member.isfile():
                data = source.extractfile(member).read()
                if key.endswith('.xcd'):
                    ET.fromstring(data)
                destination = 'etc/libreoffice/registry/' + key[len(registry_prefix):]
                if destination not in final:
                    clone = copy.copy(member)
                    clone.name = destination
                    final[destination] = clone
                    copied += 1
        for required in ['main.xcd', 'writer.xcd', 'Langpack-en-US.xcd', 'res/fcfg_langpack_en-US.xcd']:
            assert final['etc/libreoffice/registry/' + required].isfile(), required
        print('Prepared offline LibreOffice registry:', copied, 'files', flush=True)
        for key, member in sorted(final.items(), key=lambda pair: pair[1].offset):
            member.name = key
            target.addfile(member, source.extractfile(member) if member.isfile() else None)
    base_sha = sha(base)
    app = root / 'application-layer.tar'
    with tarfile.open(app, 'w', format=tarfile.PAX_FORMAT) as tar:
        add_path(tar, 'opt/pdf-tool/pdf-tool', binary, 0o755)
        add_path(tar, 'opt/pdf-tool/licenses/GO_LICENSE.txt', PROJECT/'源码/third_party/GO_LICENSE.txt')
        add_path(tar, 'opt/pdf-tool/licenses/x-image-LICENSE.txt', PROJECT/'源码/vendor/golang.org/x/image/LICENSE')
        add_path(tar, 'opt/pdf-tool/licenses/JSZIP-LICENSE.txt', PROJECT/'html/网站文件/assets/JSZIP-LICENSE.txt')
        add_path(tar, 'opt/pdf-tool/runtime-manifest.json', manifest_file)
        add_bytes(tar, 'opt/pdf-tool/IMAGE-INFO.txt', (
            'PDF converter '+VERSION+'\nLinux '+arch+
            '\nOffline LibreOffice and fonts included.\nContainer port: 8080.\n'
            'Word PDFs return in the upload response; no persistent results.\n').encode())
    app_sha = sha(app)
    config = {
        'created': CREATED, 'architecture': arch, 'os': 'linux',
        'config': {
            'User': '65534:65534', 'WorkingDir': '/tmp',
            'Env': [
                'PATH=/usr/bin:/bin', 'HOME=/tmp', 'TMPDIR=/tmp', 'LANG=C.UTF-8',
                'XDG_CACHE_HOME=/tmp/.cache', 'SAL_USE_VCLPLUGIN=svp',
                'PDF_HOST_OS=Offline container', 'PDF_HOST_ARCH='+arch,
                'PDF_ENGINE_VARIANT=LibreOffice',
            ],
            'Entrypoint': ['/opt/pdf-tool/pdf-tool'],
            'Cmd': ['--standalone','--listen','0.0.0.0:8080','--office','/usr/lib/libreoffice/program/soffice.bin'],
            'ExposedPorts': {'8080/tcp': {}}, 'StopSignal': 'SIGTERM',
            'Labels': {
                'org.opencontainers.image.title': 'PDF converter',
                'org.opencontainers.image.version': VERSION,
                'org.opencontainers.image.source': 'https://github.com/weiliang-muxue/pdf-converter',
                'org.opencontainers.image.description': 'Offline stateless ZIP, images and Word to PDF. No Nginx dependency.',
            },
        },
        'rootfs': {'type': 'layers', 'diff_ids': ['sha256:'+base_sha, 'sha256:'+app_sha]},
        'history': [
            {'created': CREATED, 'created_by': 'Verified private offline LibreOffice runtime'},
            {'created': CREATED, 'created_by': 'PDF converter standalone application'},
        ],
    }
    config_bytes = json_bytes(config)
    config_sha = hashlib.sha256(config_bytes).hexdigest()
    image_tag = 'pdf-converter:'+VERSION+'-'+arch
    docker_manifest = [{
        'Config': config_sha+'.json', 'RepoTags': [image_tag],
        'Layers': [base_sha+'/layer.tar', app_sha+'/layer.tar'],
    }]
    raw = root / 'image.tar'
    with tarfile.open(raw, 'w', format=tarfile.PAX_FORMAT) as tar:
        add_bytes(tar, config_sha+'.json', config_bytes)
        add_bytes(tar, 'manifest.json', json_bytes(docker_manifest))
        add_path(tar, base_sha+'/layer.tar', base)
        add_path(tar, app_sha+'/layer.tar', app)
    archive = OUT/('pdf-converter-k8s-'+arch+'-v'+VERSION+'.tar.gz')
    with raw.open('rb') as src, archive.open('wb') as dst:
        with gzip.GzipFile(filename='', mode='wb', fileobj=dst, compresslevel=9, mtime=0) as gz:
            shutil.copyfileobj(src, gz, 1024 * 1024)
    digest = sha(archive)
    archive.with_name(archive.name+'.sha256').write_text(digest+'  '+archive.name+'\n',encoding='ascii')
    result = {'architecture':arch, 'tag':image_tag, 'image_id':'sha256:'+config_sha,
              'bytes':archive.stat().st_size,'sha256':digest, 'archive':archive.name,
              'runtime_sha256':metadata['runtime_sha256'], 'binary_sha256':sha(binary),
              'diff_ids':config['rootfs']['diff_ids']}
    (root/'build-result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
    # A conventional offline Dockerfile is retained with its exact build context.
    (root/'Dockerfile').write_text(
        'FROM scratch\nADD runtime-layer.tar /\nADD application-layer.tar /\n'
        'ENV PATH=/usr/bin:/bin HOME=/tmp TMPDIR=/tmp LANG=C.UTF-8 XDG_CACHE_HOME=/tmp/.cache SAL_USE_VCLPLUGIN=svp\n'
        'USER 65534:65534\nWORKDIR /tmp\nEXPOSE 8080\nSTOPSIGNAL SIGTERM\n'
        'ENTRYPOINT ["/opt/pdf-tool/pdf-tool"]\n'
        'CMD ["--standalone","--listen","0.0.0.0:8080","--office","/usr/lib/libreoffice/program/soffice.bin"]\n',
        encoding='utf-8')
    print(json.dumps(result,ensure_ascii=False),flush=True)
    return result

if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('architecture', choices=['amd64','arm64'])
    build(parser.parse_args().architecture)
