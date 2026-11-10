from pathlib import Path
import json
import sys
from pypdf import PdfReader

root=Path(sys.argv[1])
checks=[]
colors={1:(220,53,69),2:(34,139,85),3:(37,99,235),10:(242,189,49)}
def read(name):
    reader=PdfReader(root/name,strict=True)
    assert not reader.is_encrypted
    return reader
def rgb_close(actual,expected):
    return all(abs(a-b)<8 for a,b in zip(actual,expected))
for name,order in [('ZIP数字排序.pdf',[1,2,3,10]),('手动排序.pdf',[2,3,1]),('纯静态部署.pdf',[1,2,3,10]),('本地HTML.pdf',[1,2,3,10])]:
    pdf=read(name)
    assert len(pdf.pages)==len(order),(name,len(pdf.pages))
    for page,number in zip(pdf.pages,order):
        image=list(page.images)[0].image.convert('RGB')
        pixel=image.getpixel((image.width//2,image.height//2))
        assert rgb_close(pixel,colors[number]),(name,number,pixel)
        assert abs(float(page.mediabox.width)-595.2756)<.01
        assert abs(float(page.mediabox.height)-841.8898)<.01
    checks.append(f'{name}: page order {order}, readable JPEG objects and A4 dimensions')
pdf=read('图片格式.pdf')
assert len(pdf.pages)==3
for page,expected in zip(pdf.pages,[(180,120),(225,450),(300,187.5)]):
    assert abs(float(page.mediabox.width)-expected[0])<.01
    assert abs(float(page.mediabox.height)-expected[1])<.01
alpha=list(pdf.pages[0].images)[0].image.convert('RGB')
assert rgb_close(alpha.getpixel((0,0)),(255,255,255))
rotated=list(pdf.pages[1].images)[0].image.convert('RGB')
assert rotated.size==(300,600)
assert rgb_close(rotated.getpixel((150,100)),colors[1])
assert rgb_close(rotated.getpixel((150,500)),(229,231,235))
checks.append('PNG transparency flattened to white; EXIF orientation and 96 DPI dimensions correct; WebP decoded')
word=read('Word文档.pdf')
assert len(word.pages)==2
text='\n'.join(page.extract_text() for page in word.pages)
assert 'HTML-WORD-2026' in text and 'END-OF-HTML-WORD' in text
assert '中文' in text and '第二页' in text
checks.append('DOCX: 2 pages, Chinese text and page-break markers preserved')
legacy=read('旧版DOC文档.pdf')
assert len(legacy.pages)>=1
assert any(page.extract_text().strip() for page in legacy.pages)
checks.append('DOC: readable, nonempty text PDF')
(root/'pdf-results.json').write_text(json.dumps({'checks':checks},ensure_ascii=False,indent=2),encoding='utf-8')
for check in checks: print('PASS:',check)
