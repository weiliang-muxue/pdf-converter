(function () {
  'use strict';
  var core = window.PDFWeb, mode = 'zip', groups = { zip: [], images: [], word: [] }, names = { zip: '图片合并', images: '图片合并', word: '' };
  var zipLabel = '', busy = false, signal = { cancelled: false }, xhr = null, resultURL = null, wordReady = false, serviceChecking = false, dragIndex = -1;
  function el(id) { return document.getElementById(id); }
  function text(id, value) { el(id).textContent = value; }
  function show(id, value) { el(id).hidden = !value; }
  function sizeText(value) { return value < 1048576 ? (value / 1024).toFixed(1) + ' KB' : (value / 1048576).toFixed(1) + ' MB'; }
  function total(items) { return items.reduce(function (sum, item) { return sum + item.file.size; }, 0); }
  function message(value, info) { text('message', value); el('message').className = 'message' + (info ? ' info' : ''); show('message', !!value); }
  function invalidate() { if (resultURL) URL.revokeObjectURL(resultURL); resultURL = null; el('downloadBtn').removeAttribute('href'); show('resultBox', false); message(''); }
  function apiPath(name) { return 'word-api/' + name; }
  function api(method, name, data, callback, isConvert) {
    var request = new XMLHttpRequest(); request.open(method, apiPath(name), true); request.timeout = isConvert ? 360000 : 8000;
    request.onload = function () { var result; try { result = JSON.parse(request.responseText); } catch (_) { result = null; }
      if (request.status >= 200 && request.status < 300 && result) callback(null, result);
      else callback(new Error(result && result.error ? result.error : 'Word 转换服务未连接，请联系管理员启用服务。图片功能可独立使用。'));
    };
    request.onerror = function () { callback(new Error('无法连接 Word 服务，请确认当前内网连接和服务状态')); };
    request.ontimeout = function () { callback(new Error(isConvert ? 'Word 转换超时，请稍后重试或检查文档' : 'Word 服务检查超时')); };
    request.onabort = function () { callback(new Error('已取消')); };
    if (isConvert) {
      request.upload.onprogress = function (event) { if (event.lengthComputable) { el('progress').value = event.loaded / event.total * 75; text('progressText', '正在上传 Word：' + Math.round(event.loaded / event.total * 100) + '%'); } };
      request.upload.onload = function () { el('progress').removeAttribute('value'); text('progressText', 'Word 已上传，正在转换，请稍候…'); };
    }
    request.send(data || null); return request;
  }
  function checkService() {
    if (serviceChecking) return;
    serviceChecking = true; wordReady = false; text('wordServiceLabel', '正在检查 Word 转换服务…'); text('wordServiceHint', 'Word 文件由内网服务器处理。'); update();
    if (location.protocol === 'file:') { serviceChecking = false; text('wordServiceLabel', '当前为本地 HTML 文件'); text('wordServiceHint', '图片功能可以直接使用；Word 请通过部署好的内网网页访问。'); update(); return; }
    api('GET', 'status', null, function (error, data) {
      serviceChecking = false; wordReady = !error && !!data.wordReady;
      text('wordServiceLabel', wordReady ? 'Word 转换服务已就绪' : 'Word 转换服务尚未就绪');
      var serverLabel = data && data.system ? '已连接：' + data.system + ' / ' + data.architecture + (data.engineTarget ? '，自动匹配 ' + data.engineTarget + '。' : '。') : '';
      text('wordServiceHint', wordReady ? serverLabel + '文档在内网转换，结果保留 60 分钟。' : '请联系管理员启用 Word 转换服务。压缩包和图片功能不受影响。');
      el('wordNotice').className = 'notice' + (wordReady ? ' ready' : ''); update();
    });
  }
  function update() {
    var list = groups[mode], isWord = mode === 'word';
    text('count', String(list.length)); show('empty', !list.length); show('settings', !isWord); show('wordNotice', isWord);
    show('archiveInfo', mode === 'zip' && !!zipLabel); text('archiveInfo', zipLabel);
    el('clearBtn').disabled = busy || !list.length; el('sortBtn').disabled = busy || list.length < 2; show('sortBtn', !isWord);
    el('chooseBtn').disabled = busy; el('convertBtn').disabled = busy || !list.length || (isWord && !wordReady);
    ['zipTab','imagesTab','wordTab','paper','margin','outputName'].forEach(function (id) { el(id).disabled = busy; });
    el('orientation').disabled = busy || el('paper').value === 'original'; el('retryService').disabled = busy || serviceChecking;
    text('summary', list.length ? (isWord ? '已选择 1 个 Word 文档' : '已排列 ' + list.length + ' 页') + ' · ' + sizeText(total(list)) : '等待添加文件');
    text('summaryHint', isWord ? '由内网服务器处理，转换后下载 PDF' : '按列表顺序生成，图片保留在当前浏览器');
    document.body.className = busy ? 'busy' : '';
  }
  function revokeItems(items) { items.forEach(function (item) { if (item.preview) URL.revokeObjectURL(item.preview); }); }
  function moveItem(from, to) {
    if (busy || from === to || from < 0 || to < 0 || from >= groups[mode].length || to >= groups[mode].length) return;
    var item = groups[mode].splice(from, 1)[0]; groups[mode].splice(to, 0, item); invalidate(); render();
  }
  function render() {
    var list = el('fileList'); list.textContent = '';
    groups[mode].forEach(function (item, index) {
      var row = document.createElement('li'); row.className = 'file-row'; row.draggable = !busy && mode !== 'word';
      if (mode !== 'word') {
        var grip = document.createElement('span'); grip.className = 'grip'; grip.textContent = '⠿'; grip.setAttribute('aria-hidden', 'true'); row.appendChild(grip);
        var position = document.createElement('div'); position.className = 'page-index';
        var input = document.createElement('input'); input.type = 'number'; input.min = '1'; input.max = String(groups[mode].length); input.value = String(index + 1); input.disabled = busy;
        input.setAttribute('aria-label', '将 ' + item.name + ' 移到页码');
        input.onchange = function () { var page = Math.floor(Number(input.value)); if (page >= 1 && page <= groups[mode].length) moveItem(index, page - 1); else input.value = String(index + 1); };
        var caption = document.createElement('small'); caption.textContent = '页'; position.appendChild(input); position.appendChild(caption); row.appendChild(position);
      }
      var preview;
      if (mode !== 'word') { if (!item.preview) item.preview = URL.createObjectURL(item.file); preview = document.createElement('img'); preview.src = item.preview; preview.alt = ''; preview.className = 'thumb'; preview.loading = 'lazy'; preview.draggable = false;
        preview.onerror = function () { var fallback = document.createElement('span'); fallback.className = 'thumb doc-thumb'; fallback.textContent = '图片'; if (preview.parentNode) preview.parentNode.replaceChild(fallback, preview); };
      } else { preview = document.createElement('span'); preview.className = 'thumb doc-thumb'; preview.textContent = 'W'; }
      row.appendChild(preview);
      var details = document.createElement('div'); details.className = 'file-details'; var name = document.createElement('span'); name.className = 'file-name'; name.textContent = item.name; name.title = item.name;
      var size = document.createElement('span'); size.className = 'file-size'; size.textContent = sizeText(item.file.size); details.appendChild(name); details.appendChild(size); row.appendChild(details);
      var actions = document.createElement('div'); actions.className = 'row-actions';
      function button(label, title, disabled, action, className) { var b = document.createElement('button'); b.type = 'button'; b.textContent = label; b.title = title; b.setAttribute('aria-label', title + '：' + item.name); b.disabled = busy || disabled; b.onclick = action; b.className = className || ''; actions.appendChild(b); }
      if (mode !== 'word') { button('↑', '上移', index === 0, function () { moveItem(index, index - 1); }); button('↓', '下移', index === groups[mode].length - 1, function () { moveItem(index, index + 1); }); }
      button('移除', '移除', false, function () { revokeItems([item]); groups[mode].splice(index, 1); invalidate(); render(); }, 'remove'); row.appendChild(actions);
      row.ondragstart = function (event) { if (busy || mode === 'word' || event.target.tagName === 'INPUT') { event.preventDefault(); return; } dragIndex = index; event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', String(index)); row.classList.add('dragging'); };
      row.ondragover = function (event) { if (dragIndex < 0 || busy) return; event.preventDefault(); event.dataTransfer.dropEffect = 'move'; row.classList.add('drag-target'); };
      row.ondragleave = function () { row.classList.remove('drag-target'); };
      row.ondragend = function () { dragIndex = -1; row.classList.remove('dragging'); row.classList.remove('drag-target'); };
      row.ondrop = function (event) { if (dragIndex < 0 || busy) return; event.preventDefault(); var from = dragIndex; dragIndex = -1; moveItem(from, index); };
      list.appendChild(row);
    }); update();
  }
  function setBusy(value, label) { busy = value; show('progressBox', value); if (value) { text('progressText', label); el('progress').value = 0; } render(); }
  async function receiveFiles(files) {
    if (busy || !files || !files.length) return;
    if (mode === 'zip') {
      if (files.length !== 1 || !/\.zip$/i.test(files[0].name)) { message('请选择一个普通 ZIP 压缩包。RAR、7Z 请先另存为 ZIP。'); return; }
      if (files[0].size > core.maxTotal) { message('ZIP 文件不能超过 200 MB'); return; }
      var file = files[0]; signal = { cancelled: false }; invalidate(); setBusy(true, '正在读取 ZIP 图片目录…');
      try {
        var imported = await core.importZip(file, signal, function (done, total) { el('progress').value = done / total * 100; text('progressText', '正在解压图片 ' + done + ' / ' + total); });
        if (signal.cancelled) throw new Error('已取消');
        revokeItems(groups.zip); groups.zip = imported.items; names.zip = file.name.replace(/\.zip$/i, ''); el('outputName').value = names.zip;
        zipLabel = file.name + ' · 已按数字名称排列 ' + imported.items.length + ' 张图片' + (imported.ignored ? ' · 忽略 ' + imported.ignored + ' 个非图片文件' : '');
      } catch (error) { message(error.message); }
      finally { setBusy(false); }
      return;
    }
    if (mode === 'word') {
      if (files.length !== 1 || !/\.docx?$/i.test(files[0].name)) { message('请选择一个 .doc 或 .docx 文档'); return; }
      if (!files[0].size || files[0].size > core.maxFile) { message('Word 文件必须大于 0 且不超过 50 MB'); return; }
      groups.word = [{ name: files[0].name, file: files[0] }]; invalidate(); render(); return;
    }
    var errors = [], list = groups.images, sum = total(list), added = 0;
    Array.prototype.forEach.call(files, function (file) {
      if (!core.imagePattern.test(file.name)) { errors.push(file.name + '：图片格式不支持'); return; }
      if (!file.size || file.size > core.maxFile) { errors.push(file.name + '：文件为空或超过 50 MB'); return; }
      if (list.length >= core.maxCount || sum + file.size > core.maxTotal) { errors.push('本批次最多 200 张、总计 200 MB'); return; }
      if (list.some(function (item) { return item.name === file.name && item.file.size === file.size && item.file.lastModified === file.lastModified; })) { errors.push(file.name + '：已在列表中'); return; }
      list.push({ name: file.name, file: file }); sum += file.size; added++;
    });
    if (added) invalidate(); render(); if (errors.length) message(errors.slice(0, 3).join('；') + (errors.length > 3 ? '…' : ''));
  }
  function setMode(value) {
    if (busy) return; names[mode] = el('outputName').value; mode = value; invalidate();
    ['zip','images','word'].forEach(function (name) { el(name + 'Tab').className = 'tab' + (name === mode ? ' active' : ''); el(name + 'Tab').setAttribute('aria-pressed', String(name === mode)); });
    var zip = mode === 'zip', word = mode === 'word';
    text('listTitleText', word ? '确认待转换文档' : '确认 PDF 页面顺序');
    text('stepTitle', zip ? '导入一个 ZIP 压缩包' : word ? '选择一个 Word 文档' : '添加需要合并的图片');
    text('stepHint', zip ? '图片以 1、2、3… 命名，解压后按数字大小自动排序。' : word ? '支持 DOC、DOCX，生成一份便于阅读和分享的 PDF。' : '可多次添加图片，按照你希望的阅读顺序排列。');
    text('dropTitle', zip ? '将 ZIP 压缩包拖放到这里' : word ? '将 Word 文档拖放到这里' : '将多张图片拖放到这里');
    text('dropHint', zip ? '支持文件夹内的图片，自动忽略非图片文件' : word ? '一次选择一个文档，尽量保留文字、表格和图片版式' : '支持 JPG、PNG、WebP、BMP、GIF，可一次选择多张');
    text('chooseBtn', zip ? '选择 ZIP 压缩包' : word ? '选择 Word 文档' : '选择图片');
    text('limits', zip ? 'ZIP 最大 200 MB · 最多 200 张图片 · 支持普通 ZIP' : word ? 'DOC / DOCX · 单个文件最大 50 MB' : '单张最大 50 MB / 2000 万像素 · 最多 200 张 / 200 MB');
    text('listHint', word ? '确认文档后点击生成，完成后下载对应 PDF。' : '从上到下依次为第 1、2、3… 页，可拖动、上下移动或直接改页码。');
    text('formatNote', word ? '不支持密码保护文档；复杂排版及缺失字体可能影响转换结果。' : '一张图片一页，保持比例；透明背景转为白色；GIF 仅导出一帧，最长边超过 4000 像素会适当缩小。');
    text('processingBadge', word ? '内网服务器处理' : '图片不离开浏览器');
    text('footerPrivacy', word ? 'Word 上传到你部署的内网服务器处理，不使用外部转换服务' : '图片和 ZIP 仅在当前浏览器处理，不上传到服务器');
    el('fileInput').accept = zip ? '.zip' : word ? '.doc,.docx' : '.jpg,.jpeg,.png,.webp,.bmp,.gif'; el('fileInput').multiple = mode === 'images'; el('fileInput').value = '';
    el('outputName').value = names[mode] || '图片合并'; render(); if (word) checkService();
  }
  function outputName() { var name = el('outputName').value.trim().replace(/\.pdf$/i, '').replace(/[<>:"/\\|?*\u0000-\u001f]/g, '_'); return (name || '图片合并') + '.pdf'; }
  function result(name, href, remote) { text('resultName', name); el('downloadBtn').href = href; el('downloadBtn').download = name; text('resultHint', remote ? '请在 60 分钟内下载，服务器会自动清理过期文件。' : '已在浏览器生成，请及时下载保存。'); show('resultBox', true); el('downloadBtn').focus(); }
  async function convert() {
    if (busy || !groups[mode].length || mode === 'word' && !wordReady) return;
    invalidate(); signal = { cancelled: false }; setBusy(true, '准备生成 PDF…');
    try {
      if (mode === 'word') {
        var data = new FormData(); data.append('kind', 'word'); data.append('files', groups.word[0].file, groups.word[0].name);
        var metadata = await new Promise(function (resolve, reject) { xhr = api('POST', 'convert', data, function (error, value) { xhr = null; if (error) reject(error); else resolve(value); }, true); });
        if (signal.cancelled) throw new Error('已取消');
        result(metadata.name, apiPath('download') + '?id=' + encodeURIComponent(metadata.id), true);
      } else {
        var blob = await core.imagesPDF(groups[mode], { paper: el('paper').value, orientation: el('orientation').value, margin: Number(el('margin').value) }, signal, function (done, count) { el('progress').value = done / count * 100; text('progressText', '正在生成 PDF：' + done + ' / ' + count + ' 页'); });
        if (signal.cancelled) throw new Error('已取消'); resultURL = URL.createObjectURL(blob); result(outputName(), resultURL, false);
      }
    } catch (error) { message(signal.cancelled ? '已取消处理，文件列表已保留。' : error.message, signal.cancelled); }
    finally { setBusy(false); }
  }
  ['zip','images','word'].forEach(function (name) { el(name + 'Tab').onclick = function () { setMode(name); }; });
  el('chooseBtn').onclick = function () { el('fileInput').click(); };
  el('fileInput').onchange = function () { var files = this.files; receiveFiles(files); this.value = ''; };
  el('clearBtn').onclick = function () { revokeItems(groups[mode]); groups[mode] = []; if (mode === 'zip') zipLabel = ''; invalidate(); render(); };
  el('sortBtn').onclick = function () { groups[mode].sort(function (a, b) { return core.naturalCompare(a.name, b.name); }); invalidate(); render(); };
  ['paper','orientation','margin','outputName'].forEach(function (id) { el(id).onchange = function () { invalidate(); update(); }; });
  el('convertBtn').onclick = convert;
  el('retryService').onclick = checkService;
  el('cancelBtn').onclick = function () { signal.cancelled = true; if (xhr) xhr.abort(); text('progressText', '正在取消…'); };
  var drop = el('dropzone');
  ['dragenter','dragover'].forEach(function (event) { drop.addEventListener(event, function (e) { e.preventDefault(); if (!busy && dragIndex < 0) drop.classList.add('over'); }); });
  ['dragleave','drop'].forEach(function (event) { drop.addEventListener(event, function (e) { e.preventDefault(); drop.classList.remove('over'); }); });
  drop.addEventListener('drop', function (e) { if (dragIndex < 0) receiveFiles(e.dataTransfer.files); });
  window.addEventListener('dragover', function (e) { e.preventDefault(); }); window.addEventListener('drop', function (e) { e.preventDefault(); });
  window.addEventListener('beforeunload', function (e) { if (busy) { e.preventDefault(); e.returnValue = '文件正在处理'; } });
  setMode('zip');
})();
