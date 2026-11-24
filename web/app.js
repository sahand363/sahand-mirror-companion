'use strict';
// sahand-mirror-companion UI — vanilla JS, no external resources.
const $ = (s, el = document) => el.querySelector(s);
const main = $('#main');
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fa = n => Number(n || 0).toLocaleString('fa-IR');
const when = t => (!t || String(t).startsWith('0001')) ? 'هرگز' : new Date(t).toLocaleString('fa-IR');
const size = n => { if (!n) return '—'; const u = ['B', 'KB', 'MB', 'GB', 'TB']; let i = 0; while (n >= 1024 && i < 4) { n /= 1024; i++; } return n.toFixed(i ? 1 : 0) + ' ' + u[i]; };
let ST = null, statusTimer = null, jobTimer = null;

function toast(m) { const t = $('#toast'); t.textContent = m; t.classList.remove('hidden'); clearTimeout(t._h); t._h = setTimeout(() => t.classList.add('hidden'), 2600); }

async function api(path, opts = {}) {
  const o = { method: opts.method || 'GET', headers: { 'X-Requested-With': 'smc' }, credentials: 'same-origin' };
  if (opts.body !== undefined) { o.headers['Content-Type'] = 'application/json'; o.body = JSON.stringify(opts.body); }
  const r = await fetch('/api/' + path, o);
  if (r.status === 401 && path !== 'login') { showLogin(); throw new Error('login'); }
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || ('HTTP ' + r.status));
  return j;
}

function copy(text) {
  const done = () => toast('کپی شد');
  if (navigator.clipboard && window.isSecureContext) { navigator.clipboard.writeText(text).then(done); return; }
  const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select();
  try { document.execCommand('copy'); done(); } catch (e) { prompt('کپی کنید:', text); }
  ta.remove();
}
document.addEventListener('click', e => { const b = e.target.closest('[data-copy]'); if (b) copy(b.dataset.copy); });

function modal(title, html) { $('#mtitle').textContent = title; $('#mbody').innerHTML = html; $('#modal').classList.remove('hidden'); }
function closeModal() { $('#modal').classList.add('hidden'); clearInterval(jobTimer); }
$('#mclose').onclick = closeModal;
$('#modal').onclick = e => { if (e.target.id === 'modal') closeModal(); };
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeModal(); });

// ---------- login ----------
function showLogin() {
  clearInterval(statusTimer);
  $('#top').classList.add('hidden'); $('#simbanner').classList.add('hidden');
  main.innerHTML = `<form class="card login" id="lf"><h2>ورود به Sahand Mirror Companion</h2>
    <label>نام کاربری</label><input name="u" value="admin" autocomplete="username" class="ltr">
    <label>رمز عبور</label><input name="p" type="password" autocomplete="current-password" class="ltr" autofocus>
    <div id="lerr"></div><p><button>ورود</button></p>
    <p class="muted">رمز را نصب‌کننده چاپ کرده است. برای رمز جدید روی سرور <code>smc</code> را اجرا کنید (گزینهٔ ۲).</p></form>`;
  $('#lf').onsubmit = async e => {
    e.preventDefault();
    try { await api('login', { method: 'POST', body: { username: e.target.u.value, password: e.target.p.value } }); start(); }
    catch (err) { $('#lerr').innerHTML = `<div class="err">${esc(err.message)}</div>`; }
  };
}
$('#logout').onclick = async () => { await api('logout', { method: 'POST' }).catch(() => { }); showLogin(); };

// ---------- top bar & simulator ----------
function renderTop() {
  const s = ST.sim || {};
  $('#top').classList.remove('hidden');
  $('#hubinfo').textContent = 'hub: ' + (ST.hub || 'not set');
  let left = '';
  if (s.on && s.until && !s.until.startsWith('0001')) {
    const m = Math.max(0, Math.round((new Date(s.until) - Date.now()) / 60000));
    left = ` · ${fa(m)} دقیقه تا وصل خودکار`;
  }
  $('#simbox').innerHTML = `<span class="lbl">${s.on ? '🔴 اینترنت ملی (شبیه‌سازی)' : '🟢 اینترنت عادی'}</span>
    <button class="switch ${s.on ? 'on' : ''}" id="simsw" title="شبیه‌ساز اینترنت ملی"></button>`;
  const b = $('#simbanner');
  if (s.on) {
    b.classList.remove('hidden');
    b.textContent = `شبیه‌ساز اینترنت ملی روشن است: همه‌چیز قطع است جز هاب ${ST.hub}${s.dns ? ' (و DNS)' : ''}${left}`;
  } else b.classList.add('hidden');
  $('#simsw').onclick = toggleSim;
}

