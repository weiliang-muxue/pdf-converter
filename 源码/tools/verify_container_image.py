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
    assert config['config']['Cmd'][-1]=='/usr/lib/libreoffice/program/oosplash'
    assert config['config']['StopSignal']=='SIGTERM'
    assert entry['RepoTags']==['word-pdf-'+config['config']['Labels']['com.pdf-tool.build-time']+'-'+expected_arch+':'+config['config']['Labels']['org.opencontainers.image.version']]
    assert not config['config'].get('Volumes')
    members={}
    registry_hashes={}
    for layer,wanted in zip(entry['Layers'],config['rootfs']['diff_ids']):
        assert 'sha256:'+digest(outer.extractfile(layer))==wanted
        with tarfile.open(fileobj=outer.extractfile(layer),mode='r|') as inner:
            for member in inner:
                name=member.name.removeprefix('./').rstrip('/')
                assert not name.startswith('/') and '..' not in PurePosixPath(name).parts
                if name:
                    members[name]=member
                if member.isfile() and (name.startswith('usr/lib/libreoffice/share/.registry/') or name.startswith('etc/libreoffice/registry/')):
                    registry_hashes[name]=digest(inner.extractfile(member))
                if name in ['opt/pdf-tool/pdf-tool','opt/pdf-tool/7zzs']:
                    h=inner.extractfile(member).read(64)
                    assert h[:4]==b'\x7fELF' and int.from_bytes(h[18:20],'little')=={'amd64':62,'arm64':183}[expected_arch]
    for name in ['opt/pdf-tool/pdf-tool','opt/pdf-tool/7zzs','usr/lib/libreoffice/program/soffice.bin','usr/lib/libreoffice/program/oosplash']:
        assert members[name].isfile() and members[name].mode & 0o111
    assert members['opt/pdf-tool/licenses/7zip-License.txt'].isfile()
    assert members['opt/pdf-tool/licenses/7zip-source.tar.xz'].isfile()
    for name, value in registry_hashes.items():
        prefix='usr/lib/libreoffice/share/.registry/'
        if name.startswith(prefix):
            destination='etc/libreoffice/registry/'+name[len(prefix):]
            assert registry_hashes.get(destination)==value, 'Missing or changed active registry: '+destination
    for name in ['main.xcd','writer.xcd','Langpack-en-US.xcd','res/fcfg_langpack_en-US.xcd']:
        assert members['etc/libreoffice/registry/'+name].isfile(), 'Inactive LibreOffice registry: '+name
    assert members['tmp'].mode & 0o777 == 0o777
    assert any(n.startswith('usr/share/fonts/') and n.endswith(('.ttf','.ttc','.otf')) for n in members)
    assert any(n.startswith('usr/share/doc/') and n.endswith('/copyright') for n in members)
    assert len(config['rootfs']['diff_ids'])==len(entry['Layers'])==2
print(json.dumps({'archive':path.name,'architecture':expected_arch,'sha256':archive_hash,'image_id':entry['Config'][:-5],'rootfs_entries':len(members),'verified':True}))
