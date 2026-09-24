/* Frontend for RepoExplorer (Wails / Go backend).
 *
 * The UI deliberately mirrors Windows Explorer: an address bar, a content pane
 * showing the current folder, click / ctrl+click / shift+click selection, F2
 * inline rename, drag & drop to move, and a status bar.
 *
 * All writes go through the Go side, which turns each operation into ONE commit
 * via the Git Data API.
 */
const api = () => window.go.main.App;
const rt = () => window.runtime;

const $ = (id) => document.getElementById(id);
const repoDisplay = $('repo-display');
const repoNameEl = $('repo-name');
const refEl = $('ref');
const loadBtn = $('load');
const filterEl = $('filter');
const listEl = $('list');
const crumbEl = $('breadcrumb');
const toastEl = $('toast');
const ctxMenu = $('ctx-menu');

/* -------------------------------------------------------------- app state */
let info = null;                  // RepoTree from Go
let currentRepo = '';             // "owner/repo" currently open
let currentPath = '';             // "" = repository root
let history = [];
let histIndex = -1;
let selection = new Set();        // selected paths in the current view
let anchorPath = null;            // for shift-click ranges
let currentEntries = [];          // entries currently rendered
let entryMap = new Map();         // path -> entry
let sizes = new Map();            // file path -> size
let filePaths = new Set();        // every file in the repo
let dirStats = new Map();         // dir path -> {count, size}
let viewMode = 'details';         // 'details' | 'icons'
let sortKey = 'name';
let sortAsc = true;
let dest = '';
let busy = false;
let dragging = null;              // paths being dragged
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

function basename(p) { const i = p.lastIndexOf('/'); return i >= 0 ? p.slice(i + 1) : p; }
function dirname(p) { const i = p.lastIndexOf('/'); return i >= 0 ? p.slice(0, i) : ''; }
const joinPath = (dir, name) => (dir ? `${dir}/${name}` : name);
const repoId = () => (info ? `${info.owner}/${info.repo}` : '');

function fileType(name) {
  const i = name.lastIndexOf('.');
  if (i <= 0) return '文件';
  return `${name.slice(i + 1).toUpperCase()} 文件`;
}

let toastTimer = null;
function toast(msg, isError = false) {
  toastEl.textContent = msg;
  toastEl.className = 'toast show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastEl.className = 'toast'; }, 3600);
}

function copyText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(text);
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
const openModal = (id) => $(id).classList.remove('hidden');
const closeModal = (id) => $(id).classList.add('hidden');

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
    input.onkeydown = (e) => {
      e.stopPropagation();
      if (e.key === 'Enter') $('prompt-ok').click();
    };

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
    if (item.header) {
      const head = document.createElement('div');
      head.className = 'ctx-header';
      head.textContent = item.label;
      ctxMenu.appendChild(head);
      continue;
    }
    const el = document.createElement('div');
    el.className = 'ctx-item'
      + (item.danger ? ' danger' : '')
      + (item.disabled ? ' disabled' : '');
    el.textContent = item.label;
    if (!item.disabled) el.onclick = () => { hideCtxMenu(); item.run(); };
    ctxMenu.appendChild(el);
  }
  ctxMenu.classList.remove('hidden');
  const rect = ctxMenu.getBoundingClientRect();
  ctxMenu.style.left = `${Math.max(8, Math.min(x, window.innerWidth - rect.width - 8))}px`;
  ctxMenu.style.top = `${Math.max(8, Math.min(y, window.innerHeight - rect.height - 8))}px`;
}

function openContextMenu(event, entries) {
  const items = [];
  const single = entries.length === 1 ? entries[0] : null;
  const ro = !canWrite();   // read-only repository: download only

  if (ro) {
    items.push({ label: '只读仓库，只能下载', header: true }, '-');
  }

  if (!entries.length) {
    items.push(
      { label: '新建文件…', run: () => promptNewFile(currentPath), disabled: ro },
      { label: '新建文件夹…', run: () => promptNewFolder(currentPath), disabled: ro },
      '-',
      { label: '上传文件…', run: () => uploadInto(currentPath, 'files'), disabled: ro },
      { label: '上传文件夹…', run: () => uploadInto(currentPath, 'folder'), disabled: ro },
      '-',
      { label: '刷新', run: doRefresh },
    );
  } else {
    if (single && !single.dir) items.push({ label: '编辑', run: () => openEditor(single), disabled: ro });
    if (single && single.dir) {
      items.push(
        { label: '打开', run: () => navigate(single.path) },
        '-',
        { label: '在此新建文件…', run: () => promptNewFile(single.path), disabled: ro },
        { label: '在此新建文件夹…', run: () => promptNewFolder(single.path), disabled: ro },
        { label: '上传到此…', run: () => uploadInto(single.path, 'files'), disabled: ro },
        '-',
      );
    }
    items.push({
      label: entries.length > 1 ? `下载（${entries.length} 项）` : '下载',
      run: downloadSelection,
    });
    items.push('-');
    if (single) items.push({ label: '重命名…', run: () => startInlineRename(single), disabled: ro });
    items.push({ label: '移动到…', run: () => promptMove(entries), disabled: ro });
    items.push('-');
    items.push({ label: '删除', danger: true, run: () => confirmDeleteEntries(entries), disabled: ro });
  }
  showCtxMenu(event.clientX, event.clientY, items);
}

