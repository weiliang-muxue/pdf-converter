// Maintainer utility. Node is only used to rebuild the HTML, never required for deployment.
const fs = require('fs');
const path = require('path');
const source = path.join(__dirname, '网站文件');
let html = fs.readFileSync(path.join(source, 'index.html'), 'utf8');
html = html.replace('<link rel="stylesheet" href="assets/style.css">', '<style>\n' + fs.readFileSync(path.join(source, 'assets/style.css'), 'utf8') + '\n</style>');
for (const name of ['jszip.min.js','core.js','app.js']) {
  const code = fs.readFileSync(path.join(source, 'assets', name), 'utf8').replace(/<\/script/gi, '<\\/script');
  html = html.replace('<script src="assets/' + name + '"></script>', () => '<script>\n' + code + '\n</script>');
}
fs.writeFileSync(path.join(__dirname, 'index.html'), html, 'utf8');
console.log('Built standalone index.html (' + Buffer.byteLength(html) + ' bytes)');
