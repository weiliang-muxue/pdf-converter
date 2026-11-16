"""Repackage verified legacy deliveries with the offline registry activation fix."""
from pathlib import Path
import copy, hashlib, io, json, sys, tarfile
P=Path(__file__).resolve().parents[2]
OUT=P/'成果/Nginx离线版-v1.1.1'
EXPECTED={
 'x86_64':'4beb761d07606bb029eb4e73dc180d4886d4fc56b4770f61f280c5cac8275b73',
 'aarch64':'8a245142174d65cb4d721e5c23ae0ee8374afd63375b27923b773f5e4e8ea6ee',
}
def sha(p):
 with p.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def add(t,name,data,mode=0o644):
 m=tarfile.TarInfo(name);m.size=len(data);m.mode=mode;t.addfile(m,io.BytesIO(data))
def build(arch):
 OUT.mkdir(parents=True,exist_ok=True)
 src=P/'成果'/('阿里云Linux3-'+arch+'离线部署包.tar.gz')
 assert sha(src)==EXPECTED[arch]
 dst=OUT/('pdf-converter-nginx-'+arch+'-v1.1.1.tar.gz')
 hashes={}
 with tarfile.open(src,'r|gz') as old,tarfile.open(dst,'w:gz',compresslevel=6) as new:
  for m in old:
   if m.isdir():new.addfile(m);continue
   assert m.isfile() and m.name.startswith('pdf/') and '..' not in m.name.split('/')
   rel=m.name[4:]
   if rel=='SHA256SUMS.txt':continue
   data=old.extractfile(m).read()
   if rel=='exe/信创Linux/start-offline.sh':data=(P/rel).read_bytes()
   elif rel=='发布信息.json':
    obj=json.loads(data);obj['package_version']='1.1.1';obj['fix']='Activate packaged LibreOffice registry before launch'
    data=json.dumps(obj,ensure_ascii=False,indent=2).encode()
   elif rel=='部署与验证.txt':
    data=('离线包修订 v1.1.1：修复 Word 引擎配置未激活导致的启动失败。程序版本仍为 1.0.3。\n\n'+data.decode('utf-8')).encode()
   add(new,m.name,data,m.mode);hashes[rel]=hashlib.sha256(data).hexdigest()
  add(new,'pdf/SHA256SUMS.txt',''.join(h+'  '+n+'\n' for n,h in sorted(hashes.items())).encode())
 with tarfile.open(dst,'r|gz') as t:
  seen={}
  for m in t:
   if m.isfile() and m.name!='pdf/SHA256SUMS.txt':
    seen[m.name[4:]]=hashlib.file_digest(t.extractfile(m),'sha256').hexdigest()
  assert seen==hashes
 digest=sha(dst);dst.with_name(dst.name+'.sha256').write_text(digest+'  '+dst.name+'\n',encoding='ascii')
 print(json.dumps({'archive':dst.name,'bytes':dst.stat().st_size,'sha256':digest,'verified_payloads':len(hashes)}),flush=True)
if __name__=='__main__':build(sys.argv[1])