/* --------------------------------------------------------- auth */
// Sign-in lives inside the "open repository" dialog: while logged out its top
// section shows the login UI instead of the repository list.
async function refreshAuth() {
  try { auth = await api().GetAuth(); }
  catch (err) { auth = { loggedIn: false, login: '', source: '' }; }
  renderAuthArea();
}

function renderAuthArea() {
  const loggedIn = !!auth.loggedIn;
  $('auth-box').classList.toggle('hidden', loggedIn);
  $('myrepo-box').classList.toggle('hidden', !loggedIn);
  $('auth-logout').classList.toggle('hidden', !loggedIn);

  if (!loggedIn) showAuthView('setup');
  renderMyRepoState();
}

function renderMyRepoState() {
  const state = $('myrepo-state');
  if (!auth.loggedIn) {
    state.textContent = '未登录';
    return;
  }
  const who = auth.source === 'oauth' ? `@${auth.login || '(未知用户)'}` : '手动 Token';
  let text = `已登录 ${who}`;
  if (myReposLoading) text += ' · 加载中…';
  else if (myReposError) text += ' · 加载失败';
  else if (myReposLoaded) text += ` · ${myRepos.length} 个`;
  state.textContent = text;
}

function showAuthView(name) {
  for (const view of ['setup', 'code']) {
    $('auth-view-' + view).classList.toggle('hidden', view !== name);
  }
}

function openExternal(url) {
  if (!url) return;
  api().OpenExternal(url).catch((err) => toast(errText(err), true));
}

function requireLogin() {
  if (auth.loggedIn) return true;
  toast('这个操作需要先登录 GitHub', true);
  openOpenModal();
  return false;
}

// A repository the credentials cannot push to is download-only.
const canWrite = () => !!(info && info.canWrite);

function requireWrite() {
  if (!info) {
    toast('请先打开仓库', true);
    return false;
  }
  if (!canWrite()) {
    toast('这个仓库你没有写权限，只能下载', true);
    return false;
  }
  return requireLogin();
}

/* ------------------------------------------------------------ repo model */
function buildIndex() {
  sizes = new Map();
  filePaths = new Set();
  dirStats = new Map();

  for (const f of info.files) {
    sizes.set(f.path, f.size);
    filePaths.add(f.path);

    const parts = f.path.split('/');
    let acc = '';
    for (let i = 0; i < parts.length - 1; i++) {
      acc = acc ? `${acc}/${parts[i]}` : parts[i];
      let s = dirStats.get(acc);
      if (!s) { s = { count: 0, size: 0 }; dirStats.set(acc, s); }
      s.count += 1;
      s.size += f.size;
    }
  }
}

const dirExists = (p) => p === '' || dirStats.has(p);

function entryFromPath(p) {
  if (filePaths.has(p)) {
    return { name: basename(p), path: p, dir: false, size: sizes.get(p) || 0, type: fileType(basename(p)) };
  }
  const st = dirStats.get(p) || { count: 0, size: 0 };
  return { name: basename(p), path: p, dir: true, size: st.size, count: st.count, type: '文件夹' };
}

function entriesOf(dir) {
  const prefix = dir ? `${dir}/` : '';
  const map = new Map();

  for (const f of info.files) {
    if (!f.path.startsWith(prefix)) continue;
    const rest = f.path.slice(prefix.length);
    if (!rest) continue;

    const slash = rest.indexOf('/');
    if (slash === -1) {
      map.set(rest, { name: rest, path: f.path, dir: false, size: f.size, type: fileType(rest) });
    } else {
      const name = rest.slice(0, slash);
      if (!map.has(name)) map.set(name, entryFromPath(prefix + name));
    }
  }

  const list = [...map.values()];
  sortEntries(list);
  return list;
}

function sortEntries(list) {
  const dir = sortAsc ? 1 : -1;
  list.sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;   // folders always first
    let r;
    if (sortKey === 'size') r = a.size - b.size;
    else if (sortKey === 'type') r = a.type.localeCompare(b.type, 'zh') || a.name.localeCompare(b.name, 'zh');
    else r = a.name.localeCompare(b.name, 'zh');
    return r * dir;
  });
}

