"""Prepare private offline Linux runtimes. Run on the build machine only.

The deployed launcher never downloads packages. Debian package payloads are
assembled into a private chroot, without changing the target OS package database.
"""
import argparse
import concurrent.futures
import gzip
import hashlib
import io
import json
import lzma
import os
from pathlib import Path
import posixpath
import re
import tarfile
import time
import urllib.request

BASE = 'https://deb.debian.org/debian/'


def download(url, target, expected=None):
    target = Path(target)
    if target.exists() and (not expected or hashlib.sha256(target.read_bytes()).hexdigest() == expected):
        return target
    target.parent.mkdir(parents=True, exist_ok=True)
    for attempt in range(6):
        try:
            attempt_url = url if attempt < 2 else url.replace('https://deb.debian.org/debian/', 'https://ftp.debian.org/debian/')
            req = urllib.request.Request(attempt_url, headers={'User-Agent': 'PDFTool-Offline-Packager/1.0'})
            with urllib.request.urlopen(req, timeout=60) as src, open(str(target)+'.part', 'wb') as dst:
                while True:
                    chunk = src.read(1024*1024)
                    if not chunk: break
                    dst.write(chunk)
            os.replace(str(target)+'.part', target)
            if expected and hashlib.sha256(target.read_bytes()).hexdigest() != expected:
                raise ValueError('SHA256 mismatch: '+target.name)
            return target
        except Exception as error:
            if attempt == 5: raise RuntimeError('Download failed: '+url) from error
            time.sleep(1+attempt)


def fields(text):
    result, current = {}, None
    for line in text.splitlines():
        if line.startswith((' ', '\t')):
            if current: result[current] += '\n'+line
        elif ': ' in line:
            current, value = line.split(': ', 1); result[current] = value
    return result


def package_index(cache, suite, arch):
    index = download(BASE+f'dists/{suite}/main/binary-{arch}/Packages.xz', cache/f'{suite}-{arch}-Packages.xz')
    data = lzma.decompress(index.read_bytes()).decode()
    packages = {}
    for paragraph in data.split('\n\n'):
        item = fields(paragraph)
        if 'Package' in item and 'Filename' in item:
            packages[item['Package']] = item
    return packages, hashlib.sha256(index.read_bytes()).hexdigest()


def closure(packages):
    providers = {}
    for name, package in packages.items():
        for item in package.get('Provides', '').split(','):
            virtual = item.strip().split(' ')[0].split(':')[0]
            if virtual: providers.setdefault(virtual, []).append(name)
    roots = ['libreoffice-writer-nogui' if 'libreoffice-writer-nogui' in packages else 'libreoffice-writer',
             'libreoffice-core-nogui', 'fonts-noto-cjk', 'fonts-liberation', 'fontconfig', 'libc-bin', 'coreutils', 'dash']
    selected = set(roots)
    queue = list(roots)
    while queue:
        name = queue.pop()
        p = packages[name]
        dependencies = ', '.join(p.get(k, '') for k in ['Depends', 'Pre-Depends'])
        for group in dependencies.split(','):
            choices = [re.split(r'[\s:(]', choice.strip())[0] for choice in group.split('|') if choice.strip()]
            if not choices: continue
            if any(c in selected for c in choices): continue
            chosen = next((c for c in choices if c in packages), None)
            if chosen is None:
                chosen = next((provider for c in choices for provider in providers.get(c, []) if provider in selected), None)
            if chosen is None:
                chosen = next((provider for c in choices for provider in providers.get(c, [])), None)
            if chosen is None: raise ValueError(f'Unresolved dependency of {name}: {group}')
            if chosen not in selected: selected.add(chosen); queue.append(chosen)
    return [packages[name] for name in sorted(selected)]


def ar_data(path):
    with open(path, 'rb') as src:
        if src.read(8) != b'!<arch>\n': raise ValueError('Invalid DEB archive')
        while True:
            header = src.read(60)
            if not header: break
            size = int(header[48:58]); name = header[:16].decode().strip().rstrip('/')
            data = src.read(size)
            if size % 2: src.read(1)
            if name.startswith('data.tar'):
                if name.endswith('.xz'): return lzma.decompress(data)
                if name.endswith('.gz'): return gzip.decompress(data)
                if name == 'data.tar': return data
                raise ValueError('Unsupported package compression: '+name)
    raise ValueError('No payload in '+str(path))


def add_text(tar, name, text, mode=0o644):
    data = text.encode(); info = tarfile.TarInfo(name); info.size = len(data); info.mode = mode
    tar.addfile(info, io.BytesIO(data))


