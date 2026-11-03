// Upload a new App version from the detail page: folders and loose files are
// packed into a tar.gz here (same format as the CLI), .zip is sent as is.
(function () {
  'use strict';
  const root = document.querySelector('[data-upload]');
  if (!root) return;

  const supported =
    typeof CompressionStream === 'function' &&
    typeof DataTransfer === 'function' &&
    typeof XMLHttpRequest === 'function' &&
    typeof TextEncoder === 'function' &&
    typeof Blob === 'function';
  if (!supported) return;

  const ui = root.querySelector('[data-upload-ui]');
  const unsupported = root.querySelector('[data-upload-unsupported]');
  const zone = root.querySelector('[data-dropzone]');
  const dirInput = root.querySelector('[data-dir-input]');
  const fileInput = root.querySelector('[data-file-input]');
  const pickDir = root.querySelector('[data-pick-dir]');
  const pickFile = root.querySelector('[data-pick-file]');
  const messageInput = root.querySelector('[data-upload-message]');
  const promoteInput = root.querySelector('[data-upload-promote]');
  const status = root.querySelector('[data-upload-status]');
  const statusText = root.querySelector('[data-upload-status-text]');
  const progress = root.querySelector('[data-upload-progress]');
  const errorBox = root.querySelector('[data-upload-error]');
  const slug = root.getAttribute('data-slug');
  const maxBytes = Number(root.getAttribute('data-max-bytes'));
  const maxFiles = Number(root.getAttribute('data-max-files'));
  const maxMB = Math.floor(maxBytes / (1 << 20));

  unsupported.hidden = true;
  ui.hidden = false;
  const canPickDir = 'webkitdirectory' in document.createElement('input');
  if (!canPickDir) pickDir.hidden = true;

  let busy = false;

  // ---------- state ----------

  function leaving(event) {
    event.preventDefault();
    event.returnValue = '';
  }
  function setBusy(on) {
    busy = on;
    pickDir.disabled = on;
    pickFile.disabled = on;
    zone.setAttribute('aria-disabled', on ? 'true' : 'false');
    zone.setAttribute('aria-busy', on ? 'true' : 'false');
    if (on) window.addEventListener('beforeunload', leaving);
    else window.removeEventListener('beforeunload', leaving);
  }
  function showStatus(text, percent) {
    errorBox.hidden = true;
    status.hidden = false;
    statusText.textContent = text;
    progress.hidden = percent === undefined;
    if (percent !== undefined) progress.value = percent;
  }
  function showError(text) {
    status.hidden = true;
    errorBox.hidden = false;
    errorBox.textContent = text;
    setBusy(false);
  }

  // ---------- collecting files ----------

  function skipName(name) {
    return name === 'node_modules' || (name.charAt(0) === '.' && name !== '.well-known');
  }
  function skipPath(path) {
    return path.split('/').some(skipName);
  }
  function readAll(reader) {
    return new Promise(function (resolve, reject) {
      const all = [];
      (function next() {
        reader.readEntries(function (entries) {
          if (!entries.length) return resolve(all);
          all.push.apply(all, entries);
          next();
        }, reject);
      })();
    });
  }
  function entryFile(entry) {
    return new Promise(function (resolve, reject) {
      entry.file(resolve, reject);
    });
  }
  // Appends every file under entry to out, with paths relative to prefix.
  async function walk(entry, prefix, out) {
    if (skipName(entry.name)) return;
    if (entry.isFile) {
      out.push({ path: prefix + entry.name, file: await entryFile(entry) });
    } else if (entry.isDirectory) {
      const children = await readAll(entry.createReader());
      for (const child of children) await walk(child, prefix + entry.name + '/', out);
    }
  }
  async function walkChildren(dir, out) {
    const children = await readAll(dir.createReader());
    for (const child of children) await walk(child, '', out);
  }

  const isZip = function (name) {
    return /\.zip$/i.test(name);
  };
  const isHTML = function (name) {
    return /\.html?$/i.test(name);
  };

  // A drop is read synchronously: the items are gone once the handler returns.
  function snapshotDrop(dataTransfer) {
    const out = [];
    for (const item of Array.from(dataTransfer.items || [])) {
      if (item.kind !== 'file') continue;
      const entry = item.webkitGetAsEntry ? item.webkitGetAsEntry() : null;
      out.push({ entry: entry, file: item.getAsFile() });
    }
    if (!out.length) for (const file of Array.from(dataTransfer.files || [])) out.push({ entry: null, file: file });
    return out;
  }

  // Resolves to {files: [{path, file}]} or {zip: File}; throws a message string.
  async function sourceFromDrop(items) {
    if (!items.length) throw '没有可发布的文件';
    if (items.length === 1) {
      const only = items[0];
      if (only.entry && only.entry.isDirectory) {
        const files = [];
        await walkChildren(only.entry, files);
        return { files: files };
      }
      const file = only.file || (only.entry ? await entryFile(only.entry) : null);
      if (!file) throw '没有可发布的文件';
      return sourceFromSingleFile(file);
    }
    const files = [];
    for (const item of items) {
      if (item.entry) await walk(item.entry, '', files);
      else if (item.file && !skipName(item.file.name)) files.push({ path: item.file.name, file: item.file });
    }
    return { files: files };
  }
  function sourceFromSingleFile(file) {
    if (isZip(file.name)) return { zip: file };
    if (isHTML(file.name)) return { files: [{ path: 'index.html', file: file }] };
    throw '不支持的文件类型：只支持文件夹、.zip 和 .html';
  }

  // ---------- checks ----------

  function indexHint(paths) {
    const dirs = paths
      .filter(function (p) {
        return /\/index\.html$/.test(p);
      })
      .map(function (p) {
        return p.slice(0, -'index.html'.length);
      });
    dirs.sort(function (a, b) {
      const da = a.split('/').length,
        db = b.split('/').length;
      return da - db || (a < b ? -1 : a > b ? 1 : 0);
    });
    return dirs.slice(0, 3);
  }
  // Returns the files that will be uploaded, or throws a message string.
  function checkFiles(files) {
    const kept = files.filter(function (f) {
      return !skipPath(f.path);
    });
    if (!kept.length) throw '没有可发布的文件';
    if (
      !kept.some(function (f) {
        return f.path === 'index.html';
      })
    ) {
      const dirs = indexHint(
        kept.map(function (f) {
          return f.path;
        }),
      );
      if (dirs.length) {
        throw (
          '根目录没有 index.html。在 ' +
          dirs.join('、') +
          ' 里找到了 index.html，可以只拖入 ' +
          dirs
            .map(function (d) {
              return d.replace(/\/$/, '');
            })
            .join('、') +
          ' 文件夹。'
        );
      }
      throw '根目录没有 index.html';
    }
    const limitText = '超过上限：最多 ' + maxMB + ' MB、' + maxFiles + ' 个文件';
    if (kept.length > maxFiles) throw limitText;
    const total = kept.reduce(function (sum, f) {
      return sum + f.file.size;
    }, 0);
    if (total > maxBytes) throw limitText;
    return kept;
  }

  // ---------- tar ----------

  const encoder = new TextEncoder();
  function octal(value, width) {
    return value.toString(8).padStart(width - 1, '0') + '\0';
  }
  function putString(block, offset, text) {
    block.set(encoder.encode(text), offset);
  }
  function tarHeader(nameBytes, size, type) {
    const block = new Uint8Array(512);
    block.set(nameBytes.subarray(0, 100), 0);
    putString(block, 100, '0000644\0');
    putString(block, 108, '0000000\0');
    putString(block, 116, '0000000\0');
    putString(block, 124, octal(size, 12));
    putString(block, 136, octal(Math.floor(Date.now() / 1000), 12));
    putString(block, 148, '        ');
    putString(block, 156, type);
    putString(block, 257, 'ustar\0');
    putString(block, 263, '00');
    let sum = 0;
    for (let i = 0; i < 512; i++) sum += block[i];
    putString(block, 148, sum.toString(8).padStart(6, '0') + '\0 ');
    return block;
  }
  // "<len> path=<path>\n", where len counts its own digits.
  function paxRecord(path) {
    const rest = encoder.encode(' path=' + path + '\n');
    let digits = String(rest.length).length;
    let len = rest.length + digits;
    while (String(len).length !== digits) {
      digits = String(len).length;
      len = rest.length + digits;
    }
    return concat(encoder.encode(String(len)), rest);
  }
  function concat(a, b) {
    const out = new Uint8Array(a.length + b.length);
    out.set(a, 0);
    out.set(b, a.length);
    return out;
  }
  function padding(size) {
    return new Uint8Array((512 - (size % 512)) % 512);
  }
  function buildTar(files) {
    const parts = [];
    for (const item of files) {
      const nameBytes = encoder.encode(item.path);
      if (nameBytes.length > 100) {
        const record = paxRecord(item.path);
        parts.push(tarHeader(encoder.encode('PaxHeader'), record.length, 'x'), record, padding(record.length));
      }
      parts.push(tarHeader(nameBytes, item.file.size, '0'), item.file, padding(item.file.size));
    }
    parts.push(new Uint8Array(1024));
    return new Blob(parts);
  }
  async function gzipTar(files) {
    const stream = buildTar(files).stream().pipeThrough(new CompressionStream('gzip'));
    return new Response(stream).blob();
  }

  // ---------- sending ----------

  function failureText(xhr) {
    let detail = null;
    try {
      detail = JSON.parse(xhr.responseText).error;
    } catch (e) {
      detail = null;
    }
    if (detail && detail.message) return detail.hint ? detail.message + '。' + detail.hint : detail.message;
    if (xhr.status === 401) return '登录已过期，请刷新页面重新登录';
    if (xhr.status === 403) return '请求被拒绝，请刷新页面后重试';
    if (xhr.status === 413) return '超过上限：最多 ' + maxMB + ' MB、' + maxFiles + ' 个文件';
    return '上传失败（HTTP ' + xhr.status + '）';
  }
  function send(body, contentType) {
    const query =
      '?promote=' + (promoteInput.checked ? '1' : '0') + '&message=' + encodeURIComponent(messageInput.value);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/apps/' + encodeURIComponent(slug) + '/upload' + query);
    xhr.setRequestHeader('Content-Type', contentType);
    xhr.upload.onprogress = function (event) {
      if (!event.lengthComputable) return;
      const percent = Math.round((event.loaded / event.total) * 100);
      showStatus('上传中 ' + percent + '%', percent);
    };
    xhr.upload.onload = function () {
      showStatus('服务器处理中');
    };
    xhr.onload = function () {
      if (xhr.status === 201) {
        let result = null;
        try {
          result = JSON.parse(xhr.responseText);
        } catch (e) {
          return showError('上传失败：服务器返回了无法识别的结果');
        }
        window.removeEventListener('beforeunload', leaving);
        window.location.assign('/apps/' + encodeURIComponent(result.app) + '?uploaded=' + result.version);
        return;
      }
      showError(failureText(xhr));
    };
    xhr.onerror = function () {
      showError('网络中断，上传失败，可以重试');
    };
    showStatus('上传中 0%', 0);
    xhr.send(body);
  }

  async function run(prepare) {
    if (busy) return;
    setBusy(true);
    try {
      showStatus('正在读取文件');
      const source = await prepare();
      if (source.zip) {
        if (source.zip.size > maxBytes) throw '超过上限：最多 ' + maxMB + ' MB、' + maxFiles + ' 个文件';
        send(source.zip, 'application/zip');
        return;
      }
      const files = checkFiles(source.files);
      showStatus('正在打包');
      send(await gzipTar(files), 'application/gzip');
    } catch (e) {
      showError(typeof e === 'string' ? e : '读取文件失败：' + (e && e.message ? e.message : e));
    }
  }

  // ---------- events ----------

  function hasFiles(event) {
    return event.dataTransfer && Array.from(event.dataTransfer.types || []).indexOf('Files') >= 0;
  }
  zone.addEventListener('dragenter', function (event) {
    if (!hasFiles(event)) return;
    event.preventDefault();
    if (!busy) zone.classList.add('is-dragover');
  });
  zone.addEventListener('dragover', function (event) {
    if (!hasFiles(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = busy ? 'none' : 'copy';
    if (!busy) zone.classList.add('is-dragover');
  });
  zone.addEventListener('dragleave', function (event) {
    if (!zone.contains(event.relatedTarget)) zone.classList.remove('is-dragover');
  });
  zone.addEventListener('drop', function (event) {
    event.preventDefault();
    zone.classList.remove('is-dragover');
    if (busy || !hasFiles(event)) return;
    const items = snapshotDrop(event.dataTransfer);
    run(function () {
      return sourceFromDrop(items);
    });
  });
  // Dropping anywhere else must neither upload nor make the browser open the file.
  document.addEventListener('dragover', function (event) {
    if (hasFiles(event) && !zone.contains(event.target)) event.preventDefault();
  });
  document.addEventListener('drop', function (event) {
    if (hasFiles(event) && !zone.contains(event.target)) event.preventDefault();
  });

  pickDir.addEventListener('click', function () {
    dirInput.click();
  });
  pickFile.addEventListener('click', function () {
    fileInput.click();
  });
  zone.addEventListener('keydown', function (event) {
    if (event.target !== zone || (event.key !== 'Enter' && event.key !== ' ')) return;
    event.preventDefault();
    if (!busy) (canPickDir ? dirInput : fileInput).click();
  });
  dirInput.addEventListener('change', function () {
    const list = Array.from(dirInput.files || []);
    dirInput.value = '';
    run(async function () {
      return {
        files: list.map(function (file) {
          return { path: (file.webkitRelativePath || file.name).split('/').slice(1).join('/'), file: file };
        }),
      };
    });
  });
  fileInput.addEventListener('change', function () {
    const list = Array.from(fileInput.files || []);
    fileInput.value = '';
    run(async function () {
      if (list.length !== 1) throw '没有可发布的文件';
      return sourceFromSingleFile(list[0]);
    });
  });
})();
