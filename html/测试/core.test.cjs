const assert = require('assert');
const fs = require('fs');
const path = require('path');
const core = require('../网站文件/assets/core.js');
const root = process.argv[2];
assert.deepStrictEqual(['10.jpg','2.jpg','1.jpg','3.jpg'].sort(core.naturalCompare), ['1.jpg','2.jpg','3.jpg','10.jpg']);
assert(core.naturalCompare('9007199254740992.png','9007199254740993.png') < 0);
const info = core.inspectZip(new Uint8Array(fs.readFileSync(path.join(root,'数字照片.zip'))));
assert.deepStrictEqual(info.records.map(x=>x.name),['相册/1.png','相册/2.png','相册/3.png','相册/10.png']);
assert.equal(info.ignored,1);
for (const name of ['无图片.zip','不安全路径.zip','超大声明.zip','加密.zip']) {
  assert.throws(()=>core.inspectZip(new Uint8Array(fs.readFileSync(path.join(root,name)))),name);
}
const page = core.layout(800,400,'a4','auto',10);
assert(page.pw > page.ph && Math.abs(page.width/page.height-2)<.001);
assert.deepStrictEqual(core.dimensions(new Uint8Array(fs.readFileSync(path.join(root,'1.png')))),[320,480]);
assert.deepStrictEqual(core.dimensions(new Uint8Array(fs.readFileSync(path.join(root,'图片.webp')))),[400,250]);
console.log('PASS: numeric order, ZIP metadata bounds, paths, encryption, dimensions, layout');
