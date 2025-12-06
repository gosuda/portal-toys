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

async function streamHealth(signal) {
  setStatus('Loading portals...', 'info');
  const r = await fetch('/api/health?stream=1', { signal });
  if (!r.ok) {
    let t = '';
    try { t = await r.text(); } catch {}
    const msg = `health fetch failed: ${r.status} ${r.statusText}${t ? ' - ' + t : ''}`;
    throw new Error(msg);
  }
  if (!r.body) {
    throw new Error('Streaming not supported by this browser');
  }
  const reader = r.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  const processBuffer = () => {
    for (;;) {
      const idx = buf.indexOf('\n');
      if (idx === -1) break;
      const line = buf.slice(0, idx).trim();
      buf = buf.slice(idx + 1);
      if (!line) continue;
      try {
        const card = JSON.parse(line);
        upsertPortal(card);
        applySearch();
      } catch (err) {
        console.warn('Failed to parse stream chunk', err, line);
      }
    }
  };
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    processBuffer();
  }
  buf += decoder.decode();
  processBuffer();
  setStatus(`Loaded ${currentList.length} portals`, 'info');
  return currentList;
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
const statusBar = document.getElementById('statusBar');
let currentStreamController = null;

function setStatus(message, level = 'info') {
  if (!statusBar) return;
  statusBar.textContent = message || '';
  statusBar.classList.toggle('error', level === 'error');
}

function clearStatus() {
  setStatus('', 'info');
}

async function refresh() {
  if (refreshing) return;
  refreshing = true;
  const controller = new AbortController();
  currentStreamController = controller;
  currentList = [];
  applySearch();
  try {
    clearStatus();
    await streamHealth(controller.signal);
  } catch (e) {
    if (controller.signal.aborted) {
      return;
    }
    console.error('Streaming health fetch failed, falling back to full fetch', e);
    try {
      const list = await getHealth();
      currentList = Array.isArray(list) ? list : (Array.isArray(list?.data) ? list.data : []);
      applySearch();
      setStatus('Loaded portals (fallback mode)', 'info');
    } catch (err) {
      console.error('Health fetch failed', err);
      setStatus('Connection looks slow or offline. Will keep retrying in the background.', 'error');
    }
  } finally {
    if (currentStreamController === controller) {
      currentStreamController = null;
    }
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

function upsertPortal(card) {
  const key = portalKey(card);
  const idx = key ? currentList.findIndex(it => portalKey(it) === key) : -1;
  if (idx >= 0) {
    currentList[idx] = { ...currentList[idx], ...card };
  } else {
    currentList.push(card);
  }
}

function portalKey(item) {
  const link = String(item.link || item.Link || '').trim().toLowerCase();
  if (link) return link;
  const name = String(item.name || item.Name || '').trim().toLowerCase();
  return name || null;
}

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
setInterval(refresh, 5000);