/* ------------------------------------------------------------ rendering */
function render() {
  renderBreadcrumb();
  listEl.innerHTML = '';
  currentEntries = [];
  entryMap = new Map();

  if (!info) {
    listEl.innerHTML = '<div class="empty">输入仓库后点「打开仓库」<br />加载完成后，双击文件夹进入，双击文件编辑</div>';
    updateStatus();
    return;
  }

  const q = filterEl.value.trim().toLowerCase();
  if (q) {
    const matches = info.files
      .filter((f) => f.path.toLowerCase().includes(q))
      .sort((a, b) => a.path.localeCompare(b.path))
      .slice(0, 2000);
    currentEntries = matches.map((f) => ({
      name: f.path, path: f.path, dir: false, size: f.size, type: fileType(basename(f.path)),
    }));
  } else {
    currentEntries = entriesOf(currentPath);
  }
  entryMap = new Map(currentEntries.map((e) => [e.path, e]));

  if (!currentEntries.length) {
    listEl.innerHTML = q ? '<div class="empty">没有匹配的文件</div>' : '<div class="empty">这个文件夹是空的</div>';
    updateStatus();
    return;
  }

  if (viewMode === 'icons') renderGrid(currentEntries, listEl);
  else renderDetails(currentEntries, listEl);

  updateStatus();
}

function renderDetails(entries, container) {
  const head = document.createElement('div');
  head.className = 'list-head';
  for (const [key, label] of [['name', '名称'], ['size', '大小'], ['type', '类型']]) {
    const cell = document.createElement('div');
    cell.textContent = label + (sortKey === key ? (sortAsc ? ' ▲' : ' ▼') : '');
    if (sortKey === key) cell.classList.add('sorted');
    cell.onclick = () => {
      if (sortKey === key) sortAsc = !sortAsc;
      else { sortKey = key; sortAsc = true; }
      render();
    };
    head.appendChild(cell);
  }
  container.appendChild(head);

  const body = document.createElement('div');
  for (const entry of entries) body.appendChild(buildRow(entry));
  container.appendChild(body);
}

function renderGrid(entries, container) {
  const grid = document.createElement('div');
  grid.className = 'grid';
  for (const entry of entries) grid.appendChild(buildTile(entry));
  container.appendChild(grid);
}

function buildRow(entry) {
  const row = document.createElement('div');
  row.className = 'item' + (selection.has(entry.path) ? ' selected' : '');
  row.__entry = entry;
  row.draggable = true;

  const name = document.createElement('div');
  name.className = 'col-name';
  const icon = document.createElement('span');
  icon.className = 'icon';
  icon.textContent = entry.dir ? '📁' : '📄';
  const label = document.createElement('span');
  label.className = 'label';
  label.textContent = entry.name;
  label.title = entry.path;
  name.append(icon, label);

  const size = document.createElement('div');
  size.className = 'col-size';
  size.textContent = entry.dir ? `${entry.count} 项` : fmtSize(entry.size);

  const type = document.createElement('div');
  type.className = 'col-type';
  type.textContent = entry.type;

  row.append(name, size, type);
  attachItemEvents(row, entry);
  return row;
}

function buildTile(entry) {
  const tile = document.createElement('div');
  tile.className = 'tile' + (selection.has(entry.path) ? ' selected' : '');
  tile.__entry = entry;
  tile.draggable = true;

  const icon = document.createElement('div');
  icon.className = 'tile-icon';
  icon.textContent = entry.dir ? '📁' : '📄';

  const label = document.createElement('div');
  label.className = 'label tile-label';
  label.textContent = entry.name;
  label.title = entry.path;

  tile.append(icon, label);
  attachItemEvents(tile, entry);
  return tile;
}

function attachItemEvents(el, entry) {
  el.onmousedown = (e) => {
    if (e.button !== 0 || el.classList.contains('editing')) return;
    selectWithModifiers(entry, e);
  };
  el.ondblclick = () => openEntry(entry);
  el.oncontextmenu = (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (!selection.has(entry.path)) {
      selection = new Set([entry.path]);
      anchorPath = entry.path;
      paintSelection();
      updateStatus();
    }
    openContextMenu(e, selectionEntries());
  };

  el.ondragstart = (e) => {
    if (!selection.has(entry.path)) {
      selection = new Set([entry.path]);
      anchorPath = entry.path;
      paintSelection();
      updateStatus();
    }
    dragging = [...selection];
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', dragging.join('\n'));
  };
  el.ondragend = () => { dragging = null; clearDropTargets(); };
}

function selectionEntries() {
  return [...selection].map((p) => entryMap.get(p) || entryFromPath(p));
}

function paintSelection() {
  for (const el of document.querySelectorAll('.item, .tile')) {
    if (!el.__entry) continue;
    el.classList.toggle('selected', selection.has(el.__entry.path));
  }
}

