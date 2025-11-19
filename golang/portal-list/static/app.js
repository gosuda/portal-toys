const statusBar = document.getElementById('statusBar');
const listHost = document.getElementById('list');
const searchBox = document.getElementById('searchBox');
const refreshButton = document.getElementById('refresh');
const portalInput = document.getElementById('portalInput');
const portalAddButton = document.getElementById('portalAddBtn');

const state = {
  items: [],
  filter: '',
  loading: false,
  error: null,
  lastUpdated: null,
};

let refreshController = null;
const supportsAbortController = typeof AbortController === 'function';
let refreshInFlight = false;

function normalizeList(payload) {
  if (Array.isArray(payload)) return payload;
  if (payload && Array.isArray(payload.data)) return payload.data;
  return [];
}

async function getHealth(signal) {
  const fetchOptions = signal ? { signal } : {};
  const response = await fetch('/api/health', fetchOptions);
  if (!response.ok) {
    let bodyText = '';
    try {
      bodyText = await response.text();
    } catch {
      // ignore
    }
    const suffix = bodyText ? ' - ' + bodyText : '';
    throw new Error(
      'health fetch failed: ' +
        response.status +
        ' ' +
        response.statusText +
        suffix,
    );
  }
  return response.json();
}

function secondsAgo(iso) {
  const timestamp = Date.parse(iso);
  if (Number.isNaN(timestamp)) return null;
  return Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
}

function setStatus(message, level) {
  if (!statusBar) return;
  const effectiveLevel = level || 'info';
  statusBar.textContent = message || '';
  statusBar.classList.toggle('status-bar--error', effectiveLevel === 'error');
}

function deriveStatusMessage() {
  if (state.error) {
    const raw = state.error && state.error.message
      ? state.error.message
      : 'Unexpected error while talking to the API.';
    return 'Unable to refresh portals: ' + raw;
  }

  if (state.loading && state.items.length === 0) {
    return 'Loading portal health...';
  }

  if (!state.items.length) {
    return 'No portals registered yet. Add a portal URL to get started.';
  }

  if (state.loading) {
    const countWhileLoading = state.items.length;
    return (
      'Refreshing ' +
      countWhileLoading +
      ' portal' +
      (countWhileLoading === 1 ? '' : 's') +
      ' in the background.'
    );
  }

  if (state.lastUpdated instanceof Date) {
    const seconds = Math.max(
      0,
      Math.floor((Date.now() - state.lastUpdated.getTime()) / 1000),
    );
    const when = seconds < 5 ? 'just now' : seconds + 's ago';
    const count = state.items.length;
    return (
      'Last updated ' +
      when +
      ' | ' +
      count +
      ' portal' +
      (count === 1 ? '' : 's') +
      '.'
    );
  }

  const fallbackCount = state.items.length;
  return (
    fallbackCount +
    ' portal' +
    (fallbackCount === 1 ? '' : 's') +
    ' loaded.'
  );
}

function renderStatus() {
  const level = state.error ? 'error' : 'info';
  setStatus(deriveStatusMessage(), level);
}

function getVisibleItems() {
  const all = state.items || [];
  const query = (state.filter || '').toLowerCase().trim();
  if (!query) return all;
  return all.filter(function (it) {
    const name = String(it.name || it.Name || '').toLowerCase();
    const link = String(it.link || it.Link || '').toLowerCase();
    return name.indexOf(query) !== -1 || link.indexOf(query) !== -1;
  });
}