async function toggleSim() {
  const sw = $('#simsw'); const on = !(ST.sim && ST.sim.on);
  if (on && !confirm(`همهٔ ارتباط‌های خروجی این سرور جز هاب قطع می‌شود. SSH و همین پنل کار می‌کنند.\n${ST.sim_minutes ? `بعد از ${ST.sim_minutes} دقیقه خودکار وصل می‌شود.` : 'بدون زمان‌سنج؛ تا خاموش نکنید روشن می‌ماند.'}\nادامه؟`)) return;
  sw.disabled = true;
  try { ST.sim = await api('sim', { method: 'POST', body: { on } }); renderTop(); toast(on ? 'شبیه‌ساز روشن شد' : 'اینترنت عادی برگشت'); runTest(); }
  catch (e) { toast(e.message); sw.disabled = false; }
}

async function refreshStatus() { ST = await api('status'); renderTop(); return ST; }

// ---------- pages ----------
async function start() {
  await refreshStatus();
  main.innerHTML = `
  <div class="card"><div class="head"><h2>تست دسترسی</h2><button id="testBtn">تست کن</button></div>
    <p class="muted">از روی همین سرور به سایت‌های معروف وصل می‌شود. هر پاسخ HTTP یعنی وصل است.</p>
    <div id="tests" class="tests"></div><div id="verdict"></div></div>
  <div class="card"><div class="head"><h2>برنامه‌های هاب</h2><button id="catBtn">بررسی لیست دانلود</button></div>
    <p class="muted" id="catInfo">برنامه‌هایی که روی هاب آینه شده‌اند و با یک دستور، حتی در قطعی، نصب می‌شوند.</p>
    <div id="catSum" class="summary"></div><div id="apps" class="apps"></div></div>
  <div class="card"><details><summary>تنظیمات</summary><div id="settings"></div></details></div>
  <div class="card"><div class="head"><h2>دسترسی</h2></div>
    <div class="grid2"><div><label>آدرس این پنل</label>${(ST.ui || []).map(u => `<div class="cmd"><code>${esc(u)}</code><button data-copy="${esc(u)}">کپی</button></div>`).join('')}</div>
    <div><label>هاب</label><div class="cmd"><code>${esc(ST.hub || '—')}</code></div><p class="muted">تغییر هاب، کلید، رمز و پورت از منوی SSH: دستور <code>smc</code></p></div></div>
    <p class="muted">نسخه ${esc(ST.version)} · ${esc(ST.hostname)}</p></div>`;
  $('#testBtn').onclick = runTest;
  $('#catBtn').onclick = refreshCatalog;
  renderTestsPlaceholder();
  renderSettings();
  loadCatalog();
  clearInterval(statusTimer);
  statusTimer = setInterval(() => refreshStatus().catch(() => { }), 15000);
}

function renderTestsPlaceholder() {
  const list = [{ name: 'هاب sahand-mirror', url: ST.hub, hub: true }, ...(ST.targets || [])];
  $('#tests').innerHTML = list.map(t => `<div class="t wait ${t.hub ? 'hub' : ''}"><div class="ic">?</div><div><div class="nm">${esc(t.name)}</div><div class="sub">${esc(t.url || '')}</div></div></div>`).join('');
}

async function runTest() {
  const b = $('#testBtn'); if (!b) return; b.disabled = true; b.textContent = 'در حال تست…';
  renderTestsPlaceholder(); $('#verdict').innerHTML = '';
  try {
    const r = await api('test', { method: 'POST' });
    $('#tests').innerHTML = r.results.map(t => `<div class="t ${t.ok ? 'ok' : 'bad'} ${t.hub ? 'hub' : ''}">
      <div class="ic">${t.ok ? '✓' : '✕'}</div><div><div class="nm">${esc(t.name)}</div>
      <div class="sub">${t.ok ? fa(t.ms) + ' ms' + (t.code ? ' · HTTP ' + t.code : '') : esc(t.error || 'قطع')}</div></div></div>`).join('');
    const hub = r.results.find(t => t.hub), others = r.results.filter(t => !t.hub), up = others.filter(t => t.ok).length;
    let v;
    if (hub && !hub.ok) v = ['bad', 'هاب در دسترس نیست! در قطعی واقعی نصب برنامه‌ها کار نمی‌کند. آدرس هاب یا فایروال را بررسی کنید.'];
    else if (up === 0) v = ['good', `فقط هاب در دسترس است — دقیقاً مثل اینترنت ملی. ${r.sim ? '' : '(شبیه‌ساز خاموش است؛ پس این سرور واقعاً قطع است)'}`];
    else if (up === others.length) v = ['mid', 'اینترنت بین‌الملل کامل وصل است. برای آزمایش قطعی، شبیه‌ساز را روشن کنید.'];
    else v = ['mid', `${fa(up)} از ${fa(others.length)} سایت وصل است.${r.sim ? ' شبیه‌ساز روشن است ولی بعضی سایت‌ها باز هستند؛ احتمالاً در فهرست مجاز هستند.' : ''}`];
    $('#verdict').innerHTML = `<div class="verdict ${v[0]}">${esc(v[1])}</div>`;
  } catch (e) { if (e.message !== 'login') toast(e.message); }
  b.disabled = false; b.textContent = 'تست دوباره';
}

