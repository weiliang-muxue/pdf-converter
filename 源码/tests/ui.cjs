// Isolated headless-browser application regression tests, no personal browser profile.
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

(async () => {
  const root = path.resolve(process.argv[2]);
  const url = process.argv[3];
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  const page = await browser.newPage({ viewport: { width: 1360, height: 1080 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  try {
    await page.goto(url);
    await page.getByText('图片与 Word 转换已就绪').waitFor();
    await page.screenshot({ path: path.join(root, 'ui-desktop.png'), fullPage: true });
    const fixtures = path.join(root, 'fixtures');
    const jpg = fs.readdirSync(fixtures).find(f => f.startsWith('01_'));
    const png = fs.readdirSync(fixtures).find(f => f.startsWith('02_'));
    await page.locator('#fileInput').setInputFiles([path.join(fixtures, jpg), path.join(fixtures, png)]);
    assert.equal(await page.locator('.file-row').count(), 2);
    await page.getByRole('button', { name: '上移：' + png }).click();
    assert.equal(await page.locator('.file-name').first().textContent(), png);
    await page.locator('#convertBtn').click();
    await page.locator('#resultBox').waitFor({ state: 'visible', timeout: 180000 });
    const downloadEvent = page.waitForEvent('download');
    await page.locator('#downloadLink').click();
    const download = await downloadEvent;
    await download.saveAs(path.join(root, 'ui-download.pdf'));
    assert(fs.readFileSync(path.join(root, 'ui-download.pdf')).subarray(0, 5).toString() === '%PDF-');
    await page.screenshot({ path: path.join(root, 'ui-images-result.png'), fullPage: true });
    await page.locator('#wordTab').click();
    await page.locator('#fileInput').setInputFiles(path.join(fixtures, '中文合同样例.docx'));
    await page.locator('#convertBtn').click();
    await page.locator('#resultBox').waitFor({ state: 'visible', timeout: 180000 });
    assert.equal(await page.locator('#resultName').textContent(), '中文合同样例.pdf');
    await page.screenshot({ path: path.join(root, 'ui-word-result.png'), fullPage: true });
    await page.locator('#imagesTab').click();
    await page.locator('#clearBtn').click();
    assert.equal(await page.locator('.file-row').count(), 0);
    await page.locator('#fileInput').setInputFiles(path.join(fixtures, '损坏图片.png'));
    await page.locator('#convertBtn').click();
    await page.locator('#message').waitFor({ state: 'visible', timeout: 30000 });
    assert((await page.locator('#message').textContent()).includes('损坏'));
    await page.setViewportSize({ width: 375, height: 900 });
    await page.screenshot({ path: path.join(root, 'ui-mobile.png'), fullPage: true });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'mobile overflow');
    await page.emulateMedia({ reducedMotion: 'reduce' });
    assert.equal(await page.locator('.dropzone').evaluate(e => getComputedStyle(e).transitionDuration), '0s');
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(root, 'ui-report.json'), JSON.stringify({ result:'PASS', checks:['desktop render', 'upload and reorder', 'image conversion and download', 'Word conversion', 'clear list', 'corrupt image feedback', '375px no horizontal overflow', 'reduced motion', 'no JavaScript errors'] }, null, 2));
    console.log('UI PASS: upload, reorder, PDF download, Word, corrupt input, responsive layout.');
  } finally { await browser.close(); }
})().catch(err => { console.error(err); process.exit(1); });
