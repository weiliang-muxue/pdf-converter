"""Run the standalone web application with real Office and browser checks."""
from pathlib import Path
import json
import os
import subprocess
import sys
import time
import tempfile
import socket
import urllib.request

p=Path(__file__).resolve().parents[2]
work=p/'临时文件/container-v1.1.0'
tempBase=Path(r'E:\CodeXWork\.codex-data\tmp')
tempBase.mkdir(parents=True,exist_ok=True)
temp=Path(tempfile.mkdtemp(prefix='pdfct-',dir=tempBase))
temp.mkdir(parents=True,exist_ok=True)
env=dict(os.environ,TEMP=str(temp),TMP=str(temp),TMPDIR=str(temp),PYTHONIOENCODING='utf-8',
    PLAYWRIGHT_MODULE=r'E:\CodeXWork\.codex-data\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules\playwright',
    PDF_TEST_URL='http://127.0.0.1:18891/',
    PDF_LEGACY_DOC=r'E:\CodeXWork\.build\pdf-tool-20261102\verification\fixtures\legacy\中文合同样例.doc')
log=(work/'service.log').open('wb')
process=subprocess.Popen([str(work/'bin/pdf-tool-test.exe'),'--standalone','--listen','127.0.0.1:18891',
    '--office',str(p/'exe/Windows10/LibreOffice/program/soffice.com')],cwd=p,env=env,stdout=log,stderr=log,
    creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
try:
    for attempt in range(240):
        if process.poll() is not None: raise RuntimeError('Service failed: '+(work/'service.log').read_text(encoding='utf-8'))
        try:
            with opener.open(env['PDF_TEST_URL']+'readyz',timeout=2) as response:
                if response.status==200: break
        except Exception:
            time.sleep(.5)
    else: raise RuntimeError('Service readiness failed')
    browserTemp=work/'browser-temp'
    browserTemp.mkdir(exist_ok=True)
    browserEnv=dict(env,TEMP=str(browserTemp),TMP=str(browserTemp),TMPDIR=str(browserTemp))
    subprocess.run([r'D:\nodejs\node.exe',str(p/'源码/tests/container_browser.cjs'),str(work/'fixtures'),str(work/'browser-results')],
        cwd=p,env=browserEnv,check=True)
    # Error handling must also clean every server-side input/profile/result file.
    invalid=work/'fixtures/invalid.docx'
    invalid.write_bytes(b'not-a-word-document')
    boundary='----pdf-converter-regression'
    body=('--'+boundary+'\r\nContent-Disposition: form-data; name="kind"\r\n\r\nword\r\n--'+boundary+
          '\r\nContent-Disposition: form-data; name="files"; filename="invalid.docx"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()+invalid.read_bytes()+('\r\n--'+boundary+'--\r\n').encode()
    request=urllib.request.Request(env['PDF_TEST_URL']+'word-api/render',data=body,headers={'Content-Type':'multipart/form-data; boundary='+boundary,'X-PDF-Request':'1'})
    try:
        opener.open(request,timeout=10)
        raise AssertionError('Invalid Word was accepted')
    except urllib.error.HTTPError as error:
        assert error.code==400
    leftovers=[str(f.relative_to(temp)) for f in temp.rglob('*') if f.is_file()]
    assert not leftovers, leftovers
    # Disconnect an upload while Office is running and check cleanup.
    document=(work/'fixtures/中文文档.docx').read_bytes()
    body=('--'+boundary+'\r\nContent-Disposition: form-data; name="kind"\r\n\r\nword\r\n--'+boundary+
          '\r\nContent-Disposition: form-data; name="files"; filename="cancel.docx"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()+document+('\r\n--'+boundary+'--\r\n').encode()
    connection=socket.create_connection(('127.0.0.1',18891),timeout=5)
    headers=('POST /word-api/render HTTP/1.1\r\nHost: 127.0.0.1:18891\r\nX-PDF-Request: 1\r\nContent-Type: multipart/form-data; boundary='+boundary+'\r\nContent-Length: '+str(len(body))+'\r\n\r\n').encode()
    connection.sendall(headers+body)
    for attempt in range(100):
        if list(temp.glob('pdf-tool-*/job-*/profile')): break
        time.sleep(.05)
    else: raise AssertionError('Cancel test did not reach conversion')
    connection.shutdown(socket.SHUT_RDWR); connection.close()
    for attempt in range(200):
        if not any(f.is_file() for f in temp.rglob('*')): break
        time.sleep(.1)
    else: raise AssertionError('Cancelled Word left temporary files')
    result={'server_files_after_requests':leftovers,'invalid_word_rejected':True,'cancelled_word_cleaned':True,
        'no_backend_download_endpoint':True,'test_platform':'Windows host, same application source; Linux container not executed'}
    (work/'service-results.json').write_text(json.dumps(result,indent=2),encoding='utf-8')
    subprocess.run([sys.executable,'-B',str(p/'源码/tests/verify_container_pdfs.py'),str(work/'browser-results')],cwd=p,env=env,check=True)
    print('PASS: real Word conversion, browser functionality and zero server-side files')
finally:
    subprocess.run(['taskkill.exe','/PID',str(process.pid),'/T','/F'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    try: process.wait(timeout=10)
    except subprocess.TimeoutExpired: process.kill(); process.wait()
    log.close()
