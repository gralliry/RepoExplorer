/* Frontend for RepoDownloader (Wails / Go backend).
 *
 * The Go side owns authentication (OAuth device flow or a manual token) and all
 * writes; every write goes through the Git Data API so an operation like
 * "rename a folder with 200 files" lands as ONE commit.
 */
const api = () => window.go.main.App;
const rt = () => window.runtime;

const $ = (id) => document.getElementById(id);
const repoEl = $('repo');
const refEl = $('ref');
const loadBtn = $('load');
const filterEl = $('filter');
const treeEl = $('tree');
const repoMetaEl = $('repo-meta');
const statCountEl = $('stat-count');
const statSizeEl = $('stat-size');
const destEl = $('dest');
const pickBtn = $('pick');
const downloadBtn = $('download');
const barEl = $('bar');
const barTextEl = $('bar-text');
const logEl = $('log');
const toastEl = $('toast');
const authBtn = $('auth-btn');
const authModal = $('auth-modal');
const ctxMenu = $('ctx-menu');

let info = null;                 // RepoTree returned by the backend
let root = null;                 // nested tree built from info.files
let dirPaths = new Set();        // every directory path in the tree
const sizes = new Map();         // file path -> size
const selected = new Set();      // selected file paths
const expanded = new Set();      // expanded directory paths
let dest = '';
let busy = false;
let metaText = '';
let auth = { loggedIn: false, login: '', source: '' };
let deviceFlow = null;

/* ------------------------------------------------------------------ utils */
function errText(err) {
  if (!err) return '未知错误';
  if (typeof err === 'string') return err;
  if (err.message) return err.message;
  return String(err);
}

function fmtSize(n) {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

function basename(p) {
  const i = p.lastIndexOf('/');
  return i >= 0 ? p.slice(i + 1) : p;
}

function dirname(p) {
  const i = p.lastIndexOf('/');
  return i >= 0 ? p.slice(0, i) : '';
}

const joinPath = (dir, name) => (dir ? `${dir}/${name}` : name);

const repoId = () => (info ? `${info.owner}/${info.repo}` : '');

let toastTimer = null;
function toast(msg, isError = false) {
  toastEl.textContent = msg;
  toastEl.className = 'toast show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastEl.className = 'toast'; }, 3600);
}

function appendLog(text) {
  const line = document.createElement('div');
  line.textContent = text;
  line.title = text;
  logEl.appendChild(line);
  while (logEl.childElementCount > 400) logEl.removeChild(logEl.firstChild);
  logEl.scrollTop = logEl.scrollHeight;
}

function copyText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    return navigator.clipboard.writeText(text);
  }
  return new Promise((resolve, reject) => {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    ok ? resolve() : reject(new Error('复制失败'));
  });
}

/* ------------------------------------------------------------ generic UI */
function openModal(id) { $(id).classList.remove('hidden'); }
function closeModal(id) { $(id).classList.add('hidden'); }

// promptModal resolves with the entered string, or null when cancelled. When
// `extra` is given it resolves with {value, extra}.
function promptModal({ title, label, hint = '', value = '', placeholder = '', extra = null, okText = '确定' }) {
  return new Promise((resolve) => {
    $('prompt-title').textContent = title;
    $('prompt-label').textContent = label;
    $('prompt-hint').textContent = hint;
    $('prompt-hint').classList.toggle('hidden', !hint);
    $('prompt-ok').textContent = okText;

    const input = $('prompt-input');
    input.value = value;
    input.placeholder = placeholder;

    const wrap = $('prompt-extra-wrap');
    const extraEl = $('prompt-extra');
    if (extra) {
      wrap.classList.remove('hidden');
      $('prompt-extra-label').textContent = extra.label;
      extraEl.value = extra.value || '';
    } else {
      wrap.classList.add('hidden');
      extraEl.value = '';
    }

    const finish = (result) => {
      closeModal('prompt-modal');
      $('prompt-ok').onclick = null;
      $('prompt-cancel').onclick = null;
      $('prompt-close').onclick = null;
      input.onkeydown = null;
      resolve(result);
    };

    $('prompt-ok').onclick = () => {
      const v = input.value.trim();
      if (!v) return toast('不能为空', true);
      finish(extra ? { value: v, extra: extraEl.value } : v);
    };
    $('prompt-cancel').onclick = () => finish(null);
    $('prompt-close').onclick = () => finish(null);
    input.onkeydown = (e) => { if (e.key === 'Enter') $('prompt-ok').click(); };

    openModal('prompt-modal');
    input.focus();
    input.select();
  });
}