function selectWithModifiers(entry, e) {
  if (e.ctrlKey || e.metaKey) {
    if (selection.has(entry.path)) selection.delete(entry.path);
    else { selection.add(entry.path); anchorPath = entry.path; }
  } else if (e.shiftKey && anchorPath) {
    const list = currentEntries.map((x) => x.path);
    const a = list.indexOf(anchorPath);
    const b = list.indexOf(entry.path);
    if (a >= 0 && b >= 0) {
      selection.clear();
      const [lo, hi] = a <= b ? [a, b] : [b, a];
      for (let i = lo; i <= hi; i++) selection.add(list[i]);
    } else {
      selection = new Set([entry.path]);
    }
  } else {
    selection = new Set([entry.path]);
    anchorPath = entry.path;
  }
  paintSelection();
  updateStatus();
}

function openEntry(entry) {
  if (entry.dir) navigate(entry.path);
  else openEditor(entry);
}

/* -------------------------------------------------------------- navigation */
function navigate(path, push = true) {
  currentPath = path || '';
  if (!dirExists(currentPath)) currentPath = '';

  if (push && history[histIndex] !== currentPath) {
    history = history.slice(0, histIndex + 1);
    history.push(currentPath);
    histIndex = history.length - 1;
  }

  selection = new Set();
  anchorPath = null;
  filterEl.value = '';
  render();
  updateNavButtons();
}

function goBack() { if (histIndex > 0) { histIndex--; navigate(history[histIndex], false); } }
function goForward() { if (histIndex < history.length - 1) { histIndex++; navigate(history[histIndex], false); } }
function goUp() { if (currentPath) navigate(dirname(currentPath)); }

function updateNavButtons() {
  $('nav-back').disabled = histIndex <= 0;
  $('nav-forward').disabled = histIndex >= history.length - 1;
  $('nav-up').disabled = !currentPath;
}

function renderBreadcrumb() {
  crumbEl.innerHTML = '';
  const rootLabel = info ? `${info.owner}/${info.repo}` : '（未打开仓库）';
  const parts = currentPath ? currentPath.split('/') : [];

  const makeCrumb = (label, path, isCurrent) => {
    const span = document.createElement('span');
    span.className = 'crumb' + (isCurrent ? ' current' : '');
    span.textContent = label;
    span.onclick = (e) => { e.stopPropagation(); navigate(path); };
    return span;
  };

  crumbEl.appendChild(makeCrumb(rootLabel, '', parts.length === 0));
  let acc = '';
  for (const part of parts) {
    acc = acc ? `${acc}/${part}` : part;
    const sep = document.createElement('span');
    sep.className = 'crumb-sep';
    sep.textContent = '›';
    crumbEl.appendChild(sep);
    crumbEl.appendChild(makeCrumb(part, acc, acc === currentPath));
  }
}

// Clicking the empty part of the address bar lets you type a path, like Explorer.
function startPathEdit() {
  if (!info) return;
  crumbEl.innerHTML = '';
  const input = document.createElement('input');
  input.value = currentPath ? `${info.owner}/${info.repo}/${currentPath}` : `${info.owner}/${info.repo}`;
  crumbEl.appendChild(input);
  input.focus();
  input.select();

  let done = false;
  const finish = (commit) => {
    if (done) return;
    done = true;
    if (!commit) { renderBreadcrumb(); return; }
    const value = input.value.trim().replace(/^\/+|\/+$/g, '');
    const parts = value.split('/').filter(Boolean);
    // Accept "owner/repo/a/b", "repo/a/b" or "a/b".
    const rest = parts.length >= 2 ? parts.slice(2).join('/') : '';
    navigate(rest);
  };
  input.onclick = (e) => e.stopPropagation();
  input.onkeydown = (e) => {
    e.stopPropagation();
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  };
  input.onblur = () => finish(false);
}

/* --------------------------------------------------------------- status */
function updateWriteControls() {
  const ro = !canWrite();
  for (const id of ['new-menu', 'upload-menu']) {
    const btn = $(id);
    btn.disabled = ro;
    btn.title = ro ? '只读仓库，只能下载' : (id === 'new-menu' ? '新建' : '上传');
  }
}

function updateStatus() {
  const itemsEl = $('status-items');
  updateWriteControls();

  if (!info) {
    itemsEl.textContent = '尚未加载仓库';
    $('status-dest').textContent = '';
    return;
  }

  let text = `${currentEntries.length} 个对象`;
  if (filterEl.value.trim()) text += '（搜索结果）';
  if (!info.canWrite) text += '　　只读（只能下载）';

  let count = 0;
  let size = 0;
  for (const p of selection) {
    count++;
    size += sizes.get(p) || 0;
  }
  if (count) text += `　　已选 ${count} 个${count && size ? `（${fmtSize(size)}）` : ''}`;
  itemsEl.textContent = text;

  $('status-dest').textContent = dest ? `下载到 ${dest}` : '下载目录：未设置（点击设置）';
}

