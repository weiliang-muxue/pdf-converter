"""Copy the editable HTML assets into Go's embedded container website."""
from pathlib import Path
project = Path(__file__).resolve().parents[2]
source = project / 'html' / '网站文件'
target = project / '源码' / 'standaloneweb'
for name in ['index.html', 'assets/style.css', 'assets/app.js', 'assets/core.js', 'assets/jszip.min.js', 'assets/JSZIP-LICENSE.txt']:
    destination = target / name
    destination.parent.mkdir(parents=True, exist_ok=True)
    data = (source / name).read_bytes()
    if name == 'index.html':
        data = data.replace('HTML 版'.encode(), '容器版'.encode())
    destination.write_bytes(data)
print('Synced container web assets')
