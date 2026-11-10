/* Runs against an isolated Nginx with the supplied offline Word service. */
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fixtures = path.resolve(process.argv[2]);
const output = path.resolve(process.argv[3]);
const base = process.env.PDF_TEST_URL || 'http://127.0.0.1:18881/';
const staticBase = process.env.PDF_STATIC_URL || 'http://127.0.0.1:18882/';
fs.mkdirSync(output, { recursive: true });
const checks = [], requests = [], errors = [], external = [];
const check = name => { checks.push(name); console.log('PASS:', name); };
const file = name => path.join(fixtures, name);
let browser;
(async () => {
  browser = await chromium.launch({ headless: true, channel: process.env.BROWSER_CHANNEL || 'msedge' });
  const context = await browser.newContext({ viewport: { width: 1366, height: 1000 }, acceptDownloads: true });
  await context.route('**/*', route => {
    const url = route.request().url();
    if (/^https?:/.test(url) && !/^https?:\/\/(localhost|127\.0\.0\.1)(:|\/)/.test(url)) {
      external.push(url); return route.abort();
    }
    return route.continue();
  });
  const page = await context.newPage();
  page.on('request', req => requests.push({ method: req.method(), url: req.url() }));
  page.on('pageerror', error => errors.push(error.message));
  page.setDefaultTimeout(15000);
  async function names() { return page.locator('.file-name').allTextContents(); }
  async function upload(name) {
    await page.locator('#fileInput').setInputFiles(Array.isArray(name) ? name.map(file) : file(name));
    await page.waitForFunction(() => !document.body.classList.contains('busy'));
  }
  async function generate(name) {
    await page.locator('#convertBtn').click();
    await page.locator('#resultBox').waitFor({ state: 'visible', timeout: 90000 });
    const promise = page.waitForEvent('download');
    await page.locator('#downloadBtn').click();
    const download = await promise;
    assert.equal(await download.failure(), null);
    await download.saveAs(path.join(output, name));
    return download.suggestedFilename();
  }
  await page.goto(base);
  await page.screenshot({ path: path.join(output, '首页-桌面.png'), fullPage: true });
  await upload('数字照片.zip');
  assert.deepEqual(await names(), ['相册/1.png', '相册/2.png', '相册/3.png', '相册/10.png']);
  assert.match(await page.locator('#archiveInfo').textContent(), /忽略 1/);
  assert.equal(await generate('ZIP数字排序.pdf'), '数字照片.pdf');
  check('ZIP numeric order 1, 2, 3, 10 and downloadable PDF');
  await page.screenshot({ path: path.join(output, '压缩包-生成结果.png'), fullPage: true });
  for (const bad of ['无图片.zip','不安全路径.zip','超大声明.zip','虚假解压大小.zip','校验失败.zip','加密.zip']) {
    await upload(bad);
    await page.locator('#message').waitFor({ state: 'visible' });
    assert.equal((await names()).length, 4, 'bad ZIP must preserve previous file list');
  }
  check('Invalid, encrypted, traversal, oversized and corrupt ZIP rejected; previous list preserved');
  await page.locator('#imagesTab').click();
  await upload(['3.png', '1.png', '2.png']);
  assert.deepEqual(await names(), ['3.png', '1.png', '2.png']);
  await page.getByRole('button', { name: '上移：1.png', exact: true }).click();
  assert.deepEqual(await names(), ['1.png', '3.png', '2.png']);
  await page.getByRole('spinbutton', { name: '将 2.png 移到页码', exact: true }).fill('1');
  await page.getByRole('spinbutton', { name: '将 2.png 移到页码', exact: true }).press('Tab');
  assert.deepEqual(await names(), ['2.png', '1.png', '3.png']);
  await page.locator('.file-row').nth(2).dragTo(page.locator('.file-row').nth(1));
  assert.deepEqual(await names(), ['2.png', '3.png', '1.png']);
  await page.locator('#outputName').fill('我的图片顺序');
  await page.locator('#outputName').press('Tab');
  assert.equal(await generate('手动排序.pdf'), '我的图片顺序.pdf');
  check('Manual selection order, up button, page-number move and drag reorder');
  await page.locator('#sortBtn').click();
  assert.deepEqual(await names(), ['1.png','2.png','3.png']);
  await page.getByRole('button', { name: '移除：2.png', exact: true }).click();
  assert.deepEqual(await names(), ['1.png','3.png']);
  check('Numeric sort and remove');
  await page.locator('#clearBtn').click();
  await upload(['透明.png','旋转.jpg','图片.webp']);
  await page.locator('#paper').selectOption('original');
  await page.locator('#margin').selectOption('0');
  assert.equal(await page.locator('#orientation').isDisabled(), true);
  await generate('图片格式.pdf');
  check('Transparent PNG, EXIF-rotated JPEG and WebP; original paper dimensions');
  await page.setViewportSize({ width: 375, height: 812 });
  await page.screenshot({ path: path.join(output, '多图-手机宽度.png'), fullPage: true });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  await page.setViewportSize({ width: 1366, height: 1000 });
  await page.locator('#clearBtn').click();
  await upload('损坏图片.png');
  await page.locator('#convertBtn').click();
  await page.locator('#message').waitFor({ state: 'visible' });
  assert.match(await page.locator('#message').textContent(), /损坏|无法读取/);
  await page.locator('#clearBtn').click();
  await upload(['1.png','2.png','3.png']);
  // Pause the conversion at its public API boundary to deterministically exercise cancellation.
  await page.evaluate(() => {
    const original = PDFWeb.imagesPDF;
    PDFWeb.imagesPDF = async (...args) => { await new Promise(r => setTimeout(r, 400)); return original(...args); };
  });
  await page.locator('#convertBtn').click();
  await page.locator('#cancelBtn').click();
  await page.locator('#message').waitFor({ state: 'visible' });
  assert.match(await page.locator('#message').textContent(), /已取消/);
  assert.equal((await names()).length, 3);
  check('Corrupt image errors and cancellation preserve editable list');
  assert.equal(requests.filter(x => x.url.includes('/word-api/')).length, 0);
  check('ZIP and image conversion made no backend requests');
  await page.locator('#wordTab').click();
  await page.waitForFunction(() => document.querySelector('#wordServiceLabel').textContent.includes('已就绪'));
  await upload('中文文档.docx');
  assert.equal(await generate('Word文档.pdf'), '中文文档.pdf');
  assert(!await page.locator('#downloadBtn').getAttribute('href').then(x => x.includes('token')));
  await page.screenshot({ path: path.join(output, 'Word-生成结果.png'), fullPage: true });
  check('DOCX to PDF through real Nginx proxy with private header authentication');
  if (process.env.PDF_LEGACY_DOC) {
    await page.locator('#fileInput').setInputFiles(process.env.PDF_LEGACY_DOC);
    await generate('旧版DOC文档.pdf');
    check('Legacy DOC through Nginx');
  }
  const health = await page.request.get(base + 'word-api/status');
  const serverInfo=await health.json();
  assert.equal(serverInfo.version, '1.0.3');
  assert(serverInfo.system && serverInfo.architecture && serverInfo.engineTarget);
  assert.equal((await page.request.get(base+'word-api/exit')).status(),404);
  assert.equal((await page.request.get(base+'word-api/download?id=invalid')).status(),404);
  await page.goto(staticBase);
  await page.locator('#wordTab').click();
  await page.waitForFunction(() => document.querySelector('#wordServiceLabel').textContent.includes('尚未就绪'));
  await upload('中文文档.docx');
  assert.equal(await page.locator('#convertBtn').isDisabled(),true);
  await page.locator('#zipTab').click();
  await upload('数字照片.zip');
  await generate('纯静态部署.pdf');
  check('Static-only Nginx clearly reports Word unavailable while image PDF works');
  await page.goto(pathToFileURL(path.resolve(__dirname,'../index.html')).href);
  await upload('数字照片.zip');
  await generate('本地HTML.pdf');
  check('Single HTML file works locally without any server');
  assert.deepEqual(errors,[]);
  assert.deepEqual(external,[]);
  check('No JavaScript errors or external network requests');
  fs.writeFileSync(path.join(output,'browser-results.json'),JSON.stringify({ date: new Date().toISOString(), browser: await browser.version(), checks, errors, external, requestCount:requests.length },null,2));
})().catch(error => { console.error(error); process.exitCode=1; }).finally(async () => { if(browser) await browser.close(); });
