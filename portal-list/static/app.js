const listEl = document.getElementById('list');
const searchBox = document.getElementById('searchBox');
const statusBar = document.getElementById('statusBar');
const copyBtn = document.getElementById('copyListBtn');
const portalInput = document.getElementById('portalInput');
const addBtn = document.getElementById('portalAddBtn');
const refreshBtn = document.getElementById('refresh');

const state = {
  items: [],
  refreshing: false,
  firstLoad: true,
  streamCtrl: null,
  cachedFullList: '',
};

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;','\'':'&#39;'}[c]));
const normalizeList = (data) => Array.isArray(data) ? data : (Array.isArray(data?.data) ? data.data : []);
const secondsAgo = (iso) => {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.floor((Date.now() - t) / 1000));
};

const portalLink = (item) => {
  const linkRaw = item.link || item.Link || '';
  if (!linkRaw) return '';
  if (/^https?:/.test(linkRaw)) return linkRaw;
  return 'https:' + String(linkRaw).replace(/^\/+/, '//');
};

const portalKey = (item) => {
  const link = String(item.link || item.Link || '').trim().toLowerCase();
  if (link) return link;
  const name = String(item.name || item.Name || '').trim().toLowerCase();
  return name || null;
};

const upsertPortal = (card) => {
  const key = portalKey(card);
  const idx = key ? state.items.findIndex((it) => portalKey(it) === key) : -1;
  if (idx >= 0) {
    state.items[idx] = { ...state.items[idx], ...card };
  } else {
    state.items.push(card);
  }
};

const renderList = (items) => {
  if (!listEl) return;
  listEl.innerHTML = '';
  for (const it of items) {
    const name = it.name || it.Name || '-';
    const link = portalLink(it);
    const conn = (it.connected ?? it.Connected);
    const ok = !!(it.healthy ?? it.Healthy ?? conn);
    const checkedAt = it.checkedAt || it.CheckedAt || null;
    const ago = checkedAt ? secondsAgo(checkedAt) : null;
    const staleClass = (ago == null || ago >= 60) ? ' stale' : '';
    listEl.insertAdjacentHTML('beforeend', `
      <div class="row${staleClass}" data-link="${esc(link)}" data-checked-at="${esc(checkedAt || '')}" tabindex="0" aria-label="${esc(name)}">
        <span class="dot ${ok ? 'ok' : 'bad'}"></span>
        <div class="name" title="${esc(name)}">${esc(name)}</div>
        <div class="url"><a href="${esc(link)}" target="_blank" onclick="event.stopPropagation()">${esc(link)}</a></div>
        <div class="status">${ok ? 'Healthy' : 'Unhealthy'}${ago != null ? ` • ${ago}s ago` : ''}</div>
      </div>
    `);
  }
  listEl.querySelectorAll('.row').forEach((el) => {
    el.addEventListener('click', () => {
      const link = el.getAttribute('data-link');
      if (link) window.open(link, '_blank');
    });
  });
};

const updateFullList = () => {
  const links = state.items.map(portalLink).filter(Boolean);
  state.cachedFullList = links.join(',');
};

const applySearch = () => {
  updateFullList();
  const q = (searchBox.value || '').toLowerCase();
  if (!q) {
    renderList(state.items);
    return;
  }
  const filtered = state.items.filter((it) => {
    const name = String(it.name || it.Name || '').toLowerCase();
    const link = String(it.link || it.Link || '').toLowerCase();
    return name.includes(q) || link.includes(q);
  });
  renderList(filtered);
};

const setStatus = (message, level = 'info') => {
  if (!statusBar) return;
  statusBar.textContent = message || '';
  statusBar.classList.toggle('error', level === 'error');
};
const clearStatus = () => setStatus('', 'info');

const getHealth = async () => {
  const r = await fetch('/api/health');
  if (!r.ok) {
    const t = await r.text().catch(() => '');
    throw new Error(`health fetch failed: ${r.status} ${r.statusText}${t ? ' - ' + t : ''}`);
  }
  return r.json();
};