function confirmModal({ title, message, items = [], okText = '确认' }) {
  return new Promise((resolve) => {
    $('confirm-title').textContent = title;
    $('confirm-message').textContent = message;
    $('confirm-ok').textContent = okText;

    const list = $('confirm-list');
    list.innerHTML = '';
    list.classList.toggle('hidden', items.length === 0);
    const MAX = 300;
    for (const item of items.slice(0, MAX)) {
      const div = document.createElement('div');
      if (item.cls) div.className = item.cls;
      div.textContent = item.text;
      div.title = item.text;
      list.appendChild(div);
    }
    if (items.length > MAX) {
      const more = document.createElement('div');
      more.className = 'more';
      more.textContent = `… 还有 ${items.length - MAX} 项`;
      list.appendChild(more);
    }

    const finish = (ok) => {
      closeModal('confirm-modal');
      $('confirm-ok').onclick = null;
      $('confirm-cancel').onclick = null;
      $('confirm-close').onclick = null;
      resolve(ok);
    };
    $('confirm-ok').onclick = () => finish(true);
    $('confirm-cancel').onclick = () => finish(false);
    $('confirm-close').onclick = () => finish(false);

    openModal('confirm-modal');
  });
}

function editorModal({ title, content, hint = '' }) {
  return new Promise((resolve) => {
    $('editor-title').textContent = title;
    $('editor-hint').textContent = hint;
    const area = $('editor-text');
    area.value = content;

    const finish = (value) => {
      closeModal('editor-modal');
      $('editor-save').onclick = null;
      $('editor-cancel').onclick = null;
      $('editor-close').onclick = null;
      resolve(value);
    };
    $('editor-save').onclick = () => finish(area.value);
    $('editor-cancel').onclick = () => finish(null);
    $('editor-close').onclick = () => finish(null);

    openModal('editor-modal');
    area.focus();
  });
}

/* --------------------------------------------------------- context menu */
function hideCtxMenu() { ctxMenu.classList.add('hidden'); }

function showCtxMenu(x, y, items) {
  ctxMenu.innerHTML = '';
  for (const item of items) {
    if (item === '-') {
      const sep = document.createElement('div');
      sep.className = 'ctx-sep';
      ctxMenu.appendChild(sep);
      continue;
    }
    const el = document.createElement('div');
    el.className = 'ctx-item' + (item.danger ? ' danger' : '');
    el.textContent = item.label;
    el.onclick = () => { hideCtxMenu(); item.run(); };
    ctxMenu.appendChild(el);
  }
  ctxMenu.classList.remove('hidden');

  const rect = ctxMenu.getBoundingClientRect();
  ctxMenu.style.left = `${Math.max(8, Math.min(x, window.innerWidth - rect.width - 8))}px`;
  ctxMenu.style.top = `${Math.max(8, Math.min(y, window.innerHeight - rect.height - 8))}px`;
}

function openContextMenu(event, node) {
  const items = [];

  if (!node) {
    items.push(
      { label: '新建文件…', run: () => promptNewFile('') },
      { label: '新建文件夹…', run: () => promptNewFolder('') },
      { label: '上传文件到根目录…', run: () => uploadInto('', 'files') },
      { label: '上传文件夹到根目录…', run: () => uploadInto('', 'folder') },
    );
  } else if (node.dir) {
    items.push(
      { label: '在此新建文件…', run: () => promptNewFile(node.path) },
      { label: '在此新建文件夹…', run: () => promptNewFolder(node.path) },
      { label: '上传到此…', run: () => uploadInto(node.path, 'files') },
      { label: '上传文件夹到此…', run: () => uploadInto(node.path, 'folder') },
      '-',
      { label: '重命名…', run: () => promptRename(node) },
      { label: '移动到…', run: () => promptMove(node) },
      { label: '删除', danger: true, run: () => confirmDelete([node]) },
      '-',
      { label: '下载此文件夹', run: () => selectAndDownload(node) },
    );
  } else {
    items.push(
      { label: '编辑', run: () => openEditor(node) },
      { label: '下载', run: () => selectAndDownload(node) },
      '-',
      { label: '重命名…', run: () => promptRename(node) },
      { label: '移动到…', run: () => promptMove(node) },
      { label: '删除', danger: true, run: () => confirmDelete([node]) },
    );
  }

  showCtxMenu(event.clientX, event.clientY, items);
}

