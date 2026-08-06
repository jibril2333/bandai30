// Bandai collection tracker — single-page client. Vanilla JS, hash routes,
// collection list driven by /api/collections.
const $ = id => document.getElementById(id);

// Status vocabulary differs by collection type:
//  - kit:      assemble-it-yourself model → tracks build progress
//  - finished: pre-built toy (Tamashii etc.) → no buildステップ, just sealed/opened
// "ordered" sits between 想要 and 未拆: paid for, not in hand yet. It is
// deliberately NOT counted as 已拥有 anywhere — you can't build what hasn't
// arrived — but it does count as "marked", so it shows up under 我的收藏.
const STATUS_SETS = {
  kit: [
    { key: 'all', label: '全部' }, { key: 'none', label: '未拥有' }, { key: 'wishlist', label: '想要' },
    { key: 'ordered', label: '未到货' },
    { key: 'sealed', label: '未拆' }, { key: 'wip', label: '在做' }, { key: 'done', label: '已完成' },
  ],
  finished: [
    { key: 'all', label: '全部' }, { key: 'none', label: '未拥有' }, { key: 'wishlist', label: '想要' },
    { key: 'ordered', label: '未到货' },
    { key: 'sealed', label: '未拆' }, { key: 'done', label: '已开封' },
  ],
};
const STATUS_LABELS = {
  kit:      { none: '未拥有', wishlist: '想要', ordered: '未到货', sealed: '未拆', wip: '在做', done: '已完成' },
  finished: { none: '未拥有', wishlist: '想要', ordered: '未到货', sealed: '未拆', wip: '在做', done: '已开封' },
};
// In hand. Ordered is excluded on purpose: 已拥有 counts what you can pick up
// off the shelf, and folding pre-orders in would inflate it.
const isOwned = s => s === 'sealed' || s === 'wip' || s === 'done';
const statusSet = type => STATUS_SETS[type] || STATUS_SETS.kit;
const statusLabel = (key, type) => (STATUS_LABELS[type] || STATUS_LABELS.kit)[key] || key;

const state = {
  user: null,
  noAuth: false,
  collections: [],      // [{code,slug,name,family,tagline,color,scraper,...}]
  stats: {},            // { code: {total,none,...} }
  items: [],
  col: null,            // active collection object
  cats: [],
  filter: { search: '', category: 'all', status: 'all' },
  view: localStorage.getItem('view') === 'list' ? 'list' : 'grid',
  editingId: null,
};

const byCode = code => state.collections.find(c => c.code === code);
const bySlug = slug => state.collections.find(c => c.slug === slug);

