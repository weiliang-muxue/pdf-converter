"""Recompress unchanged runtime tar streams and verify lossless equivalence."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import sys
import time

project = Path(sys.argv[1]).resolve()
work = (project/'临时文件'/'compression-20261110').resolve()
work.relative_to(project)
work.mkdir(parents=True, exist_ok=True)
results = []
for architecture in ['amd64', 'arm64']:
    started = time.monotonic()
    source = project/'exe'/'信创Linux'/'离线运行环境'/f'runtime-{architecture}.tar.gz'
    metadata = json.loads(source.with_name(f'runtime-{architecture}.manifest.json').read_text(encoding='utf-8'))
    with source.open('rb') as handle:
        original_hash = hashlib.file_digest(handle, 'sha256').hexdigest()
    if original_hash != metadata['runtime_sha256']:
        raise RuntimeError('Original archive checksum mismatch: '+architecture)
    destination = work/source.name
    partial = work/(source.name+'.part')
    raw_hash = hashlib.sha256()
    expanded_bytes = 0
    print('Recompressing '+architecture+' with gzip level 9', flush=True)
    with gzip.open(source, 'rb') as reader, partial.open('wb') as target:
        with gzip.GzipFile(filename='', mode='wb', fileobj=target, compresslevel=9, mtime=0) as writer:
            while True:
                block = reader.read(1024*1024)
                if not block:
                    break
                raw_hash.update(block)
                expanded_bytes += len(block)
                writer.write(block)
    restored_hash = hashlib.sha256()
    restored_bytes = 0
    with gzip.open(partial, 'rb') as reader:
        while True:
            block = reader.read(1024*1024)
            if not block:
                break
            restored_hash.update(block)
            restored_bytes += len(block)
    if restored_hash.digest() != raw_hash.digest() or restored_bytes != expanded_bytes:
        raise RuntimeError('Lossless verification failed: '+architecture)
    os.replace(partial, destination)
    with destination.open('rb') as handle:
        compressed_hash = hashlib.file_digest(handle, 'sha256').hexdigest()
    new_metadata = dict(metadata)
    new_metadata['original_runtime_sha256'] = original_hash
    new_metadata['runtime_sha256'] = compressed_hash
    new_metadata['runtime_bytes'] = destination.stat().st_size
    new_metadata['tar_sha256'] = raw_hash.hexdigest()
    new_metadata['compression'] = 'gzip level 9, original tar bytes preserved'
    destination.with_name(f'runtime-{architecture}.manifest.json').write_text(json.dumps(new_metadata,ensure_ascii=False,indent=2),encoding='utf-8')
    destination.with_name(f'runtime-{architecture}.sha256').write_text(compressed_hash+'  '+destination.name+'\n',encoding='utf-8',newline='\n')
    result = {
        'architecture':architecture,
        'original_bytes':source.stat().st_size,
        'compressed_bytes':destination.stat().st_size,
        'saved_bytes':source.stat().st_size-destination.stat().st_size,
        'saved_percent':round(100*(1-destination.stat().st_size/source.stat().st_size),2),
        'original_sha256':original_hash,
        'compressed_sha256':compressed_hash,
        'tar_sha256':raw_hash.hexdigest(),
        'expanded_bytes':expanded_bytes,
        'lossless_verification':'PASS',
        'elapsed_seconds':round(time.monotonic()-started,1),
    }
    results.append(result)
    (work/'compression-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(result,ensure_ascii=False),flush=True)