// ---------- catalog ----------
let CAT = null;
async function loadCatalog() { try { CAT = await api('catalog'); renderCatalog(); } catch (e) { } }

async function refreshCatalog() {
  const b = $('#catBtn'); b.disabled = true; b.textContent = 'در حال بررسی…';
  try { CAT = await api('catalog/refresh', { method: 'POST' }); renderCatalog(true); }
  catch (e) { if (e.message !== 'login') { $('#catSum').innerHTML = `<div class="err">${esc(e.message)}</div>`; } }
  b.disabled = false; b.textContent = 'بررسی لیست دانلود';
}

function renderCatalog(fresh) {
  if (!CAT || CAT.never) { $('#catSum').innerHTML = '<span class="muted">هنوز بررسی نشده؛ «بررسی لیست دانلود» را بزنید.</span>'; return; }
  const apps = CAT.apps || [];
  const live = apps.filter(a => a.change !== 'removed');
  $('#catInfo').innerHTML = `آخرین بررسی: ${esc(when(CAT.checked))} · هاب ${esc(CAT.version || '')} · آخرین همگام‌سازی هاب با گیت‌هاب: ${esc(when(CAT.last_sync))}`;
  let sum = `<span class="pill">${fa(live.length)} برنامه</span>`;
  if (CAT.first) sum += `<span class="pill">اولین بررسی</span>`;
  else if (!CAT.added && !CAT.removed && !CAT.updated) sum += `<span class="pill">${fresh ? 'تغییری نسبت به بررسی قبل نیست' : 'بدون تغییر'}</span>`;
  if (CAT.added) sum += `<span class="pill ok">${fa(CAT.added)} جدید</span>`;
  if (CAT.updated) sum += `<span class="pill warn">${fa(CAT.updated)} نسخهٔ جدید</span>`;
  if (CAT.removed) sum += `<span class="pill bad">${fa(CAT.removed)} حذف‌شده</span>`;
  $('#catSum').innerHTML = sum;
  $('#apps').innerHTML = apps.map(a => {
    const badge = a.change === 'new' ? '<span class="badge b-new">جدید</span>' : a.change === 'updated' ? `<span class="badge b-upd">به‌روز شد</span>` : a.change === 'removed' ? '<span class="badge b-rm">از هاب حذف شد</span>' : '';
    const r = a.ready || {};
    const ready = a.analyzed ? `<span class="pill ok">آماده ${fa(r.handled)}</span><span class="pill warn">اقدام ${fa(r.action)}</span><span class="pill bad">ناممکن ${fa(r.offline)}</span>` : '<span class="pill">تحلیل نشده</span>';
    const ver = a.latest ? `<span dir="ltr">${esc(a.latest)}</span>${a.prev ? ` <span class="muted" dir="ltr">(قبلاً ${esc(a.prev)})</span>` : ''}` : 'بدون ریلیز';
    return `<div class="app ${a.change === 'removed' ? 'removed' : ''}">
      <h3>${esc(a.name)}${badge}</h3>
      <div class="meta">${a.repo ? `<span dir="ltr">${esc(a.repo)}</span> · ` : ''}نسخه ${ver} · ${fa(a.assets)} فایل (<span dir="ltr">${size(a.size)}</span>)</div>
      <div class="pills" title="آمادگی آفلاین">${ready}</div>
      ${a.error ? `<div class="err ltr">${esc(a.error)}</div>` : ''}
      ${a.change === 'removed' ? '' : `<div class="cmd"><code>${esc(a.install)}</code><button data-copy="${esc(a.install)}">کپی</button></div>
      <div class="row"><button class="sm ghost" data-readme="${esc(a.id)}" ${a.readme ? '' : 'disabled title="README آینه نشده"'}>📖 README</button>
      <button class="sm" data-install="${esc(a.id)}">نصب روی همین سرور</button></div>`}
    </div>`;
  }).join('') || '<p class="muted">هاب هنوز برنامه‌ای ندارد.</p>';
  main.querySelectorAll('[data-readme]').forEach(b => b.onclick = () => showReadme(b.dataset.readme));
  main.querySelectorAll('[data-install]').forEach(b => b.onclick = () => installHere(b.dataset.install));
}