// ---------- API ----------
const api = {
  async fetch(path, opts = {}) {
    opts.headers = { ...(opts.headers || {}) };
    if (opts.body && !(opts.body instanceof FormData) && typeof opts.body !== 'string') {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(opts.body);
    }
    const r = await fetch(path, { credentials: 'same-origin', ...opts });
    if (r.status === 401) { state.user = null; renderLogin(); throw new Error('未登录'); }
    if (!r.ok) {
      let msg = `HTTP ${r.status}`;
      try { const j = await r.json(); if (j.error) msg = j.error; } catch {}
      throw new Error(msg);
    }
    if (r.status === 204) return null;
    return r.json();
  },
  me: () => api.fetch('/api/auth/me'),
  login: (u, p) => api.fetch('/api/auth/login', { method: 'POST', body: { username: u, password: p } }),
  logout: () => api.fetch('/api/auth/logout', { method: 'POST' }),
  collections: () => api.fetch('/api/collections'),
  stats: () => api.fetch('/api/stats'),
  categories: (code) => api.fetch('/api/categories' + (code ? `?series=${encodeURIComponent(code)}` : '')),
  listItems: (q) => {
    const qs = new URLSearchParams(Object.fromEntries(Object.entries(q || {}).filter(([_, v]) => v && v !== 'all')));
    return api.fetch('/api/items' + (qs.toString() ? `?${qs}` : ''));
  },
  // PUT only: the catalog comes from scraping, so there is no create path.
  saveItem: (it) => api.fetch(`/api/items/${encodeURIComponent(it.id)}`, { method: 'PUT', body: it }),
  deleteItem: (id) => api.fetch(`/api/items/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  upload: (file) => { const fd = new FormData(); fd.append('file', file); return api.fetch('/api/upload', { method: 'POST', body: fd }); },
  // slug omitted = refresh every collection
  scrape: (slug, mode) => api.fetch('/api/scrape' + (slug ? `/${slug}` : '') +
    (mode === 'full' ? '?mode=full' : ''), { method: 'POST' }),
  scrapeStatus: () => api.fetch('/api/scrape/status'),
  item: (id) => api.fetch(`/api/items/${encodeURIComponent(id)}`),
  settings: () => api.fetch('/api/settings'),
  saveSettings: (s) => api.fetch('/api/settings', { method: 'PUT', body: s }),
  backups: () => api.fetch('/api/backups'),
  runBackup: () => api.fetch('/api/backups', { method: 'POST' }),
};

// ---------- routing ----------
function currentRoute() {
  const h = location.hash.replace(/^#\/?/, '').toLowerCase();
  if (!h) return { name: 'landing' };
  if (h === 'mine') return { name: 'mine' };
  if (h === 'settings') return { name: 'settings' };
  const c = bySlug(h);
  return c ? { name: 'collection', code: c.code } : { name: 'landing' };
}
window.addEventListener('hashchange', () => render());

// ---------- bootstrap ----------
async function bootstrap() {
  try {
    const me = await api.me();
    state.user = me.username;
    state.noAuth = !!me.noAuth;
    await render();
  } catch {
    renderLogin();
  }
}

async function render() {
  if (!state.user) { renderLogin(); return; }
  $('topbar').hidden = false;
  $('me').textContent = state.noAuth ? '' : state.user;
  $('logout-btn').hidden = !!state.noAuth;
  [state.collections, state.stats] = await Promise.all([api.collections(), api.stats()]);
  renderNav();
  const r = currentRoute();
  if (r.name === 'landing') {
    await renderLanding();
  } else if (r.name === 'settings') {
    document.documentElement.style.setProperty('--primary', '#e8467a');
    await renderSettings();
  } else if (r.name === 'mine') {
    document.documentElement.style.setProperty('--primary', '#e8467a');
    await renderMine();
  } else {
    await loadCollection(r.code);
  }
}

// group collections into [{family, items:[...]}] preserving sort order
function groupByFamily(cols) {
  const order = [];
  const map = {};
  for (const c of cols) {
    if (!map[c.family]) { map[c.family] = []; order.push(c.family); }
    map[c.family].push(c);
  }
  return order.map(f => ({ family: f, cols: map[f] }));
}

function renderNav() {
  const cur = currentRoute();
  const mineActive = cur.name === 'mine' ? ' active' : '';
  const seriesHTML = state.collections.map(c => {
    const active = cur.name === 'collection' && cur.code === c.code;
    const total = (state.stats[c.code] && state.stats[c.code].total) || 0;
    return `<a href="#/${c.slug}" class="${active ? 'active' : ''}" style="--accent:${c.color}">${c.code} <span style="opacity:.6">${total}</span></a>`;
  }).join('');
  const setActive = cur.name === 'settings' ? ' active' : '';
  $('seriesnav').innerHTML =
    `<a href="#/mine" class="nav-mine${mineActive}" style="--accent:#e8467a">★ 我的</a>` + seriesHTML +
    `<a href="#/settings" class="nav-set${setActive}" style="--accent:#7a7074" title="设置">⚙</a>`;
  // keep the active series visible in the horizontal-scroll nav (mobile)
  const act = $('seriesnav').querySelector('a.active');
  if (act) act.scrollIntoView({ inline: 'center', block: 'nearest' });
}

// ---------- login ----------
function renderLogin() {
  $('topbar').hidden = true;
  $('main').innerHTML = `
    <div class="login">
      <h1>Bandai 收藏</h1>
      <p>登录后才能查看和编辑收藏</p>
      <input id="lu" type="text" placeholder="用户名" autocomplete="username">
      <input id="lp" type="password" placeholder="密码" autocomplete="current-password">
      <button id="lb" class="primary">登录</button>
      <div id="le" class="err"></div>
    </div>`;
  $('lb').onclick = doLogin;
  $('lp').addEventListener('keydown', e => { if (e.key === 'Enter') doLogin(); });
  $('lu').focus();
}
async function doLogin() {
  $('le').textContent = '';
  try {
    const u = $('lu').value.trim(), p = $('lp').value;
    if (!u || !p) { $('le').textContent = '请填写用户名和密码'; return; }
    const r = await api.login(u, p);
    state.user = r.username;
    await render();
  } catch (e) { $('le').textContent = e.message; }
}
$('logout-btn').onclick = async () => { await api.logout(); state.user = null; renderLogin(); };

// ---------- theme ----------
function updateThemeBtn() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark';
  const btn = $('theme-btn');
  if (btn) btn.textContent = dark ? '☀' : '🌙';
}
$('theme-btn').onclick = () => {
  const dark = document.documentElement.getAttribute('data-theme') !== 'dark';
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light');
  localStorage.setItem('theme', dark ? 'dark' : 'light');
  updateThemeBtn();
};
updateThemeBtn();

// ---------- landing (collection dashboard) ----------
async function renderLanding() {
  document.title = 'Bandai 收藏';
  document.documentElement.style.setProperty('--primary', '#e8467a');
  const items = await api.listItems({ marked: 1 });   // everything owned or wanted
  state.items = items;
  state.col = null;

  const owned = items.filter(m => isOwned(m.status));
  const wish = items.filter(m => m.status === 'wishlist');
  const onWay = items.filter(m => m.status === 'ordered');
  const spent = owned.reduce((n, m) => n + parsePrice(m.price), 0);
  let total = 0;
  for (const c of state.collections) total += (state.stats[c.code] || {}).total || 0;

  const today = new Date().toISOString().slice(0, 10);
  // Ordered items belong here too — they are the ones genuinely on the way.
  const upcoming = wish.concat(onWay).filter(m => m.releaseDate && m.releaseDate > today)
    .sort((a, b) => (a.releaseDate || '').localeCompare(b.releaseDate || ''));

  const sections = groupByFamily(state.collections).map(g => {
    const cards = g.cols.map(c => {
      const st = state.stats[c.code] || {};
      const own = (st.sealed || 0) + (st.wip || 0) + (st.done || 0);
      const tot = st.total || 0;
      const pct = tot ? Math.round(100 * own / tot) : 0;
      return `
      <a href="#/${c.slug}" class="series-card" style="--accent:${c.color}">
        <div class="code">${c.code}</div>
        <div class="tagline">${escapeHtml(c.name)}${c.tagline ? ' · ' + escapeHtml(c.tagline) : ''}</div>
        <div class="row"><span>共 <b>${tot}</b></span><span>已收 <b>${own}</b></span>${st.ordered ? `<span>未到货 <b>${st.ordered}</b></span>` : ''}${st.wishlist ? `<span>想要 <b>${st.wishlist}</b></span>` : ''}</div>
        <div class="progress"><div class="bar" style="width:${pct}%"></div></div>
      </a>`;
    }).join('');
    return `<div class="family"><h2 class="family-title">${escapeHtml(g.family || '其他')}</h2><div class="series-grid">${cards}</div></div>`;
  }).join('');

  const upcomingHTML = upcoming.length ? `
    <div class="dash-sec">
      <div class="dash-sec-h">即将发售 · 想要与未到货 <span>${upcoming.length}</span></div>
      <div class="upcoming-row">
        ${upcoming.slice(0, 12).map(m => `
          <div class="upcoming-card" data-id="${escapeAttr(m.id)}">
            <div class="uc-photo${m.photoUrl ? '' : ' noimg'}">${m.photoUrl ? `<img src="${escapeAttr(m.photoUrl)}" loading="lazy">` : ''}</div>
            <div class="uc-name">${escapeHtml(m.nameZh || m.name)}</div>
            <div class="uc-meta"><span class="uc-date">${escapeHtml(m.releaseDate)}</span>${m.price ? `<span>${fmtPrice(m.price)}</span>` : ''}</div>
          </div>`).join('')}
      </div>
    </div>` : '';

  $('main').innerHTML = `<div class="landing dash">
    <h1>我的收藏库</h1>
    <!-- column count is set from the tile count: the 未到货 tile only appears
         when something is on its way, and auto-fit would wrap it to 4+1. -->
    <div class="dash-metrics" style="--cols:${onWay.length ? 5 : 4}">
      <div class="metric"><div class="mv">${total}</div><div class="ml">总收录</div></div>
      <a href="#/mine" class="metric"><div class="mv ok">${owned.length}</div><div class="ml">已拥有 ›</div></a>
      ${onWay.length ? `<a href="#/mine" class="metric"><div class="mv way">${onWay.length}</div><div class="ml">未到货 ›</div></a>` : ''}
      <a href="#/mine" class="metric"><div class="mv want">${wish.length}</div><div class="ml">想要 ›</div></a>
      <div class="metric"><div class="mv">${spent ? fmtPrice(spent) : '¥0'}</div><div class="ml">已投入</div></div>
    </div>
    ${upcomingHTML}
    ${sections}
  </div>`;
  $('main').querySelectorAll('.upcoming-card').forEach(el => { el.onclick = () => openView(el.dataset.id); });
}

// ---------- my collection (aggregate across all series) ----------
function parsePrice(s) {
  const digits = String(s || '').replace(/[^\d]/g, '');
  return digits ? parseInt(digits, 10) : 0;
}
// price is stored as a bare number; display as ¥ with thousands separators
function fmtPrice(s) {
  const n = parsePrice(s);
  return n ? '¥' + n.toLocaleString('ja-JP') : '';
}

async function renderMine() {
  document.title = '我的收藏 · Bandai 收藏';
  const items = await api.listItems({ marked: 1 });
  state.items = items;        // so openView can find them
  state.col = null;

  const owned = items.filter(m => isOwned(m.status));
  const wish = items.filter(m => m.status === 'wishlist');
  const onWay = items.filter(m => m.status === 'ordered');
  const spent = owned.reduce((n, m) => n + parsePrice(m.price), 0);
  const today = new Date().toISOString().slice(0, 10);

  // group by collection, in nav order
  const order = state.collections.map(c => c.code);
  const byColl = {};
  for (const m of items) (byColl[m.series] = byColl[m.series] || []).push(m);
  const rank = { done: 0, wip: 1, sealed: 2, ordered: 3, wishlist: 4, none: 5 };
  const sections = order.filter(code => byColl[code]).map(code => {
    const col = byCode(code) || { code, type: 'kit', color: '#e8467a' };
    const list = byColl[code].slice().sort((a, b) => (rank[a.status] - rank[b.status]) || (b.releaseDate || '').localeCompare(a.releaseDate || ''));
    return `<div class="family">
      <h2 class="family-title" style="color:${col.color}">${escapeHtml(col.code)} <span style="opacity:.6">${list.length}</span></h2>
      <div class="grid">${list.map(m => cardHTML(m, col.type, today)).join('')}</div>
    </div>`;
  }).join('');

  $('main').innerHTML = `<div class="series-page">
    <div class="head"><h1 style="color:var(--primary)">★ 我的收藏</h1></div>
    <div class="mine-stats">
      <div class="stat"><div class="n">${owned.length}</div><div class="l">已拥有</div></div>
      ${onWay.length ? `<div class="stat"><div class="n way">${onWay.length}</div><div class="l">未到货</div></div>` : ''}
      <div class="stat"><div class="n">${wish.length}</div><div class="l">想要</div></div>
      <div class="stat"><div class="n">¥${spent.toLocaleString()}</div><div class="l">估算投入</div></div>
    </div>
    ${items.length ? sections : '<div class="empty">还没有标记任何收藏。进入某个系列，点卡片设置状态吧。</div>'}
  </div>`;
  $('main').querySelectorAll('.card').forEach(el => { el.onclick = () => openView(el.dataset.id); });
}

// ---------- 设置 ----------
// Presets rather than a free-text duration: the field is a Go duration string
// and a typo would silently disable the scheduler.
const INTERVALS = [
  { v: '', l: '关闭' },
  { v: '24h', l: '每天' },
  { v: '72h', l: '每 3 天' },
  { v: '168h', l: '每周' },
  { v: '336h', l: '每 2 周' },
  { v: '720h', l: '每 30 天' },
];

function fmtTime(unix) {
  if (!unix) return '—';
  const d = new Date(unix * 1000);
  const p = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function fmtSize(bytes) {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + ' KB';
  return (bytes / 1024 / 1024).toFixed(1) + ' MB';
}

function backupListHTML(list) {
  if (!list || !list.length) return '<span class="set-label"><small>还没有备份</small></span>';
  return `<div class="bk-items">${list.map(b => `
    <a class="bk-item" href="/api/backups/${encodeURIComponent(b.name)}" download>
      <span class="bk-when">${fmtTime(b.taken)}</span>
      <span class="bk-size">${fmtSize(b.size)}</span>
      <span class="bk-dl">下载 ↓</span>
    </a>`).join('')}</div>`;
}

function intervalSelect(id, cur) {
  const known = INTERVALS.some(i => i.v === cur);
  return `<select id="${id}">${INTERVALS.map(i =>
    `<option value="${i.v}" ${i.v === (known ? cur : '') ? 'selected' : ''}>${i.l}</option>`).join('')}</select>`;
}

async function renderSettings() {
  document.title = '设置 · Bandai 收藏';
  state.col = null;
  let cfg, backups = [];
  try {
    [cfg, backups] = await Promise.all([api.settings(), api.backups().catch(() => [])]);
  } catch (e) {
    $('main').innerHTML = `<div class="landing"><h1>设置</h1><div class="empty">读取失败: ${escapeHtml(e.message)}</div></div>`;
    return;
  }
  $('main').innerHTML = `<div class="landing settings">
    <h1>设置</h1>
    <p class="lede">自动更新在后台抓取万代官网。两种更新各有独立的周期，设置存在数据库里，重新部署不会丢。</p>

    <div class="set-card">
      <div class="set-row">
        <div class="set-label"><b>增量更新</b><small>只读列表页：新品、改价、改名。约 1 分钟</small></div>
        ${intervalSelect('s-interval', cfg.autoInterval)}
      </div>
      <div class="set-row">
        <div class="set-label"><b>全量更新</b><small>逐个打开详情页补齐多图，并替换占位图。数分钟，建议设得比增量长</small></div>
        ${intervalSelect('s-full', cfg.fullInterval)}
      </div>
      <div class="set-actions">
        <span class="set-state" id="s-state"></span>
        <button id="s-save" class="primary">保存</button>
      </div>
    </div>

    <div class="set-card">
      <div class="set-row"><div class="set-label"><b>增量 · 上次 / 下次</b></div>
        <span>${fmtTime(cfg.lastRun)} → ${cfg.nextRun ? fmtTime(cfg.nextRun) : '已关闭'}</span></div>
      <div class="set-row"><div class="set-label"><b>全量 · 上次 / 下次</b></div>
        <span>${fmtTime(cfg.lastFullRun)} → ${cfg.nextFullRun ? fmtTime(cfg.nextFullRun) : '已关闭'}</span></div>
      <div class="set-row"><div class="set-label"><b>手动更新</b><small>随时触发一次，与自动更新共用同一把锁</small></div>${refreshBtnHTML('')}</div>
      <div class="set-row set-progress" id="s-progress" hidden>
        <div class="prog-wrap">
          <div class="prog-text" id="s-progress-text"></div>
          <div class="prog-track"><div class="prog-bar" id="s-progress-bar"></div></div>
          <div class="prog-fail" id="s-progress-fail" hidden></div>
        </div>
      </div>
    </div>

    <div class="set-card">
      <div class="set-row">
        <div class="set-label"><b>自动备份</b><small>快照会先做完整性校验，不合格直接丢弃。每份约 300 KB</small></div>
        ${intervalSelect('s-backup', cfg.backupInterval)}
      </div>
      <div class="set-row">
        <div class="set-label"><b>保留份数</b><small>超出的自动删除，最旧的先删</small></div>
        <input id="s-keep" type="number" min="1" max="365" value="${cfg.backupKeep || 14}">
      </div>
      <div class="set-row">
        <div class="set-label"><b>备份 · 上次 / 下次</b></div>
        <span>${fmtTime(cfg.lastBackup)} → ${cfg.nextBackup ? fmtTime(cfg.nextBackup) : '已关闭'}</span>
      </div>
      <div class="set-row">
        <div class="set-label"><b>现有备份</b><small>下载一份存到别处——快照和数据库在同一块盘上</small></div>
        <span class="set-state" id="b-state"></span>
        <button id="b-now">立即备份</button>
      </div>
      <div class="set-row backup-list" id="b-list">${backupListHTML(backups)}</div>
    </div>
  </div>`;

  $('b-now').onclick = async () => {
    const st = $('b-state');
    st.textContent = '备份中…'; st.className = 'set-state';
    try {
      const r = await api.runBackup();
      st.textContent = '已备份 ✓'; st.className = 'set-state ok';
      $('b-list').innerHTML = backupListHTML(await api.backups());
    } catch (e) {
      st.textContent = '失败: ' + e.message; st.className = 'set-state err';
    }
  };

  $('s-save').onclick = async () => {
    const st = $('s-state');
    st.textContent = '保存中…'; st.className = 'set-state';
    try {
      await api.saveSettings({
        autoInterval: $('s-interval').value,
        fullInterval: $('s-full').value,
        backupInterval: $('s-backup').value,
        backupKeep: Math.max(1, Math.min(365, +$('s-keep').value || 14)),
      });
      st.textContent = '已保存 ✓'; st.className = 'set-state ok';
      setTimeout(() => render(), 800);   // refresh the "next run" line
    } catch (e) {
      st.textContent = '保存失败: ' + e.message; st.className = 'set-state err';
    }
  };
  wireRefresh();
}

// ---------- collection page ----------
async function loadCollection(code) {
  state.col = byCode(code);
  if (!state.col) { location.hash = ''; return; }
  const [items, cats] = await Promise.all([
    api.listItems({ series: code }),
    api.categories(code),
  ]);
  state.items = items;
  state.cats = cats;
  if (state.filter.category !== 'all' && !cats.includes(state.filter.category)) state.filter.category = 'all';
  renderCollectionPage();
}

function statusCounts() {
  const c = { all: state.items.length, none: 0, wishlist: 0, ordered: 0, sealed: 0, wip: 0, done: 0 };
  for (const m of state.items) c[m.status] = (c[m.status] || 0) + 1;
  return c;
}

function renderCollectionPage() {
  const col = state.col;
  document.documentElement.style.setProperty('--primary', col.color);
  document.title = `${col.code} · Bandai 收藏`;
  const counts = statusCounts();
  const owned = counts.sealed + counts.wip + counts.done;   // in hand; see isOwned

  // category chip data (few categories → chips, not a dropdown)
  const catCounts = {};
  for (const m of state.items) catCounts[m.category] = (catCounts[m.category] || 0) + 1;
  const catChips = [{ k: 'all', l: '全部', n: state.items.length }]
    .concat(state.cats.map(c => ({ k: c, l: c, n: catCounts[c] || 0 })));
  $('main').innerHTML = `
    <div class="series-page" style="--accent:${col.color}">
      <div class="head">
        <h1 style="color:${col.color}">${col.code}</h1>
        <span class="sub">${escapeHtml(col.name)}${col.tagline ? ' · ' + escapeHtml(col.tagline) : ''} · 共 ${counts.all} 件 · 已收集 <span id="owned-count">${owned}</span></span>
        <div class="head-actions">
          <span class="save-state" id="save-state"></span>
          <div class="view-toggle">
            <button data-v="grid" class="${state.view === 'grid' ? 'active' : ''}" aria-label="网格视图">▦</button>
            <button data-v="list" class="${state.view === 'list' ? 'active' : ''}" aria-label="列表视图">☰</button>
          </div>
        </div>
      </div>
      <div class="filterbar${state.filterOpen ? ' open' : ''}">
        <div class="search-wrap">
          <input id="search" class="search" type="search" placeholder="🔍 搜索…" value="${escapeAttr(state.filter.search)}">
          <button id="search-clear" class="search-clear${state.filter.search ? ' show' : ''}" type="button" aria-label="清空">✕</button>
        </div>
        ${state.cats.length ? `<div class="chipset" id="cat-chips"><span class="setlbl">分类</span>
          ${catChips.map(c => `<span class="pill ${state.filter.category === c.k ? 'active' : ''}" data-cat="${escapeAttr(c.k)}">${escapeHtml(c.l)}<span class="count">${c.n}</span></span>`).join('')}
        </div><span class="setdiv"></span>` : ''}
        <div class="chipset" id="status-chips"><span class="setlbl">状态</span>
          ${statusSet(col.type).map(s => `<span class="pill ${state.filter.status === s.key ? 'active' : ''}" data-status="${s.key}">${s.label}<span class="count">${counts[s.key] ?? 0}</span></span>`).join('')}
        </div>
      </div>
      <div id="filter-backdrop"${state.filterOpen ? ' class="show"' : ''}></div>
      <button id="filter-fab" aria-label="搜索与筛选">🔍${activeFilterCount() ? `<span class="fab-badge">${activeFilterCount()}</span>` : ''}</button>
      <div class="grid${state.view === 'list' ? ' list' : ''}" id="grid"></div>
      <div class="empty" id="empty" hidden>没有匹配的条目</div>
    </div>`;
  wireToolbar();
  buildGrid();
}

// ---------- 手动更新 ----------
// A refresh runs on the server for minutes, so the button fires and forgets,
// then polls for progress. Only one run exists process-wide (the weekly job
// uses the same lock), so the button also reflects a scrape someone else —
// or the scheduler — started.

let scrapeTimer = null;

function refreshBtnHTML(slug) {
  // Two buttons, because the costs differ by two orders of magnitude: the
  // incremental pass reads a handful of listing pages, the full one opens
  // every item's detail page.
  return `<span class="refresh-group">
    <button class="refresh-btn" id="refresh-btn" data-slug="${escapeAttr(slug || '')}" data-mode="incremental" title="只读列表页，检查新品与价格变动（约 1 分钟）"><span class="rb-icon">↻</span><span class="rb-label">检查更新</span></button>
    <button class="refresh-btn rb-full" id="refresh-full" data-slug="${escapeAttr(slug || '')}" data-mode="full" title="逐个打开商品详情页，补齐多图与详情（数分钟）">全量</button>
  </span>`;
}

// The settings page carries a full progress line; the button only needs to
// show that something is running.
function paintProgress(st) {
  const box = $('s-progress');
  if (!box) return;
  if (!st.running) { box.hidden = true; return; }
  const kind = st.mode === 'full' ? '全量' : '增量';
  const parts = [`${kind}更新中`];
  if (st.total) parts.push(`系列 ${st.completed + 1}/${st.total}`);
  if (st.current) parts.push(st.current);
  if (st.phase === 'listing') parts.push('读取列表页');
  if (st.phase === 'gallery') parts.push(`详情页 ${st.itemsDone}/${st.itemsAll}`);
  if (st.photos) parts.push(`已下载 ${st.photos} 张图`);
  if ((st.newItems || []).length) parts.push(`发现 ${st.newItems.length} 个新品`);
  $('s-progress-text').textContent = parts.join(' · ');

  // Two nested bars would be confusing; show whichever phase is finer-grained.
  let pct = st.total ? (st.completed / st.total) * 100 : 0;
  if (st.phase === 'gallery' && st.itemsAll) {
    pct = ((st.completed + st.itemsDone / st.itemsAll) / (st.total || 1)) * 100;
  }
  $('s-progress-bar').style.width = Math.min(100, Math.max(2, pct)) + '%';
  const fails = (st.failures || []).length;
  const fe = $('s-progress-fail');
  fe.hidden = !fails;
  if (fails) fe.textContent = `${fails} 项失败（详见容器日志）`;
  box.hidden = false;
}

function paintRefreshBtn(st) {
  const btn = $('refresh-btn');
  if (!btn) return;
  const full = $('refresh-full');
  if (full) full.disabled = !!st.running;
  paintProgress(st);
  const label = btn.querySelector('.rb-label');
  btn.classList.toggle('running', !!st.running);
  btn.disabled = !!st.running;
  if (st.running) {
    const scope = st.current ? ` · ${st.current}` : '';
    const prog = st.total ? ` ${st.completed}/${st.total}` : '';
    const kind = st.mode === 'full' ? '全量' : '';
    const pics = st.photos ? ` · ${st.photos} 图` : '';
    label.textContent = `${kind}更新中${prog}${scope}${pics}`;
  } else {
    label.textContent = '检查更新';
  }
}

function refreshDone(st) {
  const btn = $('refresh-btn');
  if (!btn) return;
  const full = $('refresh-full');
  if (full) full.disabled = false;
  paintProgress({ running: false });
  const label = btn.querySelector('.rb-label');
  btn.classList.remove('running');
  btn.disabled = false;
  const n = (st.newItems || []).length;
  const pics = st.photos || 0;
  if (st.err) {
    btn.classList.add('failed');
    label.textContent = '更新失败';
  } else {
    btn.classList.add('ok');
    label.textContent = n ? `发现 ${n} 个新品` : (pics ? `补齐 ${pics} 张图` : '已是最新');
  }
  // Let the result read for a moment, then settle back. A run that found
  // something re-renders, so the new items actually appear.
  setTimeout(() => {
    if (n || pics) { render(); return; }
    btn.classList.remove('ok', 'failed');
    paintRefreshBtn({ running: false });
  }, 2500);
}

function pollScrape() {
  clearTimeout(scrapeTimer);
  scrapeTimer = setTimeout(async () => {
    let st;
    try { st = await api.scrapeStatus(); } catch { return; }
    if (!$('refresh-btn')) return;   // navigated away
    if (st.running) { paintRefreshBtn(st); pollScrape(); }
    else refreshDone(st);
  }, 1500);
}

function wireRefresh() {
  const btn = $('refresh-btn');
  if (!btn) return;
  const start = async (el) => {
    if (el.dataset.mode === 'full' &&
        !confirm('全量更新会逐个打开每件商品的详情页补齐多图，需要数分钟。继续？')) return;
    paintRefreshBtn({ running: true, mode: el.dataset.mode });
    try {
      paintRefreshBtn(await api.scrape(el.dataset.slug, el.dataset.mode));
    } catch (e) {
      // 409 = the weekly job (or another tab) got there first; just follow it.
      if (!/already running/i.test(e.message || '')) {
        btn.classList.add('failed');
        btn.querySelector('.rb-label').textContent = '启动失败';
        btn.disabled = false;
        return;
      }
    }
    pollScrape();
  };
  btn.onclick = () => start(btn);
  const full = $('refresh-full');
  if (full) full.onclick = () => start(full);
  // Pick up a run already in flight (weekly job, or started in another tab).
  api.scrapeStatus().then(st => { if (st.running) { paintRefreshBtn(st); pollScrape(); } }).catch(() => {});
}

function activeFilterCount() {
  return (state.filter.category !== 'all' ? 1 : 0)
       + (state.filter.status !== 'all' ? 1 : 0)
       + (state.filter.search.trim() ? 1 : 0);
}

// keep the FAB badge in sync without re-rendering the whole page
function updateFabBadge() {
  const fab = $('filter-fab');
  if (!fab) return;
  const n = activeFilterCount();
  let b = fab.querySelector('.fab-badge');
  if (n) {
    if (!b) { b = document.createElement('span'); b.className = 'fab-badge'; fab.appendChild(b); }
    b.textContent = n;
  } else if (b) {
    b.remove();
  }
}

function wireToolbar() {
  // mobile: FAB toggles the bottom-sheet filter panel; backdrop tap closes
  const fb = document.querySelector('.filterbar');
  const setOpen = v => {
    state.filterOpen = v;
    fb.classList.toggle('open', v);
    $('filter-backdrop').classList.toggle('show', v);
  };
  $('filter-fab').onclick = () => setOpen(!state.filterOpen);
  $('filter-backdrop').onclick = () => setOpen(false);

  const syncSearchUI = () => {
    $('search-clear').classList.toggle('show', !!$('search').value);
    updateFabBadge();
  };
  $('search').oninput = e => { state.filter.search = e.target.value; syncSearchUI(); applyFilter(); };
  $('search-clear').onclick = () => {
    $('search').value = '';
    state.filter.search = '';
    syncSearchUI();
    applyFilter();
    $('search').focus();
  };
  // clicking an already-active chip deselects it (back to "all")
  const catChipsEl = $('cat-chips');
  if (catChipsEl) catChipsEl.querySelectorAll('.pill').forEach(el => {
    el.onclick = () => {
      state.filter.category = (state.filter.category === el.dataset.cat) ? 'all' : el.dataset.cat;
      refreshChipActive();
      applyFilter();
    };
  });
  $('status-chips').querySelectorAll('.pill').forEach(el => {
    el.onclick = () => {
      state.filter.status = (state.filter.status === el.dataset.status) ? 'all' : el.dataset.status;
      refreshChipActive();
      applyFilter();
    };
  });
  // grid / list view toggle
  document.querySelectorAll('.view-toggle button').forEach(el => {
    el.onclick = () => {
      if (state.view === el.dataset.v) return;
      state.view = el.dataset.v;
      localStorage.setItem('view', state.view);
      document.querySelectorAll('.view-toggle button').forEach(b => b.classList.toggle('active', b.dataset.v === state.view));
      $('grid').classList.toggle('list', state.view === 'list');
      buildGrid();
    };
  });
}

// Build every card once. Filtering afterwards only toggles visibility, so the
// 100+ <img> elements are never recreated/re-decoded — that's what was janky.
function buildGrid() {
  const today = new Date().toISOString().slice(0, 10);
  const type = state.col.type;
  const grid = $('grid');
  const render = state.view === 'list' ? rowHTML : cardHTML;
  grid.innerHTML = state.items.map(m => render(m, type, today)).join('');
  grid.querySelectorAll('.item').forEach(el => { el.onclick = () => openView(el.dataset.id); });
  applyFilter();
}

// Show/hide existing cards according to the current filter — no DOM rebuild.
function applyFilter() {
  const grid = $('grid');
  if (!grid) return;
  const q = state.filter.search.trim().toLowerCase();
  const cat = state.filter.category, st = state.filter.status;
  let visible = 0;
  grid.querySelectorAll('.item').forEach(el => {
    const show =
      (cat === 'all' || el.dataset.cat === cat) &&
      (st === 'all' || el.dataset.status === st) &&
      (!q || (el.dataset.search || '').includes(q));
    el.style.display = show ? '' : 'none';
    if (show) visible++;
  });
  const empty = $('empty');
  if (empty) empty.hidden = visible > 0;
}

// Rebuild a single card in place (after its status changed) — one image, cheap.
function refreshCard(m) {
  const grid = $('grid');
  if (!grid || !state.col) return;
  const el = grid.querySelector(`.item[data-id="${m.id}"]`);
  if (!el) return;
  const render = state.view === 'list' ? rowHTML : cardHTML;
  const tmp = document.createElement('template');
  tmp.innerHTML = render(m, state.col.type, new Date().toISOString().slice(0, 10)).trim();
  const fresh = tmp.content.firstElementChild;
  fresh.onclick = () => openView(m.id);
  el.replaceWith(fresh);
}

// Update the status-chip counts + "已收集 N" without rebuilding the toolbar.
function refreshStatusUI() {
  const counts = statusCounts();
  document.querySelectorAll('#status-chips .pill').forEach(el => {
    const c = el.querySelector('.count');
    if (c) c.textContent = counts[el.dataset.status] ?? 0;
  });
  const oc = $('owned-count');
  if (oc) oc.textContent = counts.sealed + counts.wip + counts.done;
}

// Reflect the active filter on the chips without rebuilding the page.
function refreshChipActive() {
  document.querySelectorAll('#cat-chips .pill').forEach(el =>
    el.classList.toggle('active', el.dataset.cat === state.filter.category));
  document.querySelectorAll('#status-chips .pill').forEach(el =>
    el.classList.toggle('active', el.dataset.status === state.filter.status));
  updateFabBadge();
}

// shared card markup (used by collection grid + "my collection" page)
function cardHTML(m, type, today) {
  const rd = m.releaseDate || '';
  const upcoming = rd && rd > today;
  const search = `${m.name} ${m.nameZh} ${m.notes}`.toLowerCase();
  return `
    <div class="item card${m.status !== 'none' ? ' is-marked s-' + m.status : ''}" data-id="${escapeAttr(m.id)}" data-cat="${escapeAttr(m.category || '')}" data-status="${escapeAttr(m.status)}" data-search="${escapeAttr(search)}">
      <div class="photo${m.photoUrl ? '' : ' noimg'}">
        ${m.photoUrl ? `<img src="${escapeAttr(m.photoUrl)}" loading="lazy" onerror="this.style.display='none';this.parentElement.classList.add('noimg')">` : ''}
        ${m.status !== 'none' ? `<span class="status-chip s-${m.status}">${statusLabel(m.status, type)}</span>` : ''}
        ${upcoming ? '<span class="soon-chip">未发售</span>' : ''}
      </div>
      <div class="card-body">
        <div class="name">${escapeHtml(m.name || m.nameZh || '(未命名)')}</div>
        ${m.nameZh && m.name ? `<div class="name-zh">${escapeHtml(m.nameZh)}</div>` : ''}
        <div class="card-foot">
          ${m.category ? `<span class="badge cat">${escapeHtml(m.category)}</span>` : '<span></span>'}
          <span class="rel">${rd ? escapeHtml(rd) : ''}${fmtPrice(m.price) ? `<b>${fmtPrice(m.price)}</b>` : ''}</span>
        </div>
      </div>
    </div>`;
}

// compact list-row markup (used when the collection page is in list view)
function rowHTML(m, type, today) {
  const rd = m.releaseDate || '';
  const upcoming = rd && rd > today;
  const search = `${m.name} ${m.nameZh} ${m.notes}`.toLowerCase();
  return `
    <div class="item row-item${m.status !== 'none' ? ' is-marked s-' + m.status : ''}" data-id="${escapeAttr(m.id)}" data-cat="${escapeAttr(m.category || '')}" data-status="${escapeAttr(m.status)}" data-search="${escapeAttr(search)}">
      <div class="ri-photo${m.photoUrl ? '' : ' noimg'}">${m.photoUrl ? `<img src="${escapeAttr(m.photoUrl)}" loading="lazy" onerror="this.style.display='none';this.parentElement.classList.add('noimg')">` : ''}</div>
      <div class="ri-main">
        <div class="ri-name">${escapeHtml(m.name || m.nameZh || '(未命名)')}</div>
        <div class="ri-sub">${m.category ? escapeHtml(m.category) : ''}${rd ? `<span class="${upcoming ? 'upcoming' : ''}">${escapeHtml(rd)}${upcoming ? ' 未发售' : ''}</span>` : ''}</div>
      </div>
      ${m.status !== 'none' ? `<span class="badge s-${m.status}">${statusLabel(m.status, type)}</span>` : '<span class="badge s-none">未拥有</span>'}
      <div class="ri-price">${fmtPrice(m.price) || ''}</div>
    </div>`;
}

// ---------- view (detail) modal ----------
function openView(id) {
  const m = state.items.find(x => x.id === id);
  if (!m) return;
  state.viewingId = id;
  const col = byCode(m.series) || state.col || {};
  const type = col.type || 'kit';
  const today = new Date().toISOString().slice(0, 10);

  const vp = $('v-photo');
  if (m.photoUrl) {
    vp.className = 'view-photo';
    vp.innerHTML = `<img src="${escapeAttr(m.photoUrl)}" onerror="this.parentElement.className='view-photo noimg';this.parentElement.innerHTML='<span>暂无官方图片</span>'">`;
  } else {
    vp.className = 'view-photo noimg';
    vp.innerHTML = '<span>暂无官方图片</span>';
  }
  // The list payload only carries the cover. Show it at once, then fetch the
  // rest so opening a card never waits on a request.
  $('v-thumbs').hidden = true;
  loadGallery(id, m.photoUrl);
  $('v-badges').innerHTML =
    `<span class="badge series" style="background:${col.color}22;color:${col.color}">${escapeHtml(col.code || m.series)}</span>` +
    (m.category ? `<span class="badge cat">${escapeHtml(m.category)}</span>` : '');
  $('v-name').textContent = m.name || m.nameZh || '(未命名)';
  const zh = $('v-namezh');
  if (m.nameZh && m.name) { zh.textContent = m.nameZh; zh.hidden = false; } else zh.hidden = true;

  const facts = [];
  if (m.releaseDate) facts.push(['发售日', m.releaseDate, m.releaseDate > today]);
  const fp = fmtPrice(m.price);
  if (fp) facts.push(['定价', fp, false]);
  $('v-facts').innerHTML = facts.map(([k, v, up]) =>
    `<div class="fact"><dt>${k}</dt><dd class="${up ? 'upcoming' : ''}">${escapeHtml(v)}${up ? ' · 未发售' : ''}</dd></div>`).join('');

  const url = officialUrl(m);
  const a = $('v-official');
  if (url) { a.href = url; a.hidden = false; } else a.hidden = true;

  renderViewStatus(m, type);

  const nw = $('v-notes-wrap');
  if (m.notes) { $('v-notes').textContent = m.notes; nw.hidden = false; } else nw.hidden = true;

  $('view-bg').hidden = false;
}

// ---------- 详情页图库 ----------
let galleryShots = [];

async function loadGallery(id, cover) {
  let shots = [];
  try {
    shots = (await api.item(id)).photos || [];
  } catch { return; }
  if (state.viewingId !== id) return;   // user moved on while we fetched

  // The cover and the gallery's first shot are the same photograph from two
  // places: the cover comes off the listing card, the gallery off the detail
  // page. Sometimes byte-identical, sometimes the same picture at 450px vs
  // 1500px — either way showing both put two near-identical images at the
  // front. When a gallery exists it supersedes the cover, and it carries the
  // larger versions.
  //
  // A photo the OWNER uploaded is different: it isn't in the gallery at all,
  // so it must still lead. Everything this app scraped is named after the item
  // — "<id>.<ext>" for a listing cover, "<id>_<n>.<ext>" for a gallery shot,
  // and the cover is now usually the latter. Uploads are
  // "upload-<date>-<id>.<ext>" and match neither.
  const bare = u => (u || '').split('?')[0].split('/').pop();
  const file = bare(cover);
  const ownUpload = !!cover && !file.startsWith(id + '.') && !file.startsWith(id + '_');
  galleryShots = shots.length ? shots.slice() : (cover ? [cover] : []);
  // Compare without the cache-busting query, or the cover looks absent from a
  // gallery it is in fact already the first entry of.
  if (ownUpload && !galleryShots.some(u => bare(u) === file)) galleryShots.unshift(cover);
  const strip = $('v-thumbs');
  if (galleryShots.length < 2) {
    // Clear as well as hide: leaving the previous item's markup in place is
    // one CSS rule away from showing it.
    strip.innerHTML = '';
    strip.hidden = true;
    return;
  }
  strip.innerHTML = galleryShots.map((u, i) =>
    `<button class="vthumb${i === 0 ? ' active' : ''}" data-i="${i}" aria-label="第 ${i + 1} 张">
       <img src="${escapeAttr(u)}" loading="lazy" alt=""></button>`).join('');
  strip.hidden = false;
  strip.querySelectorAll('.vthumb').forEach(b => {
    b.onclick = () => showShot(+b.dataset.i);
  });
}

function showShot(i) {
  if (i < 0 || i >= galleryShots.length) return;
  const vp = $('v-photo');
  vp.className = 'view-photo';
  vp.innerHTML = `<img src="${escapeAttr(galleryShots[i])}" alt="">`;
  $('v-thumbs').querySelectorAll('.vthumb').forEach(b =>
    b.classList.toggle('active', +b.dataset.i === i));
}

function currentShot() {
  const a = $('v-thumbs').querySelector('.vthumb.active');
  return a ? +a.dataset.i : 0;
}

function renderViewStatus(m, type) {
  const sets = statusSet(type).filter(s => s.key !== 'all');
  $('v-status').innerHTML = sets.map(s =>
    `<span class="pill ${m.status === s.key ? 'active' : ''}" data-k="${s.key}">${s.label}</span>`).join('');
  $('v-status').querySelectorAll('.pill').forEach(el => {
    el.onclick = async () => {
      if (m.status === el.dataset.k) return;
      const prev = m.status;
      m.status = el.dataset.k;
      renderViewStatus(m, type);
      try {
        await api.saveItem({ ...m });            // PUT — backend keeps official fields locked
        // Incrementally update the page behind the modal instead of a full rebuild.
        if (state.col && currentRoute().name === 'collection' && state.col.code === m.series) {
          refreshCard(m);
          refreshStatusUI();
          applyFilter();
          api.stats().then(s => { state.stats = s; renderNav(); }).catch(() => {});
        } else {
          await render();                        // mine / landing — full refresh (rare, few cards)
        }
      } catch (e) {
        m.status = prev; renderViewStatus(m, type);
        alert('状态保存失败: ' + e.message);
      }
    };
  });
}

function closeView() { $('view-bg').hidden = true; state.viewingId = null; }

function showSave(text, color) {
  const el = $('save-state');
  if (!el) return;
  el.textContent = text; el.style.color = color || '';
  if (text.startsWith('已保存') || text.startsWith('刷新完成')) {
    setTimeout(() => { if (el.textContent === text) { el.textContent = ''; el.style.color = ''; } }, 2000);
  }
}

// ---------- modal ----------
// Longer labels for the edit form's status dropdown, per collection type.
const STATUS_OPT_LABELS = {
  kit:      { none: '未拥有', wishlist: '想要 (Wishlist)', ordered: '已购 · 未到货', sealed: '已购 · 未拆', wip: '已购 · 在做', done: '已购 · 已完成' },
  finished: { none: '未拥有', wishlist: '想要 (Wishlist)', ordered: '已购 · 未到货', sealed: '已购 · 未拆', done: '已购 · 已开封' },
};
const colType = code => { const c = byCode(code); return c ? c.type : 'kit'; };

// Build the official product page URL from the item's id + its collection's source.
function officialUrl(item) {
  const c = byCode(item.series);
  if (!c || !item.id) return '';
  if (c.scraper === 'bandai-hobby' && /^01_\d+$/.test(item.id))
    return `https://bandai-hobby.net/item/${item.id}/`;
  // Premium Bandai exclusives are sold on the PB store, not the hobby site.
  if (c.scraper === 'bandai-hobby' && /^pb-\d+$/.test(item.id))
    return `https://p-bandai.jp/item/item-${item.id.slice(3)}/`;
  if (c.scraper === 'tamashii' && /^tw-\d+$/.test(item.id))
    return `https://tamashiiweb.com/item/${item.id.slice(3)}/`;
  return '';
}

function fillSeriesSelect(selected) {
  $('f-series').innerHTML = state.collections.map(c =>
    `<option value="${escapeAttr(c.code)}" ${c.code === selected ? 'selected' : ''}>${escapeHtml(c.code)}</option>`).join('');
}
function fillStatusSelect(type, selected) {
  const labels = STATUS_OPT_LABELS[type] || STATUS_OPT_LABELS.kit;
  const keys = statusSet(type).filter(s => s.key !== 'all').map(s => s.key);
  if (!keys.includes(selected)) selected = 'none';
  $('f-status').innerHTML = keys.map(k => `<option value="${k}" ${k === selected ? 'selected' : ''}>${labels[k]}</option>`).join('');
}
function openEdit(id) {
  const m = state.items.find(x => x.id === id);
  if (!m) return;
  state.editingId = id;
  $('modal-title').textContent = '编辑';
  fillForm(m);
  $('modal-delete').style.display = '';
  $('modal-bg').hidden = false;
}
function fillForm(m) {
  $('f-name').value = m.name || '';
  $('f-nameZh').value = m.nameZh || '';
  const seriesCode = m.series || (state.col && state.col.code);
  fillSeriesSelect(seriesCode);
  fillStatusSelect(colType(seriesCode), m.status || 'none');
  const url = officialUrl({ ...m, series: seriesCode });
  const link = $('f-official');
  if (url) { link.href = url; link.hidden = false; } else link.hidden = true;
  $('f-category').value = m.category || '';

  // Lock official (scraped) fields — only the user's own fields stay editable.
  const locked = /^(01_\d+|tw-\d+)$/.test(m.id || '');
  $('f-lock-note').hidden = !locked;
  $('f-name').readOnly = locked;
  $('f-price').readOnly = locked;
  $('f-release').readOnly = locked;
  $('f-category').readOnly = locked;
  $('f-series').disabled = locked;
  $('f-release').value = m.releaseDate || '';
  $('f-price').value = m.price || '';
  $('f-photo').value = m.photoUrl || '';
  $('f-notes').value = m.notes || '';
  updatePhotoPreview();
  $('cats').innerHTML = (state.cats || []).map(c => `<option value="${escapeAttr(c)}">`).join('');
}
function closeModal() { $('modal-bg').hidden = true; state.editingId = null; }
async function saveModal() {
  const payload = {
    id: state.editingId || newID(),
    name: $('f-name').value.trim(),
    nameZh: $('f-nameZh').value.trim(),
    series: $('f-series').value,
    category: $('f-category').value.trim(),
    status: $('f-status').value,
    releaseDate: $('f-release').value.trim(),
    price: $('f-price').value.trim(),
    photoUrl: $('f-photo').value.trim(),
    notes: $('f-notes').value.trim(),
  };
  if (!payload.name && !payload.nameZh) { alert('请至少填写一个名称'); return; }
  showSave('保存中...', '#7a7074');
  try {
    await api.saveItem(payload);
    showSave('已保存 ✓', '#047857');
    closeModal();
    // If item moved to another collection, just reload current.
    [state.stats] = [await api.stats()];
    renderNav();
    await loadCollection(state.col.code);
  } catch (e) { showSave('保存失败: ' + e.message, '#b91c1c'); }
}
async function deleteModal() {
  if (!state.editingId) return;
  if (!confirm('确认删除？')) return;
  try {
    await api.deleteItem(state.editingId);
    closeModal();
    state.stats = await api.stats();
    renderNav();
    await loadCollection(state.col.code);
  } catch (e) { alert(e.message); }
}
function newID() { return 'user-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 6); }
function updatePhotoPreview() {
  const url = $('f-photo').value.trim();
  const img = $('f-photo-preview');
  if (url) { img.src = url; img.classList.add('show'); } else img.classList.remove('show');
}

$('modal-cancel').onclick = closeModal;
$('modal-save').onclick = saveModal;
$('modal-delete').onclick = deleteModal;
$('f-series').onchange = () => fillStatusSelect(colType($('f-series').value), $('f-status').value);
$('f-photo').oninput = updatePhotoPreview;
$('f-photo-clear').onclick = () => { $('f-photo').value = ''; updatePhotoPreview(); };
$('f-photo-drop').onclick = () => $('f-photo-file').click();
$('f-photo-drop').addEventListener('dragover', e => { e.preventDefault(); $('f-photo-drop').classList.add('dragover'); });
$('f-photo-drop').addEventListener('dragleave', () => $('f-photo-drop').classList.remove('dragover'));
$('f-photo-drop').addEventListener('drop', e => {
  e.preventDefault(); $('f-photo-drop').classList.remove('dragover');
  const f = e.dataTransfer.files[0];
  if (f && f.type.startsWith('image/')) uploadPhoto(f);
});
$('f-photo-file').onchange = e => { if (e.target.files[0]) uploadPhoto(e.target.files[0]); e.target.value = ''; };
async function uploadPhoto(file) {
  const drop = $('f-photo-drop');
  drop.textContent = '上传中...';
  try {
    const r = await api.upload(file);
    $('f-photo').value = r.url; updatePhotoPreview();
    drop.textContent = '已上传 ✓';
    setTimeout(() => drop.textContent = '拖拽图片到此 / 点击上传', 1500);
  } catch (e) { drop.textContent = '上传失败: ' + e.message; }
}
$('modal-bg').onclick = e => { if (e.target === $('modal-bg')) closeModal(); };

// view modal wiring
$('view-close').onclick = closeView;
$('view-edit').onclick = () => { const id = state.viewingId; closeView(); openEdit(id); };
$('view-bg').onclick = e => { if (e.target === $('view-bg')) closeView(); };

document.addEventListener('keydown', e => {
  if (e.key === 'Escape') {
    if (!$('modal-bg').hidden) closeModal();
    else if (!$('view-bg').hidden) closeView();
  }
  // Arrow keys page through the gallery while the detail view is open.
  if (!$('view-bg').hidden && galleryShots.length > 1) {
    if (e.key === 'ArrowLeft') { e.preventDefault(); showShot(currentShot() - 1); }
    if (e.key === 'ArrowRight') { e.preventDefault(); showShot(currentShot() + 1); }
  }
  if (e.key === '/' && document.activeElement && document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'TEXTAREA') {
    const s = $('search'); if (s) { e.preventDefault(); s.focus(); }
  }
});

function escapeHtml(s) { return String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function escapeAttr(s) { return escapeHtml(s); }

if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => {}));
}

bootstrap();
