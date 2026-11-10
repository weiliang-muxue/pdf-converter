(function () {
  'use strict';
  function el(id) { return document.getElementById(id); }
  function text(id, value) { el(id).textContent = value; }
  function show(id, value) { el(id).hidden = !value; }
  var kind = 'images', groups = { images: [], word: [] }, busy = false, wordReady = false, connected = false, activeXHR = null, token = '';
  try { token = sessionStorage.getItem('pdfToken') || ''; } catch (ignore) {}
  if (location.hash.indexOf('#token=') === 0) {
    try { token = decodeURIComponent(location.hash.slice(7)); sessionStorage.setItem('pdfToken', token); } catch (ignore2) {}
    if (window.history && history.replaceState) { history.replaceState(null, '', location.pathname); }
  }
  function bytes(size) { return size < 1048576 ? (size / 1024).toFixed(1) + ' KB' : (size / 1048576).toFixed(1) + ' MB'; }
  function tell(message) { text('message', message); show('message', !!message); }
  function totalSize(list) { var size = 0; for (var i = 0; i < list.length; i++) { size += list[i].file.size; } return size; }
  function api(method, path, data, callback) {
    var xhr = new XMLHttpRequest(); xhr.open(method, path, true); xhr.setRequestHeader('X-PDF-Token', token); xhr.timeout = 360000;
    xhr.onload = function () { var result; try { result = JSON.parse(xhr.responseText); } catch (e) { result = { error: '服务返回内容异常，请重试' }; } callback(xhr.status >= 200 && xhr.status < 300 ? null : (result.error || '请求失败'), result, xhr.status); };
    xhr.onerror = function () { callback('连接中断，请确认程序仍在运行'); };
    xhr.ontimeout = function () { callback('请求超时，请减少文件数量后重试'); };
    if (data) { xhr.upload.onprogress = function (event) { if (event.lengthComputable) { el('progress').value = Math.round(event.loaded / event.total * 90); text('progressText', '正在上传文件 ' + Math.round(event.loaded / event.total * 100) + '%'); } }; xhr.upload.onload = function () { el('progress').removeAttribute('value'); text('progressText', '正在生成 PDF，请稍候…'); }; }
    xhr.send(data); return xhr;
  }
  function status() {
    api('GET', '/api/status', null, function (err, data, code) {
      if (err) { connected = false; if (code === 401) { show('loginPanel', true); text('loginError', '口令不正确，请重新输入'); el('tokenInput').focus(); } else { tell(err); } update(); return; }
      connected = true; wordReady = data.wordReady; show('loginPanel', false); show('exitBtn', !data.server);
      text('engineStatus', wordReady ? '图片与 Word 转换已就绪' : '图片转换已就绪 · Word 引擎未安装');
      text('versionText', 'v' + data.version + ' · ' + data.platform);
      if (data.server) { text('locationLabel', '服务器私有处理'); text('privacyText', '图片合并、Word 转换，文件上传到本服务器处理。'); text('footerNote', '原文件在转换后清理，生成结果保留 60 分钟。'); }
      update();
    });
  }
  function update() {
    var list = groups[kind], hasFiles = list.length > 0;
    el('convertBtn').disabled = busy || !connected || !hasFiles || (kind === 'word' && !wordReady);
    el('clearBtn').disabled = busy || !hasFiles; el('chooseBtn').disabled = busy;
    el('imagesTab').disabled = busy; el('wordTab').disabled = busy;
    el('paper').disabled = busy; el('margin').disabled = busy; el('orientation').disabled = busy || el('paper').value === 'original';
    el('exitBtn').disabled = busy;
    text('fileCount', String(list.length)); show('emptyState', !hasFiles); show('imageSettings', kind === 'images');
    show('engineNote', kind === 'word' && !wordReady && connected);
    text('summary', hasFiles ? '已选择 ' + list.length + ' 个文件，共 ' + bytes(totalSize(list)) : (kind === 'images' ? '请选择要转换的图片' : '请选择要转换的 Word 文档'));
    document.body.className = busy ? 'busy' : '';
  }
  function render() {
    var container = el('fileList'); while (container.firstChild) { container.removeChild(container.firstChild); }
    groups[kind].forEach(function (item, index) {
      var row = document.createElement('li'); row.className = 'file-row';
      var number = document.createElement('span'); number.className = 'file-number'; number.textContent = String(index + 1); row.appendChild(number);
      var preview;
      if (item.preview) { preview = document.createElement('img'); preview.src = item.preview; preview.alt = ''; preview.className = 'file-preview'; preview.onerror = function () { var symbol = document.createElement('span'); symbol.className = 'file-symbol'; symbol.textContent = '图片'; if (preview.parentNode) { preview.parentNode.replaceChild(symbol, preview); } }; }
      else { preview = document.createElement('span'); preview.className = 'file-symbol'; preview.textContent = kind === 'word' ? 'DOC' : '图片'; }
      row.appendChild(preview);
      var details = document.createElement('div'); details.className = 'file-details';
      var name = document.createElement('span'); name.className = 'file-name'; name.textContent = item.file.name; name.title = item.file.name;
      var size = document.createElement('span'); size.className = 'file-size'; size.textContent = bytes(item.file.size);
      details.appendChild(name); details.appendChild(size); row.appendChild(details);
      var actions = document.createElement('div'); actions.className = 'row-actions';
      function button(label, title, disabled, action, className) { var btn = document.createElement('button'); btn.type = 'button'; btn.textContent = label; btn.title = title; btn.setAttribute('aria-label', title + '：' + item.file.name); btn.disabled = busy || disabled; btn.className = className || ''; btn.onclick = action; actions.appendChild(btn); }
      button('↑', '上移', index === 0, function () { var list = groups[kind]; list.splice(index, 1); list.splice(index - 1, 0, item); invalidate(); render(); });
      button('↓', '下移', index === groups[kind].length - 1, function () { var list = groups[kind]; list.splice(index, 1); list.splice(index + 1, 0, item); invalidate(); render(); });
      button('移除', '移除', false, function () { if (item.preview) { URL.revokeObjectURL(item.preview); } groups[kind].splice(index, 1); invalidate(); render(); }, 'remove');
      row.appendChild(actions); container.appendChild(row);
    }); update();
  }
  function invalidate() { show('resultBox', false); tell(''); }
  function addFiles(files) {
    if (busy) { return; }
    var list = groups[kind], limit = kind === 'images' ? 200 : 20, errors = [], sum = totalSize(list), added = 0;
    var pattern = kind === 'images' ? /\.(jpe?g|png|bmp|gif|tiff?)$/i : /\.(docx?)$/i;
    for (var i = 0; i < files.length; i++) {
      var file = files[i];
      if (!pattern.test(file.name)) { errors.push(file.name + '：格式不支持'); continue; }
      if (file.size === 0 || file.size > 50 * 1048576) { errors.push(file.name + '：文件为空或超过 50 MB'); continue; }
      if (list.length >= limit || sum + file.size > 200 * 1048576) { errors.push('已达到本批次文件数量或总大小上限'); break; }
      var duplicate = list.some(function (entry) { return entry.file.name === file.name && entry.file.size === file.size && entry.file.lastModified === file.lastModified; });
      if (duplicate) { errors.push(file.name + '：已在列表中'); continue; }
      var preview = kind === 'images' && /\.(jpe?g|png|bmp|gif)$/i.test(file.name) ? URL.createObjectURL(file) : null;
      list.push({ file: file, preview: preview }); sum += file.size; added++;
    }
    if (added) { invalidate(); } render(); if (errors.length) { tell(errors.slice(0, 3).join('；') + (errors.length > 3 ? '…' : '')); }
  }
  function setKind(value) {
    if (busy) { return; } kind = value;
    var imageMode = kind === 'images'; el('imagesTab').className = 'mode' + (imageMode ? ' active' : ''); el('wordTab').className = 'mode' + (!imageMode ? ' active' : '');
    el('imagesTab').setAttribute('aria-pressed', String(imageMode)); el('wordTab').setAttribute('aria-pressed', String(!imageMode));
    text('dropTitle', imageMode ? '将图片拖到这里' : '将 Word 文档拖到这里'); text('chooseBtn', imageMode ? '选择图片' : '选择 Word 文档');
    text('formatHint', imageMode ? '支持 JPG、PNG、BMP、GIF、TIFF，可一次添加多张' : '支持 DOC、DOCX，可批量生成多个 PDF');
    text('limitHint', imageMode ? '每张最多 50 MB · 每批最多 200 张 / 200 MB' : '每个最多 50 MB · 每批最多 20 个 / 200 MB');
    text('orderHint', imageMode ? '按下方顺序生成 PDF，每张图片一页' : '每个 Word 生成一个 PDF，多个文件打包为 ZIP');
    text('settingsNote', imageMode ? '图片保持比例、居中放置；透明背景转为白色。GIF / TIFF 仅取第一帧 / 页。' : '尽量保留原有版式；复杂排版及缺失字体可能影响结果。不支持密码保护的文档。');
    el('fileInput').accept = imageMode ? '.jpg,.jpeg,.png,.bmp,.gif,.tif,.tiff' : '.doc,.docx'; el('fileInput').value = ''; invalidate(); render();
  }
  el('imagesTab').onclick = function () { setKind('images'); }; el('wordTab').onclick = function () { setKind('word'); };
  el('chooseBtn').onclick = function () { el('fileInput').click(); };
  el('fileInput').onchange = function () { addFiles(this.files); this.value = ''; };
  el('clearBtn').onclick = function () { groups[kind].forEach(function (item) { if (item.preview) { URL.revokeObjectURL(item.preview); } }); groups[kind] = []; invalidate(); render(); };
  el('paper').onchange = function () { invalidate(); update(); }; el('orientation').onchange = invalidate; el('margin').onchange = invalidate;
  var drop = el('dropzone');
  ['dragenter', 'dragover'].forEach(function (event) { drop.addEventListener(event, function (e) { e.preventDefault(); if (!busy) { drop.className = 'dropzone drag-over'; } }); });
  ['dragleave', 'drop'].forEach(function (event) { drop.addEventListener(event, function (e) { e.preventDefault(); drop.className = 'dropzone'; }); });
  drop.addEventListener('drop', function (e) { addFiles(e.dataTransfer.files); });
  window.addEventListener('dragover', function (e) { e.preventDefault(); }); window.addEventListener('drop', function (e) { e.preventDefault(); });
  el('convertBtn').onclick = function () {
    if (busy || !groups[kind].length) { return; }
    var form = new FormData(); form.append('kind', kind); form.append('paper', el('paper').value); form.append('orientation', el('orientation').value); form.append('margin', el('margin').value);
    groups[kind].forEach(function (item) { form.append('files', item.file, item.file.name); });
    busy = true; invalidate(); render(); show('progressBox', true); el('progress').value = 0; text('progressText', '正在上传…');
    activeXHR = api('POST', '/api/convert', form, function (err, data) {
      activeXHR = null; busy = false; show('progressBox', false); render();
      if (err) { tell(err); return; }
      text('resultName', data.name); el('downloadLink').href = '/api/download?id=' + encodeURIComponent(data.id) + '&token=' + encodeURIComponent(token);
      text('downloadLink', /\.zip$/i.test(data.name) ? '下载 ZIP 压缩包' : '下载 PDF'); show('resultBox', true); el('downloadLink').focus();
    });
  };
  el('cancelBtn').onclick = function () { if (activeXHR) { activeXHR.abort(); activeXHR = null; } busy = false; show('progressBox', false); tell('已取消转换'); render(); };
  el('exitBtn').onclick = function () {
    if (!window.confirm('确定退出 PDF 工具箱？尚未下载的转换结果将被清理。')) { return; }
    api('POST', '/api/exit', null, function (err) { if (err) { tell(err); return; } connected = false; show('resultBox', false); show('exitBtn', false); text('engineStatus', '程序已退出'); text('footerNote', '现在可以关闭此页面。再次使用时，请双击 PDF 工具箱程序。'); tell('程序已退出'); update(); });
  };
  el('loginBtn').onclick = function () { token = el('tokenInput').value.trim(); try { sessionStorage.setItem('pdfToken', token); } catch (ignore) {} status(); };
  el('tokenInput').onkeydown = function (e) { if (e.keyCode === 13) { el('loginBtn').click(); } };
  window.addEventListener('beforeunload', function (e) { if (busy) { e.preventDefault(); e.returnValue = '正在转换，确定离开？'; } });
  render(); if (!token) { show('loginPanel', true); el('tokenInput').focus(); } else { status(); }
}());