/* ------------------------------------------------------------------- auth */
function renderAuthButton() {
  authBtn.classList.toggle('on', auth.loggedIn);
  if (auth.loggedIn && auth.source === 'oauth') {
    authBtn.textContent = `● @${auth.login || '已登录'}`;
  } else if (auth.loggedIn) {
    authBtn.textContent = '● Token 已设置';
  } else {
    authBtn.textContent = '登录 GitHub';
  }
}

async function refreshAuth() {
  try {
    auth = await api().GetAuth();
  } catch (err) {
    auth = { loggedIn: false, login: '', source: '' };
  }
  renderAuthButton();
}

function showAuthView(name) {
  for (const view of ['user', 'code', 'setup']) {
    $('auth-view-' + view).classList.toggle('hidden', view !== name);
  }
}

function openAuthModal() {
  authModal.classList.remove('hidden');
  if (auth.loggedIn) {
    $('auth-user-name').textContent =
      auth.source === 'oauth' ? `@${auth.login || '(未知用户)'}` : '手动 Token';
    $('auth-user-source').textContent =
      auth.source === 'oauth'
        ? '已通过 GitHub OAuth 登录，凭据已保存到本地'
        : '正在使用手动填写的 Personal Access Token';
    showAuthView('user');
  } else {
    showAuthView('setup');
  }
}

function closeAuthModal() { authModal.classList.add('hidden'); }

function openExternal(url) {
  if (!url) return;
  api().OpenExternal(url).catch((err) => toast(errText(err), true));
}

function requireLogin() {
  if (auth.loggedIn) return true;
  toast('这个操作需要先登录 GitHub', true);
  openAuthModal();
  return false;
}

/* ------------------------------------------------------------- tree model */
const mkNode = (name, path, dir) => ({ name, path, dir, children: new Map(), size: 0, count: 0 });

function buildTree(files) {
  const rootNode = mkNode('', '', true);
  for (const file of files) {
    const parts = file.path.split('/');
    let node = rootNode;
    let acc = '';
    parts.forEach((part, idx) => {
      acc = acc ? `${acc}/${part}` : part;
      const isLeaf = idx === parts.length - 1;
      let child = node.children.get(part);
      if (!child) {
        child = mkNode(part, acc, !isLeaf);
        node.children.set(part, child);
      }
      node = child;
    });
    node.size = file.size;
    sizes.set(file.path, file.size);
  }
  compute(rootNode);
  return rootNode;
}

function compute(node) {
  if (!node.dir) { node.count = 1; return { count: 1, size: node.size }; }
  let count = 0;
  let size = 0;
  for (const child of node.children.values()) {
    const r = compute(child);
    count += r.count;
    size += r.size;
  }
  node.count = count;
  node.size = size;
  node.sorted = [...node.children.values()].sort((a, b) =>
    a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1);
  return { count, size };
}

function collectFiles(node, out) {
  if (!node.dir) { out.push(node.path); return; }
  for (const child of node.children.values()) collectFiles(child, out);
}

function collectDirPaths(node, out) {
  for (const child of node.children.values()) {
    if (child.dir) { out.push(child.path); collectDirPaths(child, out); }
  }
  return out;
}

function expandToFiles(nodes) {
  const set = new Set();
  for (const node of nodes) {
    if (node.dir) {
      const out = [];
      collectFiles(node, out);
      out.forEach((f) => set.add(f));
    } else {
      set.add(node.path);
    }
  }
  return [...set];
}