async function showReadme(id) {
  modal(id + ' — README', '<p class="muted">در حال دریافت از هاب…</p>');
  try { const r = await api('readme/' + encodeURIComponent(id)); $('#mtitle').textContent = id + ' — ' + (r.name || 'README'); $('#mbody').innerHTML = `<div class="md">${md(r.markdown)}</div>`; }
  catch (e) { $('#mbody').innerHTML = `<div class="err">${esc(e.message)}</div>`; }
}

async function installHere(id) {
  if (!confirm(`«${id}» از هاب روی همین سرور نصب شود؟\nنصب بدون ترمینال اجرا می‌شود؛ اگر اسکریپت سؤال بپرسد (مثل 3x-ui)، بهتر است دستور را کپی و در SSH اجرا کنید.`)) return;
  try { await api('install/' + encodeURIComponent(id), { method: 'POST' }); }
  catch (e) { toast(e.message); return; }
  modal('نصب ' + id, '<p class="muted" id="jst">در حال اجرا…</p><pre class="log" id="jlog"></pre>');
  const tick = async () => {
    try {
      const j = await api('job'); const l = $('#jlog'); if (!l) return;
      const atEnd = l.scrollTop + l.clientHeight >= l.scrollHeight - 20;
      l.textContent = j.log || ''; if (atEnd) l.scrollTop = l.scrollHeight;
      if (!j.running) { clearInterval(jobTimer); $('#jst').innerHTML = j.exit === 0 ? '<b style="color:var(--ok)">✓ نصب تمام شد</b>' : `<b style="color:var(--bad)">✕ خطا (کد ${esc(j.exit)})</b>`; }
    } catch (e) { clearInterval(jobTimer); }
  };
  clearInterval(jobTimer); jobTimer = setInterval(tick, 1500); tick();
}

// ---------- settings ----------
function renderSettings() {
  $('#settings').innerHTML = `<div class="grid2"><div>
    <label>وصل خودکار شبیه‌ساز بعد از (دقیقه، ۰ = هرگز)</label><input id="smin" type="number" min="0" max="1440" class="ltr" value="${esc(ST.sim_minutes)}">
    <div class="chk"><input type="checkbox" id="sdns" ${ST.sim_allow_dns ? 'checked' : ''}><label for="sdns" style="margin:0">DNS باز بماند (مثل قطعی واقعی که DNS داخلی کار می‌کند)</label></div>
    <label>آدرس‌های مجاز اضافه در شبیه‌ساز (IP یا CIDR، هر خط یکی؛ مثلاً میرور داخلی apt)</label><textarea id="sallow" class="ltr" style="min-height:80px">${esc((ST.sim_allow || []).join('\n'))}</textarea></div>
    <div><label>سایت‌های تست (هر خط: نام | آدرس)</label><textarea id="stargets" class="ltr" style="min-height:230px">${esc((ST.targets || []).map(t => t.name + ' | ' + t.url).join('\n'))}</textarea></div></div>
    <p><button id="ssave">ذخیره</button> <span class="muted">تغییر شبیه‌ساز از دفعهٔ بعد که روشن شود اعمال می‌شود.</span></p>`;
  $('#ssave').onclick = async () => {
    const targets = $('#stargets').value.split('\n').map(l => l.trim()).filter(Boolean).map(l => { const i = l.indexOf('|'); return i < 0 ? { name: '', url: l } : { name: l.slice(0, i).trim(), url: l.slice(i + 1).trim() }; });
    try {
      ST = await api('settings', { method: 'PUT', body: { sim_minutes: Number($('#smin').value) || 0, sim_allow_dns: $('#sdns').checked, sim_allow: $('#sallow').value.split('\n').map(s => s.trim()).filter(Boolean), targets } });
      toast('ذخیره شد'); renderSettings(); renderTestsPlaceholder(); renderTop();
    } catch (e) { toast(e.message); }
  };
}

