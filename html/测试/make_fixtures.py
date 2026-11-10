from pathlib import Path
import io
import struct
import sys
import zipfile
from PIL import Image, ImageDraw
from docx import Document
from docx.oxml.ns import qn

root=Path(sys.argv[1]); root.mkdir(parents=True,exist_ok=True)
colors={1:'#dc3545',2:'#228b55',3:'#2563eb',10:'#f2bd31'}
for number,color in colors.items():
    image=Image.new('RGB',(320,480),color)
    ImageDraw.Draw(image).text((25,25),str(number),fill='white',font_size=60)
    image.save(root/f'{number}.png')
with zipfile.ZipFile(root/'数字照片.zip','w',zipfile.ZIP_DEFLATED) as z:
    for number in [10,3,1,2]: z.write(root/f'{number}.png',f'相册/{number}.png')
    z.writestr('说明.txt','测试 ZIP 按数字排序')
    z.writestr('__MACOSX/._1.png','skip')
with zipfile.ZipFile(root/'无图片.zip','w') as z: z.writestr('readme.txt','empty archive')
with zipfile.ZipFile(root/'不安全路径.zip','w') as z: z.write(root/'1.png','../1.png')
buf=io.BytesIO()
with zipfile.ZipFile(buf,'w',zipfile.ZIP_STORED) as z: z.write(root/'1.png','1.png')
good=buf.getvalue()
central=good.index(b'PK\x01\x02')
bad=bytearray(good); struct.pack_into('<I',bad,central+24,51*1024*1024); (root/'超大声明.zip').write_bytes(bad)
bad=bytearray(good); struct.pack_into('<I',bad,central+24,1); (root/'虚假解压大小.zip').write_bytes(bad)
bad=bytearray(good); struct.pack_into('<I',bad,central+16,0); (root/'校验失败.zip').write_bytes(bad)
bad=bytearray(good); struct.pack_into('<H',bad,central+8,1); struct.pack_into('<H',bad,6,1); (root/'加密.zip').write_bytes(bad)
(root/'损坏图片.png').write_text('invalid image',encoding='utf-8')
image=Image.new('RGBA',(240,160),(0,0,0,0)); ImageDraw.Draw(image).ellipse((30,25,200,140),fill=(10,160,90,180)); image.save(root/'透明.png')
image=Image.new('RGB',(600,300),'#e5e7eb'); ImageDraw.Draw(image).rectangle((0,0,299,299),fill='#dc3545'); exif=Image.Exif(); exif[274]=6; image.save(root/'旋转.jpg',exif=exif)
Image.new('RGB',(400,250),'#663399').save(root/'图片.webp')
doc=Document(); style=doc.styles['Normal']; style.font.name='Microsoft YaHei'; style.element.get_or_add_rPr().rFonts.set(qn('w:eastAsia'),'Microsoft YaHei')
doc.add_heading('网页 Word 转 PDF 测试',0); doc.add_paragraph('HTML-WORD-2026 / 中文文字和图片。'); doc.add_picture(str(root/'1.png'),width=__import__('docx').shared.Inches(1.5)); doc.add_page_break(); doc.add_paragraph('第二页 / END-OF-HTML-WORD'); doc.save(root/'中文文档.docx')
print('Fixtures prepared:',root)