function setProgress(done, total) {
  const el = $('status-progress');
  if (!total) { el.innerHTML = ''; return; }
  const pct = Math.round((done / total) * 100);
  el.innerHTML = `<span class="bar"><i style="width:${pct}%"></i></span>${done}/${total}`;
}

function setBusy(value, label) {
  busy = value;
  document.body.classList.toggle('busy', value);
  loadBtn.textContent = value ? (label || '处理中…') : '打开仓库';
}

/* --------------------------------------------------------------- loading */
async function load() {
  const repo = currentRepo.trim();
  if (!repo) return toast('请先选择或输入一个仓库', true);
  if (busy) return;

  setBusy(true, '正在打开…');
  try {
    const tree = await api().FetchRepoTree(repo, refEl.value || '');
    info = tree;
    currentRepo = `${tree.owner}/${tree.repo}`;
    repoNameEl.textContent = currentRepo;

    const branches = tree.branches && tree.branches.length ? tree.branches : [tree.default_branch];
    refEl.innerHTML = '';
    for (const b of branches) {
      const opt = document.createElement('option');
      opt.value = b;
      opt.textContent = b === tree.default_branch ? `${b}（默认）` : b;
      refEl.appendChild(opt);
    }
    refEl.value = tree.git_ref;

    buildIndex();
    history = [''];
    histIndex = 0;
    navigate('', false);
    toast(`已打开 ${tree.owner}/${tree.repo} · ${tree.files.length} 个文件`);
  } catch (err) {
    toast(errText(err), true);
  } finally {
    setBusy(false);
  }
}

async function refreshTree() {
  if (!info) return;
  const tree = await api().FetchRepoTree(repoId(), info.git_ref);
  info = tree;
  buildIndex();

  // If the folder we're in disappeared (deleted or renamed), walk up.
  while (currentPath && !dirExists(currentPath)) currentPath = dirname(currentPath);

  selection = new Set();
  anchorPath = null;
  history = history.map((p) => (dirExists(p) ? p : ''));
  render();
  updateNavButtons();
}

async function doRefresh() {
  if (!info) return toast('请先打开仓库', true);
  setBusy(true, '正在刷新…');
  try { await refreshTree(); toast('已刷新'); }
  catch (err) { toast(errText(err), true); }
  finally { setBusy(false); }
}