// ---------- tiny safe markdown renderer ----------
function inline(s) {
  // s is already HTML-escaped
  const codes = [];
  s = s.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return '\u0000' + (codes.length - 1) + '\u0000'; });
  s = s.replace(/!\[([^\]]*)\]\(([^)\s]+)[^)]*\)/g, (_, alt) => alt ? `<span class="muted">[${alt}]</span>` : '');
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)[^)]*\)/g, (_, t, u) => /^(https?:|#)/i.test(u) ? `<a href="${u}" target="_blank" rel="noopener noreferrer">${t}</a>` : t);
  s = s.replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>').replace(/__([^_]+)__/g, '<b>$1</b>');
  s = s.replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, '$1<i>$2</i>');
  s = s.replace(/~~([^~]+)~~/g, '<s>$1</s>');
  s = s.replace(/(^|[\s(])(https?:\/\/[^\s<]+)/g, '$1<a href="$2" target="_blank" rel="noopener noreferrer">$2</a>');
  return s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${codes[i]}</code>`);
}
function md(src) {
  const lines = String(src || '').replace(/\r/g, '').split('\n');
  let out = '', i = 0, para = [];
  const flush = () => { if (para.length) { out += `<p>${inline(para.join(' '))}</p>`; para = []; } };
  const strip = l => l.replace(/<\/?[a-zA-Z][^>]*>/g, '');
  while (i < lines.length) {
    let l = lines[i];
    const fence = l.match(/^\s*(```|~~~)\s*([\w+-]*)/);
    if (fence) {
      flush(); const buf = []; i++;
      while (i < lines.length && !lines[i].trim().startsWith(fence[1])) buf.push(lines[i++]);
      i++; out += `<pre><code>${esc(buf.join('\n'))}</code></pre>`; continue;
    }
    l = strip(l);
    if (!l.trim()) { flush(); i++; continue; }
    let m;
    if ((m = l.match(/^(#{1,6})\s+(.*)$/))) { flush(); const n = m[1].length; out += `<h${n}>${inline(esc(m[2].replace(/#+\s*$/, '')))}</h${n}>`; i++; continue; }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(l)) { flush(); out += '<hr>'; i++; continue; }
    if (/^\s*\|.*\|\s*$/.test(l) && i + 1 < lines.length && /^\s*\|?\s*:?-{2,}/.test(lines[i + 1])) {
      flush(); const row = x => x.trim().replace(/^\||\|$/g, '').split('|').map(c => inline(esc(strip(c).trim())));
      out += '<table><tr>' + row(l).map(c => `<th>${c}</th>`).join('') + '</tr>'; i += 2;
      while (i < lines.length && /^\s*\|.*\|\s*$/.test(lines[i])) { out += '<tr>' + row(lines[i]).map(c => `<td>${c}</td>`).join('') + '</tr>'; i++; }
      out += '</table>'; continue;
    }
    if (/^\s*>/.test(l)) { flush(); const buf = []; while (i < lines.length && /^\s*>/.test(lines[i])) buf.push(strip(lines[i++]).replace(/^\s*>\s?/, '')); out += `<blockquote>${md(buf.join('\n'))}</blockquote>`; continue; }
    if (/^\s*([-*+]|\d+[.)])\s+/.test(l)) {
      flush(); const ordered = /^\s*\d/.test(l); out += ordered ? '<ol>' : '<ul>';
      while (i < lines.length && /^\s*([-*+]|\d+[.)])\s+/.test(lines[i])) {
        let it = strip(lines[i]).replace(/^\s*([-*+]|\d+[.)])\s+/, ''); i++;
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !/^\s*([-*+]|\d+[.)])\s+/.test(lines[i])) it += ' ' + strip(lines[i++]).trim();
        it = it.replace(/^\[( |x)\]\s*/i, (_, x) => x.trim() ? '☑ ' : '☐ ');
        out += `<li>${inline(esc(it))}</li>`;
      }
      out += ordered ? '</ol>' : '</ul>'; continue;
    }
    if (/^ {4}|\t/.test(lines[i])) { flush(); const buf = []; while (i < lines.length && (/^ {4}|\t/.test(lines[i]) || !lines[i].trim())) buf.push(lines[i++].replace(/^ {4}|\t/, '')); out += `<pre><code>${esc(buf.join('\n').trimEnd())}</code></pre>`; continue; }
    para.push(esc(l.trim())); i++;
  }
  flush(); return out;
}

// boot
api('status').then(s => { ST = s; start(); }).catch(() => { });
