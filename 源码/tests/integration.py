"""Real EXE/HTTP/LibreOffice integration test. Uses Pillow, python-docx and pypdf."""
import argparse
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile

from PIL import Image, ImageDraw, ImageFont
from docx import Document
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from pypdf import PdfReader


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--exe', required=True)
    parser.add_argument('--out', required=True)
    parser.add_argument('--keep-server', action='store_true')
    args = parser.parse_args()
    root = Path(args.out).resolve()
    root.mkdir(parents=True, exist_ok=True)
    fixtures = root / 'fixtures'
    fixtures.mkdir(exist_ok=True)
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    office = Path(args.exe).resolve().parent / 'LibreOffice/program/soffice.com'
    font_path = Path('C:/Windows/Fonts/msyh.ttc')
    font = ImageFont.truetype(str(font_path), 36) if font_path.exists() else ImageFont.load_default()
    images = []
    for i, fmt in enumerate(['JPEG', 'PNG', 'BMP', 'GIF', 'TIFF']):
        im = Image.new('RGB', (800, 500) if i % 2 == 0 else (500, 800), '#e7effa')
        draw = ImageDraw.Draw(im)
        draw.rounded_rectangle((35, 35, im.width-35, im.height-35), radius=20, fill='#2563eb')
        draw.text((70, 75), f'{i+1:02d} / {fmt}', font=font, fill='white')
        draw.text((70, 140), '图片合并测试', font=font, fill='white')
        path = fixtures / f'{i+1:02d}_测试图片.{fmt.lower()}'
        im.save(path, fmt)
        images.append(path)
    transparent = fixtures / '06_透明背景.png'
    im = Image.new('RGBA', (400, 400), (0, 0, 0, 0))
    ImageDraw.Draw(im).ellipse((50, 50, 350, 350), fill=(34, 135, 97, 210))
    im.save(transparent)
    images.append(transparent)
    rotated = fixtures / '07_手机照片.jpg'
    im = Image.new('RGB', (600, 300), '#dbeafe')
    ImageDraw.Draw(im).rectangle((0, 0, 300, 300), fill='#dc3545')
    exif = Image.Exif(); exif[274] = 6
    im.save(rotated, exif=exif)
    images.append(rotated)
    doc = Document()
    style = doc.styles['Normal']
    style.font.name = 'Microsoft YaHei'
    style.element.get_or_add_rPr().rFonts.set(qn('w:eastAsia'), 'Microsoft YaHei')
    doc.add_heading('中文 Word 转 PDF 测试', 0)
    doc.add_paragraph('合同编号：PDF-2026-001。测试中文、英文、数字及表格。')
    table = doc.add_table(rows=1, cols=3)
    table.style = 'Table Grid'
    for cell, text in zip(table.rows[0].cells, ['序号', '项目', '结果']): cell.text = text
    for i, name in enumerate(['中文字体', '表格边框', '分页与图片'], 1):
        cells = table.add_row().cells
        for cell, text in zip(cells, [str(i), name, '正常']): cell.text = text
    doc.add_picture(str(images[0]), width=__import__('docx').shared.Inches(3.8))
    doc.add_page_break()
    doc.add_heading('第二页：分页验证', 1)
    doc.add_paragraph('END-OF-DOCUMENT / 文档转换完成。')
    docx = fixtures / '中文合同样例.docx'
    doc.save(docx)
    # Produce an actual legacy .doc using the bundled engine, not a renamed DOCX.
    legacy = fixtures / 'legacy'
    legacy.mkdir(exist_ok=True)
    subprocess.run([str(office), '-env:UserInstallation=' + (root / ('legacy-profile-' + uuid.uuid4().hex)).as_uri(),
                    '--headless', '--convert-to', 'doc:MS Word 97', '--outdir', str(legacy), str(docx)],
                   check=True, timeout=180, creationflags=flags, capture_output=True)
    legacy_doc = legacy / (docx.stem + '.doc')
    assert legacy_doc.exists(), 'Legacy DOC fixture was not created'
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    token = 'pdf-integration-test-' + uuid.uuid4().hex
    base = f'http://127.0.0.1:{port}'
    env = os.environ.copy(); env['PDF_TOOL_TOKEN'] = token
    log = open(root / 'server.log', 'w', encoding='utf-8')
    process = subprocess.Popen([args.exe, '--no-browser', '--listen', f'127.0.0.1:{port}'], env=env,
                               stdout=log, stderr=log, creationflags=flags)
    evidence = {'host': 'Windows 10 x64', 'executable': str(Path(args.exe).resolve()), 'checks': []}
    def request(path, data=None, content_type=None, auth=True):
        req = urllib.request.Request(base+path, data=data)
        if auth: req.add_header('X-PDF-Token', token)
        if content_type: req.add_header('Content-Type', content_type)
        return urllib.request.urlopen(req, timeout=180)
    def convert(kind, paths, **settings):
        boundary = 'boundary' + uuid.uuid4().hex
        parts = []
        for name, value in dict(kind=kind, **settings).items():
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
        for path in paths:
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="files"; filename="{path.name}"\r\nContent-Type: application/octet-stream\r\n\r\n'.encode())
            parts.append(path.read_bytes()); parts.append(b'\r\n')
        parts.append(f'--{boundary}--\r\n'.encode())
        response = request('/api/convert', b''.join(parts), 'multipart/form-data; boundary='+boundary)
        metadata = json.load(response)
        download = request('/api/download?'+urllib.parse.urlencode({'id':metadata['id'], 'token':token}), auth=False)
        assert 'attachment' in download.headers['Content-Disposition']
        return metadata, download.read()
    try:
        for _ in range(60):
            try:
                status = json.load(request('/api/status')); break
            except (OSError, urllib.error.URLError): time.sleep(.2)
        else: raise RuntimeError('EXE did not start')
        assert status['wordReady'] is True
        evidence['platform'] = status['platform']
        evidence['checks'].append('Bundled LibreOffice detected')
        meta, data = convert('images', images, paper='a4', orientation='auto', margin='10')
        (root / 'images.pdf').write_bytes(data)
        pdf = PdfReader(io.BytesIO(data), strict=True)
        assert len(pdf.pages) == 7
        assert float(pdf.pages[0].mediabox.width) > float(pdf.pages[0].mediabox.height)
        assert float(pdf.pages[1].mediabox.width) < float(pdf.pages[1].mediabox.height)
        assert float(pdf.pages[6].mediabox.width) < float(pdf.pages[6].mediabox.height), 'EXIF rotation lost'
        image0 = list(pdf.pages[0].images)[0].image
        assert image0.width == 800 and image0.height == 500
        alpha = list(pdf.pages[5].images)[0].image.convert('RGB')
        assert min(alpha.getpixel((0, 0))) > 245, 'Alpha background is not white'
        evidence['checks'].append('7 images: JPG/PNG/BMP/GIF/TIFF, EXIF rotation, transparency, page order, strict PDF parsing')
        for paper, orientation in [('letter', 'portrait'), ('original', 'auto')]:
            _, data = convert('images', images[:1], paper=paper, orientation=orientation, margin='0')
            page = PdfReader(io.BytesIO(data), strict=True).pages[0]
            expected = (612,792) if paper=='letter' else (600,375)
            assert abs(float(page.mediabox.width)-expected[0]) < .1
            assert abs(float(page.mediabox.height)-expected[1]) < .1
        evidence['checks'].append('Letter portrait and original-size pages with zero margins')
        for path, name in [(docx, 'word-docx.pdf'), (legacy_doc, 'word-doc.pdf')]:
            _, data = convert('word', [path])
            (root / name).write_bytes(data)
            pdf = PdfReader(io.BytesIO(data), strict=True)
            assert len(pdf.pages) == 2, f'{name}: expected 2 pages'
            content = '\n'.join(page.extract_text() for page in pdf.pages)
            assert 'PDF-2026-001' in content and 'END-OF-DOCUMENT' in content
            assert '中文' in content and '表格' in content
            evidence['checks'].append(f'{name}: two pages, Chinese text, table, image and page break')
        meta, data = convert('word', [docx, legacy_doc])
        (root / 'batch.zip').write_bytes(data)
        with zipfile.ZipFile(io.BytesIO(data)) as z:
            assert len(z.namelist()) == 2 and len(set(z.namelist())) == 2
            for name in z.namelist(): assert len(PdfReader(io.BytesIO(z.read(name)), strict=True).pages) == 2
        evidence['checks'].append('Batch DOC/DOCX: ZIP contains two PDFs with distinct names')
        bad = fixtures / '损坏图片.png'; bad.write_text('not an image', encoding='utf-8')
        try: convert('images', [bad]); raise AssertionError('Invalid image accepted')
        except urllib.error.HTTPError as e: assert e.code == 400
        try: request('/api/status', auth=False); raise AssertionError('Unauthenticated API accepted')
        except urllib.error.HTTPError as e: assert e.code == 401
        evidence['checks'].append('Corrupt-file error and unauthenticated-request rejection')
        evidence['result'] = 'PASS'
        (root / 'report.json').write_text(json.dumps(evidence, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(evidence, ensure_ascii=False, indent=2))
        if args.keep_server:
            (root / 'browser-session.json').write_text(json.dumps({'url':base+'/#token='+token,'pid':process.pid},ensure_ascii=False),encoding='utf-8')
    finally:
        if not args.keep_server or evidence.get('result') != 'PASS':
            try: request('/api/exit', b'')
            except Exception: process.terminate()
            process.wait(timeout=15)
        log.close()


if __name__ == '__main__':
    main()