async function runMutation(label, fn) {
  if (!info) return toast('请先打开仓库', true);
  if (!requireWrite()) return;

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

/* -------------------------------------------------------------- download */
async function chooseDest() {
  try {
    const picked = await api().PickFolder();
    if (picked) { dest = picked; updateStatus(); }
  } catch (err) {
    toast(errText(err), true);
  }
}

async function downloadSelection() {
  if (!info) return toast('请先打开仓库', true);
  if (!selection.size) return toast('请先选中要下载的文件', true);
  if (!dest) {
    await chooseDest();
    if (!dest) return;
  }
  await download();
}

async function download() {
  if (busy) return;
  const paths = [...selection];
  setProgress(0, paths.length);
  setBusy(true, '正在下载…');

  const off = rt().EventsOn('download-progress', (p) => setProgress(p.done, p.total));

  try {
    const res = await api().DownloadFiles(repoId(), info.git_ref, dest, paths);
    setProgress(0, 0);
    toast(`下载完成：成功 ${res.downloaded}，失败 ${res.failed}`, res.failed > 0);
    if (res.errors && res.errors.length) {
      console.warn('下载失败明细', res.errors);
      toast(`有 ${res.errors.length} 个文件失败，详见控制台`, true);
    }
  } catch (err) {
    toast(errText(err), true);
  } finally {
    if (typeof off === 'function') off();
    setBusy(false);
  }
}

/* ------------------------------------------------------------- mutations */
function collectMovesForPath(p, newPath) {
  if (filePaths.has(p)) return [{ from: p, to: newPath }];
  const prefix = `${p}/`;
  return info.files
    .filter((f) => f.path.startsWith(prefix))
    .map((f) => ({ from: f.path, to: `${newPath}/${f.path.slice(prefix.length)}` }));
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

async function confirmDeletePaths(paths) {
  if (!paths.length) return toast('没有可删除的文件', true);
  const ok = await confirmModal({
    title: '删除',
    message: `将从当前分支删除 ${paths.length} 个文件（之后可以在 git 历史里找回）：`,
    items: paths.map((p) => ({ text: p, cls: 'del' })),
    okText: '确认删除',
  });
  if (!ok) return;
  await runMutation('正在删除…', () => api().DeletePaths(repoId(), info.git_ref, paths, ''));
}

function confirmDeleteEntries(entries) {
  const files = new Set();
  for (const e of entries) collectMovesForPath(e.path, e.path).forEach((m) => files.add(m.from));
  return confirmDeletePaths([...files]);
}

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

async function promptMove(entries) {
  const answer = await promptModal({
    title: '移动到',
    label: '目标目录',
    hint: entries.length > 1
      ? `将移动 ${entries.length} 项，名称保持不变。留空表示仓库根目录。`
      : '留空表示仓库根目录',
    value: currentPath,
    okText: '下一步',
  });
  if (answer === null) return;

  const dir = answer.replace(/^\/+|\/+$/g, '');
  const moves = [];
  for (const entry of entries) {
    const newPath = joinPath(dir, entry.name);
    if (newPath === entry.path) continue;
    moves.push(...collectMovesForPath(entry.path, newPath));
  }
  if (!moves.length) return toast('目标路径没有变化');
  await confirmAndMove(moves, entries.length === 1 ? `移动「${entries[0].name}」` : `移动 ${entries.length} 项`);
}

// Explorer-style inline rename (F2 / context menu).
function startInlineRename(entry) {
  if (!requireWrite()) return;
  const el = [...document.querySelectorAll('.item, .tile')]
    .find((node) => node.__entry && node.__entry.path === entry.path);
  if (!el) return;
  const label = el.querySelector('.label');
  if (!label) return;

  el.classList.add('editing');
  const input = document.createElement('input');
  input.value = entry.name;
  label.textContent = '';
  label.appendChild(input);

  const dot = entry.dir ? -1 : entry.name.lastIndexOf('.');
  input.focus();
  input.setSelectionRange(0, dot > 0 ? dot : entry.name.length);

  let done = false;
  const finish = (commit) => {
    if (done) return;
    done = true;
    el.classList.remove('editing');
    if (!commit) return render();

    const name = input.value.trim();
    if (!name || name === entry.name) return render();
    if (name.includes('/')) {
      toast('名称不能包含 /，要换目录请用「移动到」', true);
      return render();
    }
    const newPath = joinPath(dirname(entry.path), name);
    confirmAndMove(collectMovesForPath(entry.path, newPath), `重命名「${entry.name}」`);
  };

  input.onmousedown = (e) => e.stopPropagation();
  input.onclick = (e) => e.stopPropagation();
  input.onkeydown = (e) => {
    e.stopPropagation();
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  };
  input.onblur = () => finish(true);
}

async function uploadInto(dir, kind) {
  if (!requireWrite()) return;

  let picked = [];
  try {
    if (kind === 'folder') {
      const folder = await api().PickUploadFolder();
      picked = folder ? [folder] : [];
    } else {
      picked = (await api().PickUploadFiles()) || [];
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

async function openEditor(entry) {
  if (!info) return;
  if (!requireWrite()) return;

  let file;
  setBusy(true, '正在读取…');
  try {
    file = await api().ReadFile(repoId(), info.git_ref, entry.path);
  } catch (err) {
    toast(errText(err), true);
    return;
  } finally {
    setBusy(false);
  }

  if (file.tooLarge) return toast(`文件太大（${fmtSize(file.size)}），请用右键「下载」`, true);
  if (file.binary) return toast('这是二进制文件，编辑器打不开，请用右键「下载」', true);

  const content = await editorModal({
    title: entry.path,
    content: file.content,
    hint: '保存会在当前分支上创建一个提交。',
  });
  if (content === null || content === file.content) return;

  await runMutation('正在提交…', () => api().SaveFile(repoId(), info.git_ref, entry.path, content, ''));
}

/* ---------------------------------------------------------- drag & drop */
function clearDropTargets() {
  document.querySelectorAll('.drop-target').forEach((el) => el.classList.remove('drop-target'));
}

function setupDragAndDrop() {
  listEl.addEventListener('dragover', (e) => {
    if (!dragging) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    const el = e.target.closest('.item, .tile');
    const folder = el && el.__entry && el.__entry.dir ? el : null;
    clearDropTargets();
    if (folder) folder.classList.add('drop-target');
  });

  listEl.addEventListener('dragleave', (e) => {
    if (!e.relatedTarget || !listEl.contains(e.relatedTarget)) clearDropTargets();
  });

  listEl.addEventListener('drop', async (e) => {
    if (!dragging) return;
    e.preventDefault();

    const el = e.target.closest('.item, .tile');
    const folder = el && el.__entry && el.__entry.dir ? el.__entry : null;
    const paths = dragging;
    dragging = null;
    clearDropTargets();

    const targetDir = folder ? folder.path : currentPath;
    const moves = [];
    for (const p of paths) {
      if (dirname(p) === targetDir) continue;
      if (folder && (p === folder.path || folder.path.startsWith(`${p}/`))) continue;
      moves.push(...collectMovesForPath(p, joinPath(targetDir, basename(p))));
    }
    if (!moves.length) return toast('没有需要移动的文件');
    await confirmAndMove(moves, folder ? `移动到「${folder.name}」` : '移动到当前文件夹');
  });
}

/* ------------------------------------------------------------ repo picker */
let myRepos = [];
let myReposLoaded = false;
let myReposLoading = false;
let myReposError = '';
const collapsedGroups = new Set();   // which affiliation groups the user folded away

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));
}

function openOpenModal() {
  openModal('open-modal');
  $('manual-repo').value = currentRepo || '';
  $('myrepo-search').value = '';
  renderAuthArea();
  renderMyRepos();
  if (auth.loggedIn) refreshMyRepos();
}

function openRepo(fullName) {
  const name = (fullName || '').trim();
  if (!name) return toast('请输入仓库地址', true);
  currentRepo = name;
  repoNameEl.textContent = name;
  closeModal('open-modal');
  load();
}

async function refreshMyRepos(force = false) {
  if (!auth.loggedIn) {
    myRepos = [];
    myReposLoaded = false;
    myReposError = '';
    renderMyRepoState();
    renderMyRepos();
    return;
  }
  if (myReposLoading) return;
  if (myReposLoaded && !force) return;

  myReposLoading = true;
  renderMyRepoState();
  try {
    myRepos = (await api().ListMyRepos()) || [];
    myReposError = '';
    myReposLoaded = true;
  } catch (err) {
    myRepos = [];
    myReposError = errText(err);
    myReposLoaded = false;
  } finally {
    myReposLoading = false;
    renderMyRepoState();
    renderMyRepos();
  }
}

const REPO_GROUPS = [
  ['owner', '我的仓库'],
  ['organization', '组织仓库'],
  ['collaborator', '协作仓库'],
];

function buildRepoRow(repo) {
  const row = document.createElement('div');
  row.className = 'repo-row';

  const name = document.createElement('div');
  name.className = 'repo-name';
  name.textContent = repo.fullName;
  if (repo.private) {
    const badge = document.createElement('span');
    badge.className = 'badge';
    badge.textContent = '私有';
    name.appendChild(badge);
  }

  const desc = document.createElement('div');
  desc.className = 'repo-desc';
  desc.textContent = repo.description || '';

  row.append(name, desc);
  row.title = repo.description || repo.fullName;
  row.onclick = () => openRepo(repo.fullName);
  return row;
}

function renderMyRepos() {
  const list = $('myrepo-list');
  list.innerHTML = '';

  if (!auth.loggedIn) {
    list.innerHTML = '<div class="repo-empty">登录 GitHub 后，这里会列出你的仓库</div>';
    return;
  }
  if (myReposError) {
    list.innerHTML = `<div class="repo-empty">${escapeHtml(myReposError)}</div>`;
    return;
  }

  const q = $('myrepo-search').value.trim().toLowerCase();
  const items = myRepos.filter((r) =>
    !q || r.fullName.toLowerCase().includes(q) || (r.description || '').toLowerCase().includes(q));

  if (!items.length) {
    list.innerHTML = `<div class="repo-empty">${myRepos.length ? '没有匹配的仓库' : '还没有仓库'}</div>`;
    return;
  }

  // Group by where the repository comes from, so a repo you merely collaborate
  // on is never mistaken for one of your own. Each group can be folded away.
  // Empty groups are still shown (with a 0) so the categories are always visible.
  for (const [key, label] of REPO_GROUPS) {
    const group = items.filter((r) => r.category === key);
    const collapsed = collapsedGroups.has(key);

    const head = document.createElement('div');
    head.className = 'repo-group' + (collapsed ? ' collapsed' : '');
    head.title = collapsed ? '展开' : '收起';

    const arrow = document.createElement('span');
    arrow.className = 'repo-group-arrow';
    arrow.textContent = collapsed ? '▸' : '▾';

    const text = document.createElement('span');
    text.textContent = `${label} · ${group.length}`;

    head.append(arrow, text);
    head.onclick = () => {
      if (collapsedGroups.has(key)) collapsedGroups.delete(key);
      else collapsedGroups.add(key);
      renderMyRepos();
    };
    list.appendChild(head);

    if (!collapsed) {
      for (const repo of group) list.appendChild(buildRepoRow(repo));
    }
  }
}

/* ------------------------------------------------------------------ chrome */
loadBtn.onclick = openOpenModal;
repoDisplay.onclick = openOpenModal;

$('open-close').onclick = () => closeModal('open-modal');
$('open-modal').addEventListener('click', (e) => { if (e.target === $('open-modal')) closeModal('open-modal'); });
$('myrepo-search').oninput = renderMyRepos;
$('manual-open').onclick = () => openRepo($('manual-repo').value);
$('manual-repo').onkeydown = (e) => {
  e.stopPropagation();
  if (e.key === 'Enter') openRepo($('manual-repo').value);
};

filterEl.oninput = () => { render(); };
$('refresh').onclick = doRefresh;
$('download').onclick = downloadSelection;
$('status-dest').onclick = chooseDest;
$('nav-back').onclick = goBack;
$('nav-forward').onclick = goForward;
$('nav-up').onclick = goUp;

$('view-toggle').onclick = () => {
  viewMode = viewMode === 'details' ? 'icons' : 'details';
  $('view-toggle').textContent = viewMode === 'details' ? '详细信息' : '图标';
  render();
};

crumbEl.onmousedown = (e) => {
  if (e.target === crumbEl) startPathEdit();
};

$('new-menu').onclick = (e) => {
  const r = e.currentTarget.getBoundingClientRect();
  showCtxMenu(r.left, r.bottom + 4, [
    { label: '新建文件…', run: () => promptNewFile(currentPath) },
    { label: '新建文件夹…', run: () => promptNewFolder(currentPath) },
  ]);
};

$('upload-menu').onclick = (e) => {
  const r = e.currentTarget.getBoundingClientRect();
  showCtxMenu(r.left, r.bottom + 4, [
    { label: '上传文件…', run: () => uploadInto(currentPath, 'files') },
    { label: '上传文件夹…', run: () => uploadInto(currentPath, 'folder') },
  ]);
};

// Clicking empty space clears the selection (like Explorer).
listEl.addEventListener('mousedown', (e) => {
  if (e.button !== 0) return;
  if (e.target.closest('.item') || e.target.closest('.tile') || e.target.closest('.list-head')) return;
  selection = new Set();
  anchorPath = null;
  paintSelection();
  updateStatus();
});

// Right click on empty space -> actions for the current folder.
listEl.addEventListener('contextmenu', (e) => {
  e.preventDefault();
  const el = e.target.closest('.item, .tile');
  if (el && el.__entry) return;   // handled per item
  if (!info) return;
  selection = new Set();
  paintSelection();
  updateStatus();
  openContextMenu(e, []);
});

document.addEventListener('mousedown', (e) => {
  if (!ctxMenu.contains(e.target)) hideCtxMenu();
});
window.addEventListener('blur', hideCtxMenu);

document.addEventListener('keydown', (e) => {
  const tag = (e.target.tagName || '').toUpperCase();
  const typing = tag === 'INPUT' || tag === 'TEXTAREA' || e.target.isContentEditable;

  if (e.key === 'Escape') {
    hideCtxMenu();
    closeModal('prompt-modal');
    closeModal('confirm-modal');
    if (typing) e.target.blur();
    return;
  }
  if (typing || !info) return;

  if (e.key === 'Backspace') { e.preventDefault(); goUp(); }
  else if (e.key === 'F5') { e.preventDefault(); doRefresh(); }
  else if (e.key === 'F2') { e.preventDefault(); const p = [...selection][0]; if (p) startInlineRename(entryMap.get(p) || entryFromPath(p)); }
  else if (e.key === 'Delete') { e.preventDefault(); if (selection.size) confirmDeletePaths([...selection]); }
  else if (e.key === 'Enter') { e.preventDefault(); const p = [...selection][0]; if (p) openEntry(entryMap.get(p) || entryFromPath(p)); }
  else if (e.ctrlKey && e.key.toLowerCase() === 'a') {
    e.preventDefault();
    selection = new Set(currentEntries.map((x) => x.path));
    paintSelection();
    updateStatus();
  } else if (e.altKey && e.key === 'ArrowLeft') { e.preventDefault(); goBack(); }
  else if (e.altKey && e.key === 'ArrowRight') { e.preventDefault(); goForward(); }
});

refEl.onchange = () => { if (info) load(); };

/* ------------------------------------------------------------- auth wiring */
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
  showAuthView('setup');
};

$('auth-save-token').onclick = async () => {
  const token = $('auth-token').value.trim();
  if (!token) return toast('请先填写 Token', true);
  try {
    await api().SaveManualToken(token);
    $('auth-token').value = '';
    myReposLoaded = false;
    await refreshAuth();
    renderMyRepos();
    await refreshMyRepos();
    toast('Token 已保存');
  } catch (err) {
    toast(errText(err), true);
  }
};

$('auth-logout').onclick = async () => {
  try {
    await api().ClearAuth();
    myRepos = [];
    myReposLoaded = false;
    myReposError = '';
    await refreshAuth();
    renderMyRepos();
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
    openModal('open-modal');
    renderAuthArea();
    showAuthView('code');
  });

  rt().EventsOn('github-login-done', async (result) => {
    deviceFlow = null;
    myReposLoaded = false;      // the picker should reload the list for this account
    await refreshAuth();
    renderMyRepos();
    await refreshMyRepos();
    toast(`已登录 GitHub：@${result.login || '(未知用户)'}`);
  });

  rt().EventsOn('github-login-error', (message) => {
    deviceFlow = null;
    toast(String(message), true);
    if (!auth.loggedIn) showAuthView('setup');
  });
}

/* ------------------------------------------------------------------ start */
setupDragAndDrop();
updateNavButtons();
updateStatus();
registerLoginEvents();
refreshAuth();
