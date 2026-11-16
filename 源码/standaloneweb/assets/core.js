/* PDF Web: browser-local image conversion and bounded ZIP import. */
(function (root) {
  'use strict';
  var MAX_FILE = 50 * 1024 * 1024, MAX_TOTAL = 200 * 1024 * 1024, MAX_COUNT = 200;
  var IMAGE_EXT = /\.(jpe?g|png|bmp|gif|webp)$/i;
  var crcTable = new Uint32Array(256);
  for (var n = 0; n < 256; n++) { var c = n; for (var j = 0; j < 8; j++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; crcTable[n] = c >>> 0; }
  function crcUpdate(crc, bytes) { for (var i = 0; i < bytes.length; i++) crc = crcTable[(crc ^ bytes[i]) & 255] ^ (crc >>> 8); return crc >>> 0; }
  function crc32(bytes) { return (crcUpdate(0xffffffff, bytes) ^ 0xffffffff) >>> 0; }
  function baseName(name) { return name.replace(/\\/g, '/').split('/').pop(); }
  function naturalCompare(a, b) {
    var left = baseName(a).normalize('NFKC').toLowerCase().match(/\d+|\D+/g) || [];
    var right = baseName(b).normalize('NFKC').toLowerCase().match(/\d+|\D+/g) || [];
    for (var i = 0; i < Math.min(left.length, right.length); i++) {
      var x = left[i], y = right[i];
      if (/^\d+$/.test(x) && /^\d+$/.test(y)) {
        var nx = x.replace(/^0+(?=\d)/, ''), ny = y.replace(/^0+(?=\d)/, '');
        if (nx.length !== ny.length) return nx.length - ny.length;
        if (nx !== ny) return nx < ny ? -1 : 1;
      } else if (x !== y) return x.localeCompare(y, 'zh-CN');
    }
    return left.length - right.length || a.localeCompare(b, 'zh-CN');
  }
  function readBytes(blob) { return new Promise(function (resolve, reject) { var r = new FileReader(); r.onload = function () { resolve(new Uint8Array(r.result)); }; r.onerror = function () { reject(new Error('读取文件失败')); }; r.readAsArrayBuffer(blob); }); }
  function cancelled(signal) { if (signal && signal.cancelled) throw new Error('已取消'); }
  function decodeLegacy(bytes) {
    try { return new TextDecoder('utf-8', { fatal: true }).decode(bytes); }
    catch (_) { return new TextDecoder('gb18030').decode(bytes); }
  }
  function inspectZip(bytes) {
    if (!bytes.length || bytes.length > MAX_TOTAL) throw new Error('ZIP 压缩包必须大于 0 且不超过 200 MB');
    var view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength), end = -1;
    for (var i = bytes.length - 22; i >= Math.max(0, bytes.length - 65557); i--) {
      if (view.getUint32(i, true) === 0x06054b50 && i + 22 + view.getUint16(i + 20, true) === bytes.length) { end = i; break; }
    }
    if (end < 0) throw new Error('不是完整的 ZIP 压缩包；RAR、7Z 请先另存为 ZIP');
    var count = view.getUint16(end + 10, true), offset = view.getUint32(end + 16, true), size = view.getUint32(end + 12, true);
    if (view.getUint16(end + 4, true) || view.getUint16(end + 6, true) || view.getUint16(end + 8, true) !== count) throw new Error('不支持分卷 ZIP');
    if (count === 65535 || offset === 0xffffffff || size === 0xffffffff) throw new Error('不支持 ZIP64，请使用普通 ZIP');
    if (count > 2000 || offset + size > end) throw new Error('压缩包目录无效或文件条目过多');
    var records = [], total = 0, ignored = 0, names = new Set(), pos = offset;
    for (i = 0; i < count; i++) {
      if (pos + 46 > offset + size || view.getUint32(pos, true) !== 0x02014b50) throw new Error('ZIP 目录已损坏');
      var flags = view.getUint16(pos + 8, true), method = view.getUint16(pos + 10, true), checksum = view.getUint32(pos + 16, true);
      var compressed = view.getUint32(pos + 20, true), expanded = view.getUint32(pos + 24, true);
      var nl = view.getUint16(pos + 28, true), el = view.getUint16(pos + 30, true), cl = view.getUint16(pos + 32, true), local = view.getUint32(pos + 42, true);
      if (pos + 46 + nl + el + cl > offset + size) throw new Error('ZIP 文件名目录损坏');
      var rawName = bytes.subarray(pos + 46, pos + 46 + nl);
      var name = flags & 2048 ? new TextDecoder('utf-8', { fatal: true }).decode(rawName) : decodeLegacy(rawName);
      var extra = pos + 46 + nl, extraEnd = extra + el;
      while (extra + 4 <= extraEnd) {
        var tag = view.getUint16(extra, true), len = view.getUint16(extra + 2, true); extra += 4;
        if (extra + len > extraEnd) throw new Error('ZIP 扩展信息损坏');
        if (tag === 0x7075 && len >= 5 && bytes[extra] === 1 && view.getUint32(extra + 1, true) === crc32(rawName)) name = new TextDecoder('utf-8').decode(bytes.subarray(extra + 5, extra + len));
        extra += len;
      }
      pos += 46 + nl + el + cl;
      name = name.replace(/\\/g, '/');
      if (name.indexOf('\0') !== -1 || name[0] === '/' || /^[a-z]:/i.test(name) || name.split('/').indexOf('..') !== -1) throw new Error('ZIP 中存在不安全的文件路径');
      if (names.has(name)) throw new Error('ZIP 内有重复的文件路径：' + name);
      names.add(name);
      if (/\/$/.test(name) || /(^|\/)__MACOSX\//.test(name) || /(^|\/)\._/.test(name)) continue;
      if (!IMAGE_EXT.test(name)) { ignored++; continue; }
      if (flags & 1) throw new Error('不支持加密 ZIP，请取消密码后再上传');
      if (method !== 0 && method !== 8) throw new Error('ZIP 使用了不支持的压缩方式，请选择标准 ZIP / Deflate');
      if (!expanded || expanded > MAX_FILE) throw new Error(name + '：解压后为空或超过 50 MB');
      total += expanded;
      if (total > MAX_TOTAL || records.length >= MAX_COUNT) throw new Error('解压后的图片最多 200 张、总计 200 MB');
      if (local + 30 > offset || view.getUint32(local, true) !== 0x04034b50) throw new Error('ZIP 图片数据位置无效');
      var dataStart = local + 30 + view.getUint16(local + 26, true) + view.getUint16(local + 28, true);
      if (dataStart + compressed > offset || view.getUint16(local + 8, true) !== method || view.getUint16(local + 6, true) & 1) throw new Error('ZIP 图片数据已损坏或加密');
      records.push({ name: name, size: expanded, crc: checksum });
    }
    if (!records.length) throw new Error('ZIP 中没有 JPG、PNG、WebP、BMP 或 GIF 图片');
    records.sort(function (a, b) { return naturalCompare(a.name, b.name); });
    return { records: records, ignored: ignored };
  }
  function extractBounded(file, record, signal) {
    return new Promise(function (resolve, reject) {
      var chunks = [], length = 0, checksum = 0xffffffff, stopped = false;
      var stream = file.internalStream('uint8array');
      function fail(err) { if (stopped) return; stopped = true; stream.pause(); chunks = []; reject(err); }
      stream.on('data', function (data) {
        if (stopped) return;
        if (signal.cancelled) { fail(new Error('已取消')); return; }
        length += data.length;
        if (length > record.size || length > MAX_FILE) { fail(new Error(record.name + '：实际解压大小超出限制')); return; }
        checksum = crcUpdate(checksum, data); chunks.push(data);
      });
      stream.on('error', fail);
      stream.on('end', function () {
        if (stopped) return;
        if (length !== record.size || ((checksum ^ 0xffffffff) >>> 0) !== record.crc) { fail(new Error(record.name + '：ZIP 校验失败，图片可能已损坏')); return; }
        stopped = true; resolve(new Blob(chunks)); chunks = [];
      }); stream.resume();
    });
  }
  async function importZip(blob, signal, onProgress) {
    var bytes = await readBytes(blob); cancelled(signal); var info = inspectZip(bytes);
    var zip = await root.JSZip.loadAsync(bytes, { decodeFileName: decodeLegacy, createFolders: false, checkCRC32: false });
    var result = [];
    for (var i = 0; i < info.records.length; i++) {
      cancelled(signal); var record = info.records[i], file = zip.file(record.name);
      if (!file) throw new Error('无法读取 ZIP 条目：' + record.name);
      var imageBlob = await extractBounded(file, record, signal);
      result.push({ name: record.name, file: imageBlob }); onProgress(i + 1, info.records.length);
    }
    return { items: result, ignored: info.ignored };
  }
  function dimensions(bytes) {
    var v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    if (bytes.length > 24 && v.getUint32(0) === 0x89504e47) return [v.getUint32(16), v.getUint32(20)];
    if (bytes.length > 10 && String.fromCharCode.apply(null, bytes.subarray(0, 3)) === 'GIF') return [v.getUint16(6, true), v.getUint16(8, true)];
    if (bytes.length > 26 && bytes[0] === 66 && bytes[1] === 77) return [Math.abs(v.getInt32(18, true)), Math.abs(v.getInt32(22, true))];
    if (bytes.length > 30 && v.getUint32(0) === 0x52494646 && v.getUint32(8) === 0x57454250) {
      var chunk = String.fromCharCode.apply(null, bytes.subarray(12, 16));
      if (chunk === 'VP8X') return [1 + bytes[24] + (bytes[25] << 8) + (bytes[26] << 16), 1 + bytes[27] + (bytes[28] << 8) + (bytes[29] << 16)];
      if (chunk === 'VP8L' && bytes[20] === 0x2f) return [1 + bytes[21] + ((bytes[22] & 63) << 8), 1 + (bytes[22] >> 6) + (bytes[23] << 2) + ((bytes[24] & 15) << 10)];
      if (chunk === 'VP8 ' && bytes[23] === 0x9d && bytes[24] === 1 && bytes[25] === 0x2a) return [v.getUint16(26, true) & 16383, v.getUint16(28, true) & 16383];
    }
    if (bytes.length > 4 && bytes[0] === 255 && bytes[1] === 216) {
      var pos = 2;
      while (pos + 4 <= bytes.length) {
        if (bytes[pos] !== 255) break;
        var marker = bytes[pos + 1];
        if (marker === 255) { pos++; continue; }
        if (marker === 217 || marker === 218) break;
        var len = v.getUint16(pos + 2); if (len < 2 || pos + 2 + len > bytes.length) break;
        if ([192,193,194,195,197,198,199,201,202,203,205,206,207].indexOf(marker) !== -1 && len >= 7) return [v.getUint16(pos + 7), v.getUint16(pos + 5)];
        pos += len + 2;
      }
    }
    return null;
  }
  function validateDimensions(width, height) { if (!width || !height || width * height > 20000000 || Math.max(width, height) > 20000) throw new Error('图片最多 2000 万像素，单边不能超过 20000 像素'); }
  async function jpegImage(blob, signal) {
    var header = await readBytes(blob.slice(0, 1024 * 1024)), dim = dimensions(header);
    if (dim) validateDimensions(dim[0], dim[1]); cancelled(signal);
    var objectURL = URL.createObjectURL(blob), image = new Image();
    try {
      await new Promise(function (resolve, reject) { image.onload = resolve; image.onerror = function () { reject(new Error('图片损坏或当前浏览器无法读取此格式')); }; image.src = objectURL; });
      cancelled(signal); validateDimensions(image.naturalWidth, image.naturalHeight);
      var scale = Math.min(1, 4000 / Math.max(image.naturalWidth, image.naturalHeight));
      var width = Math.max(1, Math.round(image.naturalWidth * scale)), height = Math.max(1, Math.round(image.naturalHeight * scale));
      var canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height;
      var ctx = canvas.getContext('2d'); if (!ctx) throw new Error('浏览器无法创建绘图画布');
      ctx.fillStyle = '#ffffff'; ctx.fillRect(0, 0, width, height); ctx.drawImage(image, 0, 0, width, height);
      var jpeg = await new Promise(function (resolve, reject) { canvas.toBlob(function (value) { if (value) resolve(value); else reject(new Error('图片编码失败')); }, 'image/jpeg', 0.94); });
      canvas.width = canvas.height = 1;
      return { bytes: await readBytes(jpeg), width: width, height: height, originalWidth: image.naturalWidth, originalHeight: image.naturalHeight };
    } finally { URL.revokeObjectURL(objectURL); image.src = ''; }
  }
  function layout(width, height, paper, orientation, margin) {
    var pw = paper === 'letter' ? 612 : 595.2756, ph = paper === 'letter' ? 792 : 841.8898;
    if (paper === 'original') { var limit = Math.min(1, 14000 / (Math.max(width, height) * 0.75)); pw = width * 0.75 * limit + 2 * margin; ph = height * 0.75 * limit + 2 * margin; }
    else if (orientation === 'landscape' || orientation === 'auto' && width > height) { var temp = pw; pw = ph; ph = temp; }
    var scale = Math.min((pw - margin * 2) / width, (ph - margin * 2) / height), w = width * scale, h = height * scale;
    return { pw: pw, ph: ph, x: (pw - w) / 2, y: (ph - h) / 2, width: w, height: h };
  }
  async function imagesPDF(items, options, signal, progress) {
    if (!items.length || items.length > MAX_COUNT) throw new Error('请选择 1 至 200 张图片');
    var parts = [], count = 0, offsets = new Array(3 + 3 * items.length), encoder = new TextEncoder();
    function add(value) { var bytes = typeof value === 'string' ? encoder.encode(value) : value; parts.push(bytes); count += bytes.length; if (count > MAX_TOTAL) throw new Error('生成的 PDF 超过 200 MB，请分批转换'); }
    function object(id) { offsets[id] = count; add(id + ' 0 obj\n'); }
    function number(value) { return value.toFixed(4); }
    add('%PDF-1.4\n%\u00e2\u00e3\u00cf\u00d3\n'); object(1); add('<< /Type /Catalog /Pages 2 0 R >>\nendobj\n');
    object(2); add('<< /Type /Pages /Count ' + items.length + ' /Kids [');
    for (var i = 0; i < items.length; i++) add((3 + i * 3) + ' 0 R '); add('] >>\nendobj\n');
    for (i = 0; i < items.length; i++) {
      cancelled(signal); progress(i, items.length);
      var img;
      try { img = await jpegImage(items[i].file, signal); } catch (error) { throw new Error('第 ' + (i + 1) + ' 页（' + items[i].name + '）：' + error.message); }
      cancelled(signal);
      var page = layout(img.originalWidth, img.originalHeight, options.paper, options.orientation, options.margin * 72 / 25.4), id = 3 + i * 3;
      object(id); add('<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ' + number(page.pw) + ' ' + number(page.ph) + '] /Resources << /XObject << /Im0 ' + (id + 1) + ' 0 R >> >> /Contents ' + (id + 2) + ' 0 R >>\nendobj\n');
      object(id + 1); add('<< /Type /XObject /Subtype /Image /Width ' + img.width + ' /Height ' + img.height + ' /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ' + img.bytes.length + ' >>\nstream\n');
      add(img.bytes); add('\nendstream\nendobj\n');
      var content = 'q\n' + number(page.width) + ' 0 0 ' + number(page.height) + ' ' + number(page.x) + ' ' + number(page.y) + ' cm\n/Im0 Do\nQ\n';
      object(id + 2); add('<< /Length ' + content.length + ' >>\nstream\n' + content + 'endstream\nendobj\n');
      progress(i + 1, items.length); await new Promise(function (resolve) { setTimeout(resolve, 0); });
    }
    var xref = count; add('xref\n0 ' + offsets.length + '\n0000000000 65535 f \n');
    for (i = 1; i < offsets.length; i++) add(('0000000000' + offsets[i]).slice(-10) + ' 00000 n \n');
    add('trailer\n<< /Size ' + offsets.length + ' /Root 1 0 R >>\nstartxref\n' + xref + '\n%%EOF\n');
    return new Blob(parts, { type: 'application/pdf' });
  }
  var api = { naturalCompare: naturalCompare, inspectZip: inspectZip, importZip: importZip, imagesPDF: imagesPDF, layout: layout, dimensions: dimensions, readBytes: readBytes, imagePattern: IMAGE_EXT, maxFile: MAX_FILE, maxTotal: MAX_TOTAL, maxCount: MAX_COUNT };
  root.PDFWeb = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
