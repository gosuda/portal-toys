async function getHealth() {
  const r = await fetch('/api/health');
  if (!r.ok) {
    let t = '';
    try { t = await r.text(); } catch {}
    const msg = `health fetch failed: ${r.status} ${r.statusText}${t ? ' - ' + t : ''}`;
    throw new Error(msg);
  }
  return r.json();
}

function esc(s) { return String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;','\'':'&#39;'}[c])); }

// meta removed per request

function renderList(list) {
  const wrap = document.getElementById('list');
  wrap.innerHTML = '';
  const items = Array.isArray(list) ? list : (Array.isArray(list?.data) ? list.data : []);
  for (const it of items) {
    // When health=1, backend returns normalized fields
    const name = it.name || it.Name || '-';
    const linkRaw = it.link || it.Link || '';
    const link = /^https?:/.test(linkRaw) ? linkRaw : ('https:' + linkRaw.replace(/^\/+/, '//'));
    const conn = (it.connected ?? it.Connected);
    const ok = !!(it.healthy ?? it.Healthy ?? conn);
    const checkedAt = it.checkedAt || it.CheckedAt || null;
    const ago = checkedAt ? secondsAgo(checkedAt) : null;
    const staleClass = (ago == null || ago >= 60) ? ' stale' : '';
    wrap.insertAdjacentHTML('beforeend', `
      <div class="row${staleClass}" data-link="${esc(link)}" data-checked-at="${esc(checkedAt || '')}" tabindex="0" aria-label="${esc(name)}">
        <span class="dot ${ok ? 'ok' : 'bad'}"></span>
        <div class="name" title="${esc(name)}">${esc(name)}</div>
        <div class="url"><a href="${esc(link)}" target="_blank" onclick="event.stopPropagation()">${esc(link)}</a></div>
        <div class="status">${ok ? 'Healthy' : 'Unhealthy'}${ago != null ? ` • ${ago}s ago` : ''}</div>
      </div>
    `);
  }
  // click to open
  for (const el of wrap.querySelectorAll('.row')) {
    el.addEventListener('click', () => {
      const link = el.getAttribute('data-link');
      if (link) window.open(link, '_blank');
    });
  }
}

function secondsAgo(iso) {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.floor((Date.now() - t) / 1000));
}

function tickAgo() {
  for (const el of document.querySelectorAll('.row')) {
    const info = el.querySelector('.status');
    const iso = el.getAttribute('data-checked-at');
    if (!info || !iso) continue;
    const okDot = el.querySelector('.dot');
    const ok = okDot?.classList.contains('ok');
    const ago = secondsAgo(iso);
    info.textContent = `${ok ? 'Healthy' : 'Unhealthy'}${ago != null ? ` • ${ago}s ago` : ''}`;
    if (ago == null || ago >= 60) {
      el.classList.add('stale');
    } else {
      el.classList.remove('stale');
    }
  }
}

let currentList = [];
let refreshing = false;
async function refresh() {
  if (refreshing) return;
  refreshing = true;
  try {
    const list = await getHealth();
    currentList = Array.isArray(list) ? list : (Array.isArray(list?.data) ? list.data : []);
    applySearch();
  } catch (e) {
    console.error('Health fetch failed', e);
    alert('Failed to load the list. ' + (e && e.message ? e.message : ''));
  } finally {
    refreshing = false;
  }
}

document.getElementById('refresh').addEventListener('click', refresh);

async function addPortal(url) {
  const val = (url || '').trim();
  if (!val) return;
  const r = await fetch('/api/sites', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: val }),
  });
  if (!r.ok) {
    const t = await r.text().catch(() => '');
    throw new Error(`${r.status} ${r.statusText}${t ? ' - ' + t : ''}`);
  }
  await refresh();
}

document.getElementById('portalAddBtn').addEventListener('click', async () => {
  const el = document.getElementById('portalInput');
  try {
    await addPortal(el.value);
    el.value = '';
  } catch (err) {
    alert('Failed to register URL. ' + (err && err.message ? err.message : ''));
  }
});

document.getElementById('portalInput').addEventListener('keydown', async (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    document.getElementById('portalAddBtn').click();
  }
});

// search
const searchBox = document.getElementById('searchBox');
searchBox.addEventListener('input', applySearch);

function applySearch() {
  const q = (searchBox.value || '').toLowerCase();
  if (!q) {
    renderList(currentList);
    return;
  }
  const filtered = (currentList || []).filter(it => {
    const name = String(it.name || it.Name || '').toLowerCase();
    const link = String(it.link || it.Link || '').toLowerCase();
    return name.includes(q) || link.includes(q);
  });
  renderList(filtered);
}

refresh();
setInterval(tickAgo, 1000);
setInterval(refresh, 30000);