const streamHealth = async (signal) => {
  setStatus('Loading portals...', 'info');
  const r = await fetch('/api/health?stream=1', { signal });
  if (!r.ok) {
    const t = await r.text().catch(() => '');
    throw new Error(`health fetch failed: ${r.status} ${r.statusText}${t ? ' - ' + t : ''}`);
  }
  const reader = r.body?.getReader();
  if (!reader) throw new Error('Streaming not supported by this browser');
  const decoder = new TextDecoder();
  let buf = '';
  const flushLines = () => {
    for (;;) {
      const idx = buf.indexOf('\n');
      if (idx === -1) return;
      const line = buf.slice(0, idx).trim();
      buf = buf.slice(idx + 1);
      if (!line) continue;
      try {
        upsertPortal(JSON.parse(line));
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
    flushLines();
  }
  buf += decoder.decode();
  flushLines();
  setStatus(`Loaded ${state.items.length} portals`, 'info');
};

const refresh = async ({ initial = false } = {}) => {
  if (state.refreshing) return;
  const useStream = initial && state.firstLoad;
  state.refreshing = true;

  if (useStream) {
    const controller = new AbortController();
    let timedOut = false;
    const timeoutId = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 10000);
    state.streamCtrl = controller;
    state.items = [];
    applySearch();
    try {
      clearStatus();
      await streamHealth(controller.signal);
      state.firstLoad = false;
    } catch (err) {
      if (controller.signal.aborted && !timedOut) return;
      console.error('Streaming health failed, fallback to full fetch', err);
      try {
        const list = await getHealth();
        state.items = normalizeList(list);
        applySearch();
        setStatus('Loaded portals (fallback mode)', 'info');
        state.firstLoad = false;
      } catch (err2) {
        console.error('Health fetch failed', err2);
        setStatus('Connection looks slow or offline. Will keep retrying in the background.', 'error');
      }
    } finally {
      clearTimeout(timeoutId);
      if (state.streamCtrl === controller) state.streamCtrl = null;
      state.refreshing = false;
    }
    return;
  }

  try {
    setStatus('Refreshing...', 'info');
    const list = await getHealth();
    state.items = normalizeList(list);
    applySearch();
    clearStatus();
  } catch (err) {
    console.error('Health fetch failed', err);
    setStatus('Connection looks slow or offline. Will keep retrying in the background.', 'error');
  } finally {
    state.refreshing = false;
  }
};

const addPortal = async (url) => {
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
};

const tickAgo = () => {
  document.querySelectorAll('.row').forEach((el) => {
    const info = el.querySelector('.status');
    const iso = el.getAttribute('data-checked-at');
    if (!info || !iso) return;
    const ok = el.querySelector('.dot')?.classList.contains('ok');
    const ago = secondsAgo(iso);
    info.textContent = `${ok ? 'Healthy' : 'Unhealthy'}${ago != null ? ` • ${ago}s ago` : ''}`;
    if (ago == null || ago >= 60) el.classList.add('stale'); else el.classList.remove('stale');
  });
};

// Event wiring
refreshBtn?.addEventListener('click', () => refresh());
addBtn?.addEventListener('click', async () => {
  try {
    await addPortal(portalInput.value);
    portalInput.value = '';
  } catch (err) {
    alert('Failed to register URL. ' + (err?.message || ''));
  }
});
portalInput?.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    addBtn?.click();
  }
});
searchBox?.addEventListener('input', applySearch);
copyBtn?.addEventListener('click', async () => {
  if (!navigator.clipboard) {
    alert('Clipboard is not available in this browser.');
    return;
  }
  try {
    await navigator.clipboard.writeText(state.cachedFullList || '');
    setStatus('Copied full list to clipboard', 'info');
  } catch (err) {
    console.error('Copy failed', err);
    setStatus('Failed to copy list', 'error');
  }
});

// Kickoff
refresh({ initial: true });
setInterval(tickAgo, 1000);
setInterval(() => refresh(), 5000);