/* -------------------------------------------------------------- rendering */
function render() {
  if (!root) {
    treeEl.innerHTML = '<div class="empty">输入仓库后点击“加载目录”<br />右键文件或文件夹可以进行增删改</div>';
    return;
  }
  const q = filterEl.value.trim().toLowerCase();
  treeEl.innerHTML = '';
  if (q) { renderFiltered(q); return; }
  if (metaText) repoMetaEl.textContent = metaText;
  renderChildren(root, treeEl, 0);
}

function renderChildren(node, container, depth) {
  for (const child of node.sorted || []) {
    container.appendChild(buildRow(child, depth));
    if (child.dir && expanded.has(child.path)) {
      const box = document.createElement('div');
      container.appendChild(box);
      renderChildren(child, box, depth + 1);
    }
  }
}

function buildRow(node, depth) {
  const row = document.createElement('div');
  row.className = 'row ' + (node.dir ? 'dir' : 'file');
  row.style.paddingLeft = `${10 + depth * 15}px`;
  row.__node = node;

  const cb = document.createElement('input');
  cb.type = 'checkbox';
  cb.className = 'cb';

  const twisty = document.createElement('span');
  twisty.className = 'twisty';
  twisty.textContent = node.dir ? (expanded.has(node.path) ? '▾' : '▸') : '';
  if (node.dir) twisty.onclick = (e) => { e.stopPropagation(); toggleExpand(node); };

  const icon = document.createElement('span');
  icon.className = 'icon';
  icon.textContent = node.dir ? (expanded.has(node.path) ? '📂' : '📁') : '📄';

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = node.name;
  name.title = node.path;

  const size = document.createElement('span');
  size.className = 'size';
  size.textContent = node.dir ? `${node.count} 项 · ${fmtSize(node.size)}` : fmtSize(node.size);

  if (node.dir) {
    const files = [];
    collectFiles(node, files);
    const picked = files.reduce((n, f) => n + (selected.has(f) ? 1 : 0), 0);
    cb.checked = files.length > 0 && picked === files.length;
    cb.indeterminate = picked > 0 && picked < files.length;
    cb.onchange = () => { toggleDir(node, cb.checked); refresh(); };
    row.onclick = (e) => {
      if (e.target === cb || e.target === twisty) return;
      toggleExpand(node);
    };
  } else {
    cb.checked = selected.has(node.path);
    cb.onchange = () => {
      if (cb.checked) selected.add(node.path);
      else selected.delete(node.path);
      refresh();
    };
    row.ondblclick = () => openEditor(node);
  }

  row.append(cb, twisty, icon, name, size);
  return row;
}

function renderFiltered(q) {
  const matches = info.files.filter((f) => f.path.toLowerCase().includes(q));
  if (!matches.length) {
    treeEl.innerHTML = '<div class="empty">没有匹配的文件</div>';
    return;
  }
  const frag = document.createDocumentFragment();
  for (const f of matches.slice(0, 3000)) {
    const row = document.createElement('div');
    row.className = 'row file';
    row.style.paddingLeft = '10px';
    row.__node = { name: basename(f.path), path: f.path, dir: false };

    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.className = 'cb';
    cb.checked = selected.has(f.path);
    cb.onchange = () => {
      if (cb.checked) selected.add(f.path);
      else selected.delete(f.path);
      updateStats();
      updateFilterCount(matches.length);
    };

    const name = document.createElement('span');
    name.className = 'name';
    name.textContent = f.path;
    name.title = f.path;

    const size = document.createElement('span');
    size.className = 'size';
    size.textContent = fmtSize(f.size);

    row.append(cb, name, size);
    frag.appendChild(row);
  }
  treeEl.appendChild(frag);
  updateFilterCount(matches.length);
}

function updateFilterCount(n) {
  if (filterEl.value.trim()) repoMetaEl.textContent = `筛选出 ${n} 个文件`;
}

function toggleExpand(node) {
  if (expanded.has(node.path)) expanded.delete(node.path);
  else expanded.add(node.path);
  refresh();
}