function createPortalCard(it) {
  const name = it.name || it.Name || '-';
  const linkRaw = it.link || it.Link || '';
  const safeLinkRaw = String(linkRaw);
  let link;
  if (/^https?:/i.test(safeLinkRaw)) {
    link = safeLinkRaw;
  } else {
    link = 'https:' + safeLinkRaw.replace(/^\/+/, '//');
  }

  const connection = it.connected != null ? it.connected : it.Connected;
  const healthyField =
    it.healthy != null
      ? it.healthy
      : it.Healthy != null
      ? it.Healthy
      : connection;
  const ok = !!healthyField;

  const checkedAt = it.checkedAt || it.CheckedAt || null;
  const ago = checkedAt ? secondsAgo(checkedAt) : null;
  const isStale = ago == null || ago >= 60;

  const card = document.createElement('a');
  card.className = 'portal-card' + (isStale ? ' portal-card--stale' : '');
  card.href = link;
  card.target = '_blank';
  card.rel = 'noreferrer';
  card.dataset.checkedAt = checkedAt || '';
  card.dataset.ok = ok ? '1' : '0';
  card.setAttribute('role', 'listitem');
  card.setAttribute(
    'aria-label',
    name + ' portal (' + (ok ? 'healthy' : 'unhealthy') + ')',
  );

  const header = document.createElement('header');
  header.className = 'portal-card__header';

  const titleGroup = document.createElement('div');
  titleGroup.className = 'portal-card__title-group';

  const dot = document.createElement('span');
  dot.className =
    'portal-card__dot ' + (ok ? 'portal-card__dot--ok' : 'portal-card__dot--bad');

  const nameEl = document.createElement('div');
  nameEl.className = 'portal-card__name';
  nameEl.title = String(name);
  nameEl.textContent = String(name);

  titleGroup.appendChild(dot);
  titleGroup.appendChild(nameEl);

  const statusEl = document.createElement('p');
  statusEl.className = 'portal-card__status';
  let statusText = ok ? 'Healthy' : 'Unhealthy';
  if (ago != null) {
    statusText += ' - ' + ago + 's ago';
  }
  statusEl.textContent = statusText;

  header.appendChild(titleGroup);
  header.appendChild(statusEl);

  const urlEl = document.createElement('p');
  urlEl.className = 'portal-card__url';
  urlEl.textContent = link;

  card.appendChild(header);
  card.appendChild(urlEl);

  card.addEventListener('keydown', function (event) {
    if (event.key === ' ' || event.key === 'Spacebar') {
      event.preventDefault();
      card.click();
    }
  });

  return card;
}

function renderSkeletonCards(host, count) {
  const n = typeof count === 'number' && count > 0 ? count : 4;
  for (let i = 0; i < n; i += 1) {
    const card = document.createElement('div');
    card.className = 'portal-card portal-card--skeleton';
    card.setAttribute('aria-hidden', 'true');

    const header = document.createElement('div');
    header.className = 'portal-card__header';

    const pill = document.createElement('div');
    pill.className = 'portal-card__line portal-card__line--pill';

    const status = document.createElement('div');
    status.className = 'portal-card__line portal-card__line--status';

    header.appendChild(pill);
    header.appendChild(status);

    const title = document.createElement('div');
    title.className = 'portal-card__line portal-card__line--title';

    const url = document.createElement('div');
    url.className = 'portal-card__line portal-card__line--url';

    card.appendChild(header);
    card.appendChild(title);
    card.appendChild(url);

    host.appendChild(card);
  }
}

function renderEmptyState(host, hasFilter) {
  const card = document.createElement('div');
  card.className = 'portal-card portal-card--empty';

  const title = document.createElement('h2');
  title.className = 'portal-card__empty-title';
  title.textContent = hasFilter
    ? 'No portals match this search'
    : 'No portals registered yet';

  const body = document.createElement('p');
  body.className = 'portal-card__empty-body';
  body.textContent = hasFilter
    ? 'Try searching by a different name or URL, or clear the search box.'
    : 'Register a portal URL above to see it appear in this list.';

  card.appendChild(title);
  card.appendChild(body);
  host.appendChild(card);
}

function renderErrorState(host) {
  const card = document.createElement('div');
  card.className = 'portal-card portal-card--error';

  const title = document.createElement('h2');
  title.className = 'portal-card__empty-title';
  title.textContent = 'Unable to load portals';

  const body = document.createElement('p');
  body.className = 'portal-card__empty-body';
  const raw = state.error && state.error.message
    ? state.error.message
    : 'The portal health endpoint is not responding.';
  body.textContent = raw;

  card.appendChild(title);
  card.appendChild(body);
  host.appendChild(card);
}

