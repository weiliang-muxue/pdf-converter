"""Static ELF/dependency check of the offline rootfs; this is not a machine test."""
import json
from pathlib import Path
import posixpath
import struct
import sys
import tarfile


def elf_info(data):
    if data[:4] != b'\x7fELF' or data[4:6] != b'\x02\x01':
        raise ValueError('Expected ELF64 little endian')
    machine = struct.unpack_from('<H', data, 18)[0]
    phoff = struct.unpack_from('<Q', data, 32)[0]
    phsize, phnum = struct.unpack_from('<HH', data, 54)
    segments = [struct.unpack_from('<IIQQQQQQ', data, phoff+i*phsize) for i in range(phnum)]
    loads = [s for s in segments if s[0] == 1]
    dynamic = next((s for s in segments if s[0] == 2), None)
    interp = next((data[s[2]:s[2]+s[5]].rstrip(b'\0').decode() for s in segments if s[0] == 3), None)
    needed, straddr = [], None
    if dynamic:
        for offset in range(dynamic[2], dynamic[2]+dynamic[5], 16):
            tag, value = struct.unpack_from('<qQ', data, offset)
            if tag == 0: break
            if tag == 1: needed.append(value)
            if tag == 5: straddr = value
    dependencies = []
    if needed:
        segment = next(s for s in loads if s[3] <= straddr < s[3]+s[5])
        string_offset = segment[2]+straddr-segment[3]
        for value in needed:
            start = string_offset+value
            dependencies.append(data[start:data.index(b'\0',start)].decode())
    return machine, interp, dependencies


def check(archive):
    arch = archive.name.removeprefix('runtime-').removesuffix('.tar.gz')
    machine_expected = {'amd64':62, 'arm64':183, 'loong64':258}[arch]
    triplet = {'amd64':'x86_64-linux-gnu','arm64':'aarch64-linux-gnu','loong64':'loongarch64-linux-gnu'}[arch]
    entries, content = {}, {}
    with tarfile.open(archive, 'r|gz') as tar:
        for item in tar:
            entries[item.name] = item
            if item.isfile() and ('.so' in posixpath.basename(item.name) or item.name.endswith('/soffice.bin') or '/ld-' in item.name):
                content[item.name] = tar.extractfile(item).read()
    def resolve(name, depth=0):
        if depth > 30: raise ValueError('Symlink loop: '+name)
        name = posixpath.normpath(name.lstrip('/'))
        parts = name.split('/')
        for i in range(len(parts)):
            prefix = '/'.join(parts[:i+1]); entry = entries.get(prefix)
            if entry and (entry.issym() or entry.islnk()):
                dest = entry.linkname
                if entry.issym() and not dest.startswith('/'):
                    dest = posixpath.join(posixpath.dirname(prefix), dest)
                rest = '/'.join(parts[i+1:])
                return resolve(posixpath.join(dest,rest),depth+1)
        return name if name in entries else None
    paths = ['usr/lib/libreoffice/program', 'lib/'+triplet, 'usr/lib/'+triplet,'lib64','usr/lib64','lib','usr/lib']
    pending = ['usr/lib/libreoffice/program/soffice.bin']; checked = set()
    while pending:
        name = resolve(pending.pop())
        if name in checked: continue
        if name not in content: raise ValueError('Missing ELF content: '+str(name))
        checked.add(name)
        machine, interpreter, dependencies = elf_info(content[name])
        assert machine == machine_expected, (name,machine,machine_expected)
        if interpreter:
            loader = resolve(interpreter)
            if not loader: raise ValueError('Missing interpreter '+interpreter)
            pending.append(loader)
        for dependency in dependencies:
            match = next((resolved for p in paths if (resolved:=resolve(p+'/'+dependency))), None)
            if not match: raise ValueError(f'Missing {dependency} required by {name}')
            pending.append(match)
    fonts = [n for n in entries if 'NotoSansCJK' in n or 'NotoSerifCJK' in n]
    assert fonts, 'No Chinese fonts bundled'
    result = {'result':'PASS','scope':'Static dependency closure only; no Linux machine execution','architecture':arch,'elf_objects_checked':len(checked),'cjk_font_files':len(fonts),'runtime':str(archive)}
    archive.with_name('runtime-'+arch+'.validation.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(result,ensure_ascii=False),flush=True)


if __name__ == '__main__':
    for name in sys.argv[1:]: check(Path(name))