function toggleDir(node, on) {
  const files = [];
  collectFiles(node, files);
  for (const f of files) { if (on) selected.add(f); else selected.delete(f); }
}

function refresh() {
  const scroll = treeEl.scrollTop;
  render();
  treeEl.scrollTop = scroll;
  updateStats();
}

function updateStats() {
  let size = 0;
  for (const p of selected) size += sizes.get(p) || 0;
  statCountEl.textContent = String(selected.size);
  statSizeEl.textContent = fmtSize(size);
}

function updateMeta() {
  const totalSize = info.files.reduce((a, b) => a + b.size, 0);
  metaText =
    `${info.owner}/${info.repo} @ ${info.git_ref} · ${info.files.length} 个文件 · ${fmtSize(totalSize)}` +
    (info.truncated ? ' · ⚠ 目录过大，结果被 GitHub 截断' : '');
  repoMetaEl.textContent = metaText;
}

/* --------------------------------------------------------------- loading */
async function load() {
  const repo = repoEl.value.trim();
  if (!repo) return toast('请输入仓库地址', true);
  if (busy) return;
  setBusy(true, '正在获取目录…');
  try {
    const tree = await api().FetchRepoTree(repo, refEl.value || '');
    info = tree;

    const branches = tree.branches && tree.branches.length ? tree.branches : [tree.default_branch];
    refEl.innerHTML = '';
    for (const b of branches) {
      const opt = document.createElement('option');
      opt.value = b;
      opt.textContent = b === tree.default_branch ? `${b}（默认）` : b;
      refEl.appendChild(opt);
    }
    refEl.value = tree.git_ref;

    sizes.clear();
    selected.clear();
    expanded.clear();
    root = buildTree(tree.files);
    dirPaths = new Set(collectDirPaths(root, []));
    for (const child of root.children.values()) if (child.dir) expanded.add(child.path);

    updateMeta();
    refresh();
    toast(`已加载 ${tree.files.length} 个文件`);
  } catch (err) {
    toast(errText(err), true);
  } finally {
    setBusy(false);
  }
}

// Re-fetch the tree, keeping the expanded folders and the current selection.
async function refreshTree() {
  if (!info) return;
  const keepExpanded = [...expanded];
  const keepSelected = [...selected];

  const tree = await api().FetchRepoTree(repoId(), info.git_ref);
  info = tree;

  sizes.clear();
  selected.clear();
  expanded.clear();
  root = buildTree(tree.files);
  dirPaths = new Set(collectDirPaths(root, []));

  for (const p of keepExpanded) if (dirPaths.has(p)) expanded.add(p);
  for (const p of keepSelected) if (sizes.has(p)) selected.add(p);

  updateMeta();
  refresh();
}

// runMutation wraps a write: it guards on auth, shows progress, refreshes the
// tree afterwards and reports the commit message.
async function runMutation(label, fn) {
  if (!info) return toast('请先加载仓库', true);
  if (!requireLogin()) return;

  setBusy(true, label);
  try {
    const result = await fn();
    await refreshTree();
    toast(`已提交：${result.message}`);
  } catch (err) {
    toast(errText(err), true);
  } finally {
    setBusy(false);
  }
}

/* -------------------------------------------------------------- downloads */
async function pickFolder() {
  try {
    const picked = await api().PickFolder();
    if (picked) {
      dest = picked;
      destEl.value = picked;
    }
  } catch (err) {
    toast(errText(err), true);
  }
}

function selectAndDownload(node) {
  const files = expandToFiles([node]);
  selected.clear();
  for (const f of files) selected.add(f);
  refresh();
  toast(`已勾选 ${files.length} 个文件，点右侧「下载选中」开始下载`);
}

