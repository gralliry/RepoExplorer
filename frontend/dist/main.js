/* Frontend for the GitHub folder downloader (Wails / Go backend).
 *
 * The backend methods are exposed by Wails as window.go.main.App.<Method>(...)
 * and the runtime helpers live on window.runtime. Authentication (OAuth device
 * flow or a manual token) is handled entirely in Go and persisted to disk, so
 * the frontend only renders state and triggers actions.
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

let info = null;                 // RepoTree returned by the backend
let root = null;                 // nested tree built from info.files
const sizes = new Map();         // file path -> size
const selected = new Set();      // selected file paths
const expanded = new Set();      // expanded directory paths
let dest = '';
let busy = false;
let metaText = '';
let auth = { loggedIn: false, login: '', source: '' };
let deviceFlow = null;           // last github-login-code payload

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

let toastTimer = null;
function toast(msg, isError = false) {
  toastEl.textContent = msg;
  toastEl.className = 'toast show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { toastEl.className = 'toast'; }, 3200);
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

function closeAuthModal() {
  authModal.classList.add('hidden');
}

function openExternal(url) {
  if (!url) return;
  api().OpenExternal(url).catch((err) => toast(errText(err), true));
}

/* ------------------------------------------------------------- tree model */
const mkNode = (name, path, dir) => ({ name, path, dir, children: new Map(), size: 0, count: 0 });

function buildTree(files) {
  const root = mkNode('', '', true);
  for (const file of files) {
    const parts = file.path.split('/');
    let node = root;
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
  compute(root);
  return root;
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

/* -------------------------------------------------------------- rendering */
function render() {
  if (!root) {
    treeEl.innerHTML = '<div class="empty">输入仓库后点击“加载目录”</div>';
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
  if (filterEl.value.trim()) {
    repoMetaEl.textContent = `筛选出 ${n} 个文件`;
  }
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

/* ---------------------------------------------------------------- actions */
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
    for (const child of root.children.values()) if (child.dir) expanded.add(child.path);

    const totalSize = tree.files.reduce((a, b) => a + b.size, 0);
    metaText =
      `${tree.owner}/${tree.repo} @ ${tree.git_ref} · ${tree.files.length} 个文件 · ${fmtSize(totalSize)}` +
      (tree.truncated ? ' · ⚠ 目录过大，结果被 GitHub 截断' : '');
    repoMetaEl.textContent = metaText;

    refresh();
    toast(`已加载 ${tree.files.length} 个文件`);
  } catch (err) {
    toast(errText(err), true);
  } finally {
    setBusy(false);
  }
}

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
    const res = await api().DownloadFiles(
      `${info.owner}/${info.repo}`,
      info.git_ref,
      dest,
      paths,
    );
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

function setProgress(done, total) {
  const pct = total ? Math.round((done / total) * 100) : 0;
  barEl.style.width = `${pct}%`;
  barTextEl.textContent = `${done} / ${total} (${pct}%)`;
}

function setBusy(value, label) {
  busy = value;
  loadBtn.disabled = value;
  downloadBtn.disabled = value;
  pickBtn.disabled = value;
  loadBtn.textContent = value ? (label || '处理中…') : '加载目录';
  downloadBtn.textContent = value ? '处理中…' : '开始下载';
}

/* ------------------------------------------------------------------ wiring */
loadBtn.onclick = load;
pickBtn.onclick = pickFolder;
downloadBtn.onclick = download;

repoEl.addEventListener('keydown', (e) => { if (e.key === 'Enter') load(); });
refEl.addEventListener('change', () => { if (info) load(); });

let filterTimer = null;
filterEl.addEventListener('input', () => {
  clearTimeout(filterTimer);
  filterTimer = setTimeout(render, 150);
});

$('select-all').onclick = () => {
  if (!info) return;
  for (const f of info.files) selected.add(f.path);
  refresh();
};

$('clear-all').onclick = () => {
  selected.clear();
  refresh();
};

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
