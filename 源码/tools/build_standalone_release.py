
"""Package the existing standalone 1.0.3 programs without rebuilding them."""
from pathlib import Path
import hashlib
import io
import json
import os
import stat
import tarfile
import zipfile

P=Path(__file__).resolve().parents[2]
OUT=P/'成果/EXE离线版-v1.0.3'
OUT.mkdir(parents=True,exist_ok=True)
def digest(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def files(root):
    result={}
    for parent,dirs,names in os.walk(root,followlinks=False):
        for name in dirs+names:
            item=Path(parent)/name
            if getattr(item.lstat(),'st_file_attributes',0)&stat.FILE_ATTRIBUTE_REPARSE_POINT:
                raise RuntimeError('Refusing reparse point: '+str(item))
        for name in names:
            item=Path(parent)/name
            result[item.relative_to(root).as_posix()]=item
    return result
def verify_expected(path,relative):
    manifest={}
    for line in (P/'exe/SHA256SUMS.txt').read_text(encoding='utf-8-sig').splitlines():
        if line.strip():
            h,n=line.split('  ',1);manifest[n.replace('\\','/')]=h
    assert digest(path)==manifest[relative]
def windows():
    one=files(P/'exe/Windows7/LibreOffice')
    two=files(P/'exe/Windows10/LibreOffice')
    assert set(one)==set(two),'Word engine files differ; do not deduplicate'
    office_hashes={}
    for name in sorted(one):
        assert one[name].stat().st_size==two[name].stat().st_size
        a=digest(one[name]);b=digest(two[name]);assert a==b,name
        office_hashes[name]=a
    source={}
    for name,item in two.items():source['LibreOffice/'+name]=item
    for name in ['Windows7','Windows10']:
        for rel,item in files(P/'exe'/name).items():
            if rel.startswith('LibreOffice/') or rel=='开始使用.txt':continue
            source[name+'/'+rel]=item
            if item.suffix=='.exe':verify_expected(item,name+'/'+rel)
    source['许可/Go.txt']=P/'源码/third_party/GO_LICENSE.txt'
    source['许可/x-image.txt']=P/'源码/vendor/golang.org/x/image/LICENSE'
    note='''PDF 工具箱 Windows 离线版 v1.0.3
请整体解压并保留 pdf-tool-windows 目录，不要只复制 EXE。
Windows7/PDF工具箱-Win7.exe 适用于 Windows 7 SP1 32/64 位，也可供 32 位 Windows 10 使用。
Windows10/PDF工具箱-Win10.exe 适用于 64 位 Windows 10。
两个 EXE 共用上级 LibreOffice 文件夹，程序会自动查找，无需联网下载依赖。
如 Win7 提示缺少 api-ms-win-crt DLL，查看 Windows7/运行库补丁/说明.txt 并安装对应操作系统位数补丁。
本版为原独立 EXE，提供多图顺序合并和 DOC/DOCX 转换。ZIP 图片入口在 HTML/Nginx 和 K8S 镜像版。
界面由 EXE 在浏览器中打开。不需要服务器、Docker、Node、Python 或 Microsoft Office。
两个原始 EXE 已在当前 Windows 10 x64 测试；Win7 实机仍需现场验收。
'''
    archive=OUT/'pdf-tool-windows-v1.0.3.zip'
    payload={}
    with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=9,allowZip64=True) as z:
        for rel,item in sorted(source.items()):
            z.write(item,'pdf-tool-windows/'+rel)
            payload[rel]=office_hashes[rel[len('LibreOffice/'):]] if rel.startswith('LibreOffice/') else digest(item)
        z.writestr('pdf-tool-windows/使用说明.txt',note)
        payload['使用说明.txt']=hashlib.sha256(note.encode()).hexdigest()
        z.writestr('pdf-tool-windows/SHA256SUMS.txt',''.join(h+'  '+rel+'\n' for rel,h in payload.items()))
    with zipfile.ZipFile(archive,'r') as z:
        assert z.testzip() is None
        assert len(z.namelist())==len(payload)+1
    h=digest(archive)
    archive.with_name(archive.name+'.sha256').write_text(h+'  '+archive.name+'\n',encoding='ascii')
    result={'archive':archive.name,'sha256':h,'bytes':archive.stat().st_size,'payload_files':len(payload),'shared_office_files':len(office_hashes),'office_deduplication':'byte-identical Windows7/Windows10 engines','zip_crc':'PASS'}
    (OUT/'windows-package.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(result),flush=True)
def loong():
    root=P/'exe/信创Linux'
    source={}
    for name in ['start.sh','start-offline.sh','pdf-tool-linux-loong64','部署说明.txt']:
        source[name]=root/name
    for suffix in ['tar.gz','sha256','manifest.json']:
        rel='离线运行环境/runtime-loong64.'+suffix
        source[rel]=root/rel
    verify_expected(root/'pdf-tool-linux-loong64','信创Linux/pdf-tool-linux-loong64')
    verify_expected(root/'离线运行环境/runtime-loong64.tar.gz','信创Linux/离线运行环境/runtime-loong64.tar.gz')
    archive=OUT/'pdf-tool-linux-loong64-v1.0.3.tar.gz'
    with tarfile.open(archive,'w:gz',compresslevel=6,format=tarfile.PAX_FORMAT) as tar:
        for rel,item in source.items():
            info=tarfile.TarInfo('pdf-tool-linux-loong64/'+rel);info.size=item.stat().st_size
            info.mode=0o755 if rel.endswith('.sh') or rel=='pdf-tool-linux-loong64' else 0o644
            with item.open('rb') as stream:tar.addfile(info,stream)
        note='原生 Linux LoongArch64 离线版 v1.0.3。只用于上游 ABI / Linux 5.19+ 的 LoongArch64，不适用于旧 MIPS/旧 ABI。需要管理员权限，启动后程序降权运行。含完整 Word 引擎，无需联网安装。静态架构与依赖已检查，龙芯实机未验收。\n'
        data=note.encode();info=tarfile.TarInfo('pdf-tool-linux-loong64/版本说明.txt');info.size=len(data);info.mode=0o644;tar.addfile(info,io.BytesIO(data))
    with tarfile.open(archive,'r:gz') as tar:
        for member in tar:
            rel=member.name.split('/',1)[1]
            if rel in source:assert hashlib.file_digest(tar.extractfile(member),'sha256').hexdigest()==digest(source[rel])
    h=digest(archive);archive.with_name(archive.name+'.sha256').write_text(h+'  '+archive.name+'\n',encoding='ascii')
    result={'archive':archive.name,'sha256':h,'bytes':archive.stat().st_size,'payload_hashes':'PASS'}
    (OUT/'loong64-package.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
    print(json.dumps(result),flush=True)
if __name__=='__main__':
    import sys
    {'windows':windows,'loong64':loong}[sys.argv[1]]()