async function download() {
  if (!info) return toast('请先加载仓库', true);
  if (!selected.size) return toast('请先勾选要下载的文件', true);
  if (!dest) return toast('请先选择下载目录', true);
  if (busy) return;

  const paths = [...selected];
  logEl.innerHTML = '';
  setProgress(0, paths.length);
  setBusy(true, '正在下载…');

  const off = rt().EventsOn('download-progress', (p) => {
    setProgress(p.done, p.total);
    appendLog(`${p.done}/${p.total}  ${p.current}${p.failed ? '  ✗' : ''}`);
  });

  try {
    const res = await api().DownloadFiles(repoId(), info.git_ref, dest, paths);
    toast(`下载完成：成功 ${res.downloaded}，失败 ${res.failed}`, res.failed > 0);
    if (res.errors && res.errors.length) {
      appendLog('—— 失败明细 ——');
      res.errors.slice(0, 100).forEach(appendLog);
    }
  } catch (err) {
    toast(errText(err), true);
  } finally {
    if (typeof off === 'function') off();
    setBusy(false);
  }
}

/* -------------------------------------------------------------- mutations */
function collectMoves(node, newPath) {
  if (!node.dir) return [{ from: node.path, to: newPath }];
  const files = [];
  collectFiles(node, files);
  const prefix = `${node.path}/`;
  return files.map((f) => ({ from: f, to: `${newPath}/${f.slice(prefix.length)}` }));
}

async function confirmAndMove(moves, title) {
  const ok = await confirmModal({
    title,
    message: `将在当前分支上产生 1 个提交，涉及 ${moves.length} 个文件：`,
    items: moves.map((m) => ({ text: `${m.from}  →  ${m.to}` })),
    okText: '确认移动',
  });
  if (!ok) return;
  await runMutation('正在提交…', () => api().MovePaths(repoId(), info.git_ref, moves, ''));
}

async function confirmDeleteFiles(files) {
  if (!files.length) return toast('没有可删除的文件', true);
  const ok = await confirmModal({
    title: '删除',
    message: `将从当前分支删除 ${files.length} 个文件（之后可以在 git 历史里找回）：`,
    items: files.map((f) => ({ text: f, cls: 'del' })),
    okText: '确认删除',
  });
  if (!ok) return;
  await runMutation('正在删除…', () => api().DeletePaths(repoId(), info.git_ref, files, ''));
}

const confirmDelete = (nodes) => confirmDeleteFiles(expandToFiles(nodes));

async function promptNewFile(dir) {
  const answer = await promptModal({
    title: '新建文件',
    label: '文件路径',
    hint: dir ? `将创建在 ${dir}/` : '将创建在仓库根目录',
    value: dir ? `${dir}/` : '',
    extra: { label: '文件内容（可留空）', value: '' },
    okText: '创建',
  });
  if (!answer) return;

  const target = answer.value.replace(/^\/+/, '');
  if (!target) return toast('路径不能为空', true);
  await runMutation('正在创建…', () => api().SaveFile(repoId(), info.git_ref, target, answer.extra, ''));
}

async function promptNewFolder(dir) {
  const folder = await promptModal({
    title: '新建文件夹',
    label: '文件夹路径',
    hint: 'Git 无法保存空文件夹，所以需要同时创建里面的第一个文件。',
    value: dir ? `${dir}/` : '',
    okText: '下一步',
  });
  if (!folder) return;
  const folderPath = folder.replace(/^\/+|\/+$/g, '');
  if (!folderPath) return toast('路径不能为空', true);

  const file = await promptModal({
    title: '第一个文件',
    label: '文件名',
    hint: `将创建在 ${folderPath}/`,
    extra: { label: '文件内容（可留空）', value: '' },
    okText: '创建',
  });
  if (!file) return;
  if (file.value.includes('/')) return toast('文件名不能包含 /', true);

  const target = `${folderPath}/${file.value}`;
  await runMutation('正在创建…', () => api().SaveFile(repoId(), info.git_ref, target, file.extra, ''));
}

async function promptRename(node) {
  const name = await promptModal({
    title: '重命名',
    label: '新名称',
    hint: node.path,
    value: node.name,
    okText: '下一步',
  });
  if (!name || name === node.name) return;
  if (name.includes('/')) return toast('名称不能包含 /，要换目录请用「移动到」', true);

  const newPath = joinPath(dirname(node.path), name);
  await confirmAndMove(collectMoves(node, newPath), `重命名「${node.name}」`);
}