function renderList() {
  if (!listHost) return;
  listHost.innerHTML = '';

  if (state.loading && state.items.length === 0) {
    renderSkeletonCards(listHost, 4);
    return;
  }

  if (state.error && state.items.length === 0) {
    renderErrorState(listHost);
    return;
  }

  const visible = getVisibleItems();
  if (!visible.length) {
    renderEmptyState(listHost, !!state.filter);
    return;
  }

  for (const it of visible) {
    listHost.appendChild(createPortalCard(it));
  }
}

function render() {
  renderStatus();
  renderList();
}

function tickAgo() {
  if (!listHost) return;
  const cards = listHost.querySelectorAll('.portal-card[data-checked-at]');
  for (let i = 0; i < cards.length; i += 1) {
    const card = cards[i];
    const statusEl = card.querySelector('.portal-card__status');
    const iso = card.getAttribute('data-checked-at');
    if (!statusEl || !iso) continue;
    const ok = card.getAttribute('data-ok') === '1';
    const ago = secondsAgo(iso);
    let text = ok ? 'Healthy' : 'Unhealthy';
    if (ago != null) {
      text += ' - ' + ago + 's ago';
    }
    statusEl.textContent = text;
    const stale = ago == null || ago >= 60;
    card.classList.toggle('portal-card--stale', stale);
  }
}

async function refresh(options) {
  const opts = options || {};

  if (!supportsAbortController && refreshInFlight) {
    return;
  }

  if (supportsAbortController && refreshController) {
    refreshController.abort();
  }

  const controller = supportsAbortController ? new AbortController() : null;
  refreshController = controller;

  state.loading = true;
  state.error = null;
  refreshInFlight = true;

  if (!opts.silent) {
    render();
  } else {
    renderStatus();
  }

  try {
    const list = await getHealth(controller ? controller.signal : undefined);
    state.items = normalizeList(list);
    state.lastUpdated = new Date();
  } catch (err) {
    if (controller && err && err.name === 'AbortError') {
      return;
    }
    console.error('Health fetch failed', err);
    state.error = err instanceof Error ? err : new Error('Health fetch failed');
  } finally {
    if (!controller || refreshController === controller) {
      refreshController = null;
      state.loading = false;
      refreshInFlight = false;
      render();
    }
  }
}

async function addPortal(url) {
  const value = (url || '').trim();
  if (!value) return;

  const response = await fetch('/api/sites', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: value }),
  });

  if (!response.ok) {
    let text = '';
    try {
      text = await response.text();
    } catch {
      // ignore
    }
    const extra = text ? ' - ' + text : '';
    throw new Error(
      response.status + ' ' + response.statusText + extra,
    );
  }
}

if (refreshButton) {
  refreshButton.addEventListener('click', function () {
    refresh({ silent: false });
  });
}

if (portalAddButton && portalInput) {
  portalAddButton.addEventListener('click', async function () {
    const value = portalInput.value;
    if (!value) {
      setStatus('Enter a portal URL before registering.', 'error');
      return;
    }

    portalAddButton.disabled = true;
    try {
      state.error = null;
      setStatus('Registering portal...', 'info');
      await addPortal(value);
      portalInput.value = '';
      setStatus('Portal registered. Refreshing list.', 'info');
      await refresh({ silent: false });
    } catch (err) {
      console.error('Failed to register portal', err);
      state.error = err instanceof Error
        ? err
        : new Error('Failed to register portal');
      const message = state.error.message || 'Failed to register portal.';
      setStatus(message, 'error');
    } finally {
      portalAddButton.disabled = false;
    }
  });

  portalInput.addEventListener('keydown', function (event) {
    if (event.key === 'Enter') {
      event.preventDefault();
      portalAddButton.click();
    }
  });
}

if (searchBox) {
  searchBox.addEventListener('input', function () {
    state.filter = searchBox.value || '';
    renderList();
  });
}

refresh({ silent: false });
setInterval(tickAgo, 1000);
setInterval(function () {
  refresh({ silent: true });
}, 5000);
