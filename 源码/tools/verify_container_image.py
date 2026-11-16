"""Independent content verification of an exported Docker image."""
from pathlib import Path, PurePosixPath
import hashlib
import json
import sys
import tarfile

path = Path(sys.argv[1]).resolve()
expected_arch = sys.argv[2]
def digest(stream):
    return hashlib.file_digest(stream,'sha256').hexdigest()
with path.open('rb') as f:
    archive_hash=digest(f)
expected=path.with_name(path.name+'.sha256').read_text(encoding='ascii').split()[0]
assert expected == archive_hash
with tarfile.open(path,'r:gz') as outer:
    manifest=json.load(outer.extractfile('manifest.json'))
    assert len(manifest)==1
    entry=manifest[0]
    data=outer.extractfile(entry['Config']).read()
    assert hashlib.sha256(data).hexdigest()+'.json'==entry['Config']
    config=json.loads(data)
    assert config['os']=='linux' and config['architecture']==expected_arch
    assert config['config']['User']=='65534:65534'
    assert config['config']['ExposedPorts']=={'8080/tcp':{}}
    assert '--standalone' in config['config']['Cmd']
    assert config['config']['StopSignal']=='SIGTERM'
    assert entry['RepoTags']==['pdf-converter:1.1.0-'+expected_arch]
    assert not config['config'].get('Volumes')
    members={}
    for layer,wanted in zip(entry['Layers'],config['rootfs']['diff_ids']):
        assert 'sha256:'+digest(outer.extractfile(layer))==wanted
        with tarfile.open(fileobj=outer.extractfile(layer),mode='r|') as inner:
            for member in inner:
                name=member.name.removeprefix('./').rstrip('/')
                assert not name.startswith('/') and '..' not in PurePosixPath(name).parts
                if name:
                    members[name]=member
                if name=='opt/pdf-tool/pdf-tool':
                    h=inner.extractfile(member).read(64)
                    assert h[:4]==b'\x7fELF' and int.from_bytes(h[18:20],'little')=={'amd64':62,'arm64':183}[expected_arch]
    for name in ['opt/pdf-tool/pdf-tool','usr/lib/libreoffice/program/soffice.bin']:
        assert members[name].isfile() and members[name].mode & 0o111
    assert members['tmp'].mode & 0o777 == 0o777
    assert any(n.startswith('usr/share/fonts/') and n.endswith(('.ttf','.ttc','.otf')) for n in members)
    assert any(n.startswith('usr/share/doc/') and n.endswith('/copyright') for n in members)
    assert len(config['rootfs']['diff_ids'])==len(entry['Layers'])==2
print(json.dumps({'archive':path.name,'architecture':expected_arch,'sha256':archive_hash,'image_id':entry['Config'][:-5],'rootfs_entries':len(members),'verified':True}))