async function promptMove(node) {
  const answer = await promptModal({
    title: '移动到',
    label: '目标目录',
    hint: '留空表示仓库根目录',
    value: dirname(node.path),
    okText: '下一步',
  });
  if (answer === null) return;

  const dir = answer.replace(/^\/+|\/+$/g, '');
  const newPath = joinPath(dir, node.name);
  if (newPath === node.path) return toast('目标路径没有变化');
  await confirmAndMove(collectMoves(node, newPath), `移动「${node.path}」`);
}

async function uploadInto(dir, kind) {
  if (!requireLogin()) return;

  let picked = [];
  try {
    if (kind === 'folder') {
      const folder = await api().PickUploadFolder();
      picked = folder ? [folder] : [];
    } else {
      picked = await api().PickUploadFiles() || [];
    }
  } catch (err) {
    return toast(errText(err), true);
  }
  if (!picked.length) return;

  let items;
  try {
    items = await api().PlanUpload(dir, picked);
  } catch (err) {
    return toast(errText(err), true);
  }

  const ok = await confirmModal({
    title: '上传',
    message: `将向 ${dir || '仓库根目录'} 上传 ${items.length} 个文件（1 个提交）：`,
    items: items.map((it) => ({ text: `${it.path}   ${fmtSize(it.size)}`, cls: 'add' })),
    okText: '确认上传',
  });
  if (!ok) return;

  await runMutation('正在上传…', () => api().UploadFiles(repoId(), info.git_ref, dir, picked, ''));
}

async function openEditor(node) {
  if (!info) return;
  if (!requireLogin()) return;

  // Read first, and release the busy state before opening the editor — otherwise
  // the global "busy" rule would block the editor's own buttons.
  let file;
  setBusy(true, '正在读取…');
  try {
    file = await api().ReadFile(repoId(), info.git_ref, node.path);
  } catch (err) {
    toast(errText(err), true);
    return;
  } finally {
    setBusy(false);
  }

  if (file.tooLarge) {
    return toast(`文件太大（${fmtSize(file.size)}），请用右键「下载」`, true);
  }
  if (file.binary) {
    return toast('这是二进制文件，编辑器打不开，请用右键「下载」', true);
  }

  const content = await editorModal({
    title: node.path,
    content: file.content,
    hint: '保存会在当前分支上创建一个提交。',
  });
  if (content === null || content === file.content) return;

  await runMutation('正在提交…', () => api().SaveFile(repoId(), info.git_ref, node.path, content, ''));
}

/* ------------------------------------------------------------------ chrome */
function setProgress(done, total) {
  const pct = total ? Math.round((done / total) * 100) : 0;
  barEl.style.width = `${pct}%`;
  barTextEl.textContent = `${done} / ${total} (${pct}%)`;
}

function setBusy(value, label) {
  busy = value;
  document.body.classList.toggle('busy', value);
  loadBtn.textContent = value ? (label || '处理中…') : '加载目录';
  downloadBtn.textContent = value ? '处理中…' : '下载选中';
}

/* ------------------------------------------------------------------ wiring */
loadBtn.onclick = load;
pickBtn.onclick = pickFolder;
downloadBtn.onclick = download;

repoEl.onkeydown = (e) => { if (e.key === 'Enter') load(); };
refEl.onchange = () => { if (info) load(); };

let filterTimer = null;
filterEl.oninput = () => {
  clearTimeout(filterTimer);
  filterTimer = setTimeout(render, 150);
};

$('refresh').onclick = () => {
  if (!info) return toast('请先加载仓库', true);
  setBusy(true, '正在刷新…');
  refreshTree().then(() => toast('已刷新')).catch((err) => toast(errText(err), true)).finally(() => setBusy(false));
};

$('select-all').onclick = () => {
  if (!info) return;
  for (const f of info.files) selected.add(f.path);
  refresh();
};

$('clear-all').onclick = () => {
  selected.clear();
  refresh();
};

$('delete-selected').onclick = () => {
  if (!selected.size) return toast('请先勾选要删除的文件', true);
  confirmDeleteFiles([...selected]);
};