def build(arch, cache, output):
    suite = 'unstable' if arch == 'loong64' else 'bookworm'
    print(f'{arch}: reading package metadata ({suite})', flush=True)
    packages, index_hash = package_index(cache, suite, arch)
    chosen = closure(packages)
    manifest = {'architecture': arch, 'suite': suite, 'index_sha256': index_hash, 'packages': []}
    for p in chosen:
        manifest['packages'].append({key:p.get(key,'') for key in ['Package','Version','Source','Filename','SHA256','Size']})
    print(f'{arch}: {len(chosen)} packages, {sum(int(p["Size"]) for p in chosen)//1048576} MB compressed', flush=True)
    def get(p):
        return download(BASE+p['Filename'], cache/'packages'/Path(p['Filename']).name, p['SHA256'])
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        archives = list(executor.map(get, chosen))
    output.mkdir(parents=True, exist_ok=True)
    archive = output/f'runtime-{arch}.tar.gz'
    print(f'{arch}: assembling private runtime', flush=True)
    entries = set()
    with tarfile.open(str(archive)+'.part', 'w:gz', compresslevel=3, format=tarfile.PAX_FORMAT) as target:
        for p, deb in zip(chosen, archives):
            with tarfile.open(fileobj=io.BytesIO(ar_data(deb)), mode='r:') as payload:
                for item in payload:
                    name = item.name.removeprefix('./').rstrip('/')
                    if not name or name == '.': continue
                    if name.startswith('/') or '..' in name.split('/'): raise ValueError('Unsafe package path')
                    # Avoid huge manuals/translations; retain all copyright/license metadata.
                    if name.startswith(('usr/share/man/', 'usr/share/locale/')): continue
                    item.name = name; item.uid = item.gid = 0; item.uname = item.gname = 'root'
                    if item.issym() and item.linkname.startswith('/'):
                        item.linkname = posixpath.relpath(item.linkname.lstrip('/'), posixpath.dirname(name))
                    target.addfile(item, payload.extractfile(item) if item.isfile() else None)
                    entries.add(name)
        # Newer Debian payloads use merged /usr; these aliases supply interpreter paths.
        for name, destination in [('bin','usr/bin'),('sbin','usr/sbin'),('lib','usr/lib'),('lib64','usr/lib64')]:
            if name not in entries and not any(n.startswith(name+'/') for n in entries):
                info=tarfile.TarInfo(name); info.type=tarfile.SYMTYPE; info.linkname=destination; info.mode=0o777
                target.addfile(info); entries.add(name)
        for name in ['tmp','proc','dev','home','home/pdftool','opt','opt/pdf-tool','etc']:
            info=tarfile.TarInfo(name); info.type=tarfile.DIRTYPE; info.mode=0o1777 if name=='tmp' else 0o755
            if name=='home/pdftool': info.uid=info.gid=65534
            target.addfile(info)
        # Minimal static identity and NSS config. No package manager or host reconfiguration is needed.
        add_text(target,'etc/passwd','root:x:0:0:root:/root:/bin/sh\npdftool:x:65534:65534:PDF Tool:/home/pdftool:/bin/sh\n')
        add_text(target,'etc/group','root:x:0:\npdftool:x:65534:\n')
        add_text(target,'etc/nsswitch.conf','passwd: files\ngroup: files\nhosts: files\n')
        add_text(target,'etc/hosts','127.0.0.1 localhost\n::1 localhost\n')
        add_text(target,'etc/resolv.conf','')
        add_text(target,'opt/pdf-tool/OFFLINE-RUNTIME.txt','Private offline LibreOffice runtime. No network downloads at deployment.\n')
    required=['usr/lib/libreoffice/program/soffice.bin']
    for entry in required:
        if entry not in entries: raise ValueError('Runtime missing '+entry)
    os.replace(str(archive)+'.part', archive)
    manifest['runtime_sha256']=hashlib.sha256(archive.read_bytes()).hexdigest()
    manifest['runtime_bytes']=archive.stat().st_size
    (output/f'runtime-{arch}.manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2),encoding='utf-8')
    (output/f'runtime-{arch}.sha256').write_text(manifest['runtime_sha256']+'  '+archive.name+'\n',encoding='ascii')
    print(f'{arch}: READY {archive.stat().st_size//1048576} MB', flush=True)


if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--arch',choices=['amd64','arm64','loong64'],required=True)
    parser.add_argument('--cache',required=True); parser.add_argument('--output',required=True)
    args=parser.parse_args(); build(args.arch,Path(args.cache),Path(args.output))