$('move-selected').onclick = async () => {
  const files = [...selected];
  if (!files.length) return toast('请先勾选要移动的文件', true);
  const answer = await promptModal({
    title: '移动选中',
    label: '目标目录',
    hint: `将移动 ${files.length} 个文件，文件名保持不变。留空表示仓库根目录。`,
    value: '',
    okText: '下一步',
  });
  if (answer === null) return;

  const dir = answer.replace(/^\/+|\/+$/g, '');
  const moves = files.map((f) => ({ from: f, to: joinPath(dir, basename(f)) }));
  const changed = moves.filter((m) => m.from !== m.to);
  if (!changed.length) return toast('目标路径没有变化');
  await confirmAndMove(changed, '移动选中');
};

// Right click anywhere in the tree.
treeEl.addEventListener('contextmenu', (e) => {
  e.preventDefault();
  if (!root) return;
  const row = e.target.closest ? e.target.closest('.row') : null;
  openContextMenu(e, row && row.__node ? row.__node : null);
});

document.addEventListener('mousedown', (e) => {
  if (!ctxMenu.contains(e.target)) hideCtxMenu();
});
document.addEventListener('keydown', (e) => {
  if (e.key !== 'Escape') return;
  hideCtxMenu();
  closeModal('prompt-modal');
  closeModal('confirm-modal');
});
window.addEventListener('blur', hideCtxMenu);

/* ------------------------------------------------------------- auth wiring */
authBtn.onclick = openAuthModal;
$('auth-close').onclick = closeAuthModal;
authModal.addEventListener('click', (e) => { if (e.target === authModal) closeAuthModal(); });

$('auth-start').onclick = async () => {
  try {
    await api().StartGitHubLogin();
    deviceFlow = null;
    $('auth-user-code').textContent = '········';
    $('auth-code-status').textContent = '正在向 GitHub 申请验证码…';
    showAuthView('code');
  } catch (err) {
    toast(errText(err), true);
  }
};

$('auth-open').onclick = () => {
  if (deviceFlow && deviceFlow.verificationUri) openExternal(deviceFlow.verificationUri);
};

$('auth-copy').onclick = () => {
  const code = $('auth-user-code').textContent.trim();
  copyText(code).then(() => toast('验证码已复制')).catch(() => toast('复制失败，请手动选中复制', true));
};

$('auth-cancel').onclick = async () => {
  try { await api().CancelGitHubLogin(); } catch (err) { /* 忽略 */ }
  deviceFlow = null;
  showAuthView(auth.loggedIn ? 'user' : 'setup');
};

$('auth-save-token').onclick = async () => {
  const token = $('auth-token').value.trim();
  if (!token) return toast('请先填写 Token', true);
  try {
    await api().SaveManualToken(token);
    $('auth-token').value = '';
    await refreshAuth();
    closeAuthModal();
    toast('Token 已保存');
  } catch (err) {
    toast(errText(err), true);
  }
};

$('auth-logout').onclick = async () => {
  try {
    await api().ClearAuth();
    await refreshAuth();
    openAuthModal();
    toast('已清除本地凭据');
  } catch (err) {
    toast(errText(err), true);
  }
};

function registerLoginEvents(attempt = 0) {
  if (!window.runtime || typeof window.runtime.EventsOn !== 'function') {
    if (attempt < 40) setTimeout(() => registerLoginEvents(attempt + 1), 50);
    return;
  }

  rt().EventsOn('github-login-code', (code) => {
    deviceFlow = code;
    $('auth-user-code').textContent = code.userCode;
    $('auth-code-status').textContent = `等待你在浏览器中授权…（验证码 ${code.expiresIn} 秒内有效）`;
    authModal.classList.remove('hidden');
    showAuthView('code');
  });

  rt().EventsOn('github-login-done', async (result) => {
    deviceFlow = null;
    await refreshAuth();
    closeAuthModal();
    toast(`已登录 GitHub：@${result.login || '(未知用户)'}`);
  });

  rt().EventsOn('github-login-error', (message) => {
    deviceFlow = null;
    toast(String(message), true);
    showAuthView(auth.loggedIn ? 'user' : 'setup');
  });
}

/* ------------------------------------------------------------------ start */
updateStats();
registerLoginEvents();
refreshAuth();
