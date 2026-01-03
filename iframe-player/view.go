package main

import (
	"html/template"
	"net/http"
)

// NewHandler serves the iframe playlist viewer UI.
func NewHandler(name string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Name string
		}{Name: name}
		_ = indexPage.Execute(w, data)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

var indexPage = template.Must(template.New("iframe").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.Name}}</title>
  <style>
    :root{
      --bg:#0b0f14; --panel:#111827; --border:#1f2937; --fg:#e5e7eb; --muted:#9ca3af;
      --accent:#38bdf8; --accent-2:#22c55e; --danger:#ef4444;
    }
    *{ box-sizing:border-box }
    body{ margin:0; background:radial-gradient(1200px 600px at 20% -10%, #10223a 0%, #0b0f14 55%); color:var(--fg);
      font-family:'Space Grotesk','Segoe UI',system-ui,-apple-system,sans-serif; }
    .wrap{ max-width:1400px; margin:0 auto; padding:24px; }
    .head{ display:flex; align-items:flex-end; gap:16px; }
    .title{ font-size:28px; font-weight:700; letter-spacing:-0.02em; }
    .sub{ color:var(--muted); font-size:13px; }
    .main{ margin-top:18px; display:grid; grid-template-columns:1fr; gap:16px; }
    .panel{ background:var(--panel); border:1px solid var(--border); border-radius:12px; padding:14px; display:flex; flex-direction:column; }
    .screen{ width:100%; aspect-ratio:16/9; background:#000; border:1px solid var(--border); border-radius:10px; overflow:hidden; }
    iframe{ width:100%; height:100%; border:0; }
    .status{ margin-top:10px; color:var(--muted); font-size:12px; word-break:break-all; }
    .controls{ margin-top:12px; display:flex; flex-wrap:wrap; gap:10px; align-items:center; }
    .btn{ background:#0b1220; border:1px solid var(--border); color:var(--fg); border-radius:8px; padding:8px 12px; cursor:pointer; }
    .btn:hover{ border-color:var(--accent); }
    .btn-danger:hover{ border-color:var(--danger); }
    .field{ display:flex; align-items:center; gap:6px; color:var(--muted); font-size:12px; }
    .field input[type="number"]{ width:90px; }
    .input, textarea{ background:#0b1220; border:1px solid var(--border); color:var(--fg); border-radius:8px; padding:10px; }
    .input-row{ display:grid; grid-template-columns: 1fr auto; gap:10px; align-items:center; }
    .url-input{ width:100%; }
    .queue{ margin-top:12px; border:1px solid var(--border); border-radius:10px; background:#0b1220; overflow:auto; min-height:120px; max-height:360px; }
    .queue-item{ display:flex; align-items:center; justify-content:space-between; gap:10px; padding:8px 10px; border-bottom:1px dashed #1f2a37; cursor:pointer; }
    .queue-item:last-child{ border-bottom:none; }
    .queue-item.active{ background:#0b1a24; }
    .queue-empty{ color:var(--muted); font-size:13px; padding:14px; text-align:center; }
    .meta{ color:var(--muted); font-size:11px; white-space:nowrap; display:flex; gap:8px; align-items:center; }
    .url{ word-break:break-all; font-size:12px; }
    .row{ display:flex; gap:10px; flex-wrap:wrap; }
    .btn-icon{ background:transparent; border:1px solid var(--border); color:var(--fg); border-radius:6px; padding:2px 6px; cursor:pointer; font-size:12px; }
    .btn-icon:hover{ border-color:var(--danger); color:var(--danger); }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="head">
      <div class="title">{{.Name}}</div>
    </div>

    <div class="main">
      <div class="panel">
        <div id="screen" class="screen"></div>
        <div id="status" class="status">Ready.</div>
        <div class="controls">
          <button id="prev" class="btn" title="Previous item">Prev</button>
          <button id="next" class="btn" title="Next item">Next</button>
          <div class="field">
            <span>Auto next (sec)</span>
            <input id="autoNext" class="input" type="number" min="0" step="1" value="0" />
          </div>
          <label class="field">
            <input id="loop" type="checkbox" />
            <span>Loop</span>
          </label>
          <label class="field">
            <input id="addAutoplay" type="checkbox" checked />
            <span>Add autoplay=1</span>
          </label>
        </div>
      </div>

      <div class="panel">
        <div class="input-row">
          <input id="urls" class="input url-input" type="text" placeholder="Paste URLs (comma or newline separated)" />
          <button id="append" class="btn">Add</button>
        </div>
      </div>
      <div class="panel">
        <div id="queue" class="queue"></div>
      </div>
    </div>
  </div>

  <script>
  // YouTube IFrame API loader (Promise)
  const YT_API_READY = new Promise((resolve) => {
    if (window.YT && window.YT.Player) return resolve();
    const tag = document.createElement('script');
    tag.src = "https://www.youtube.com/iframe_api";
    document.head.appendChild(tag);
    window.onYouTubeIframeAPIReady = () => resolve();
  });
  </script>

  <script>
  (function(){
    const screen = document.getElementById('screen');
    const status = document.getElementById('status');
    const queue = document.getElementById('queue');
    const urlsEl = document.getElementById('urls');
    const appendBtn = document.getElementById('append');
    const prevBtn = document.getElementById('prev');
    const nextBtn = document.getElementById('next');
    const autoNextEl = document.getElementById('autoNext');
    const loopEl = document.getElementById('loop');
    const addAutoplayEl = document.getElementById('addAutoplay');

    let playlist = [];
    let currentIdx = -1;
    let autoTimer = null;
    let ytPlayer = null;

    function parseInput(text){
      return text.split(/[\n,]+/).map(s => s.trim()).filter(Boolean);
    }

    function extractIframeSrc(raw){
      if (!raw) return null;
      const m = raw.match(/<iframe[^>]*\s+src\s*=\s*["']([^"']+)["'][^>]*>/i);
      if (m && m[1]) return m[1].trim();
      return null;
    }

    function normalizeUrl(raw){
      let u = raw.trim();
      if (!u) return null;
      if (u.startsWith('//')) u = 'https:' + u;
      if (!/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u)) {
        u = 'https://' + u;
      }
      try { new URL(u); } catch (e) { return null; }
      return u;
    }

    function toYouTubeEmbed(u){
      const id = parseYouTubeId(u);
      if (!id) return null;
      return makeYouTubeEmbed(id);
    }

    function parseYouTubeId(u){
      try{
        const url = new URL(u);
        if (url.hostname === 'youtu.be') {
          return url.pathname.split('/').filter(Boolean)[0] || null;
        }
        if (url.hostname.endsWith('youtube.com')) {
          if (url.pathname === '/watch') return url.searchParams.get('v');
          const parts = url.pathname.split('/').filter(Boolean);
          const i = parts.indexOf('embed'); if (i >= 0 && parts[i+1]) return parts[i+1];
          if (parts[0] === 'shorts' && parts[1]) return parts[1];
        }
      } catch (e) {}
      return null;
    }

    function makeYouTubeEmbed(id){
      const autoplay = addAutoplayEl.checked ? '1' : '0';
      return 'https://www.youtube.com/embed/' + id + '?autoplay=' + autoplay + '&rel=0&playsinline=1';
    }

    function withAutoplay(u){
      try{
        const url = new URL(u);
        if (addAutoplayEl.checked) {
          url.searchParams.set('autoplay', '1');
        } else {
          url.searchParams.delete('autoplay');
        }
        return url.toString();
      }catch(e){
        return u;
      }
    }

    function normalizeForIframe(raw){
      const iframeSrc = extractIframeSrc(raw);
      const base = normalizeUrl(iframeSrc || raw);
      if (!base) return null;
      const ytId = parseYouTubeId(base);
      if (ytId) {
        return { raw, url: makeYouTubeEmbed(ytId), type: 'youtube', ytId };
      }
      return { raw, url: withAutoplay(base), type: 'iframe', ytId: '' };
    }

    function clearScreen(){
      if (ytPlayer) {
        try { ytPlayer.destroy(); } catch (e) {}
        ytPlayer = null;
      }
      screen.innerHTML = '';
    }

    function showIframe(url){
      clearScreen();
      const iframe = document.createElement('iframe');
      iframe.src = withAutoplay(url);
      iframe.allow = 'autoplay; fullscreen; picture-in-picture; encrypted-media';
      screen.appendChild(iframe);
    }

    function handleEnded(){
      if (autoTimer) { clearTimeout(autoTimer); autoTimer = null; }
      const ni = getNextIndex();
      if (ni >= 0) playIndex(ni);
    }

    function showYouTube(id){
      clearScreen();
      const mountId = 'yt_mount_' + Date.now();
      const mount = document.createElement('div');
      mount.id = mountId;
      mount.style.width = '100%';
      mount.style.height = '100%';
      screen.appendChild(mount);

      YT_API_READY.then(() => {
        ytPlayer = new YT.Player(mountId, {
          width: '100%',
          height: '100%',
          videoId: id,
          playerVars: { autoplay: addAutoplayEl.checked ? 1 : 0, rel: 0, playsinline: 1, enablejsapi: 1 },
          events: {
            onReady: (e) => { if (addAutoplayEl.checked) { try { e.target.playVideo(); } catch (e) {} } },
            onStateChange: (e) => {
              if (e.data === YT.PlayerState.ENDED) {
                handleEnded();
              }
            }
          }
        });
      });
    }

    function renderQueue(){
      queue.innerHTML = '';
      if (playlist.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'queue-empty';
        empty.textContent = 'No items yet.';
        queue.appendChild(empty);
        return;
      }
      playlist.forEach((it, i) => {
        const row = document.createElement('div');
        row.className = 'queue-item' + (i === currentIdx ? ' active' : '');
        const left = document.createElement('div');
        left.className = 'url';
        left.textContent = it.raw;
        const right = document.createElement('div');
        right.className = 'meta';
        const idx = document.createElement('span');
        idx.textContent = String(i + 1);
        const del = document.createElement('button');
        del.className = 'btn-icon';
        del.title = 'Remove';
        del.textContent = 'x';
        del.addEventListener('click', (e) => {
          e.stopPropagation();
          removeIndex(i);
        });
        right.appendChild(idx);
        right.appendChild(del);
        row.appendChild(left);
        row.appendChild(right);
        row.addEventListener('click', () => playIndex(i));
        queue.appendChild(row);
      });
    }

    function updateStatus(){
      if (currentIdx < 0 || currentIdx >= playlist.length) {
        status.textContent = 'Ready.';
        return;
      }
      status.textContent = 'Playing ' + String(currentIdx + 1) + '/' + String(playlist.length) + ': ' + playlist[currentIdx].raw;
    }

    function scheduleAutoNext(){
      if (autoTimer) { clearTimeout(autoTimer); autoTimer = null; }
      const sec = parseInt(autoNextEl.value, 10);
      if (!sec || sec <= 0) return;
      autoTimer = setTimeout(() => {
        const ni = getNextIndex();
        if (ni >= 0) playIndex(ni);
      }, sec * 1000);
    }

    function getNextIndex(){
      if (playlist.length === 0) return -1;
      if (currentIdx + 1 < playlist.length) return currentIdx + 1;
      if (loopEl.checked) return 0;
      return -1;
    }

    function playIndex(i){
      if (i < 0 || i >= playlist.length) return;
      currentIdx = i;
      const it = playlist[i];
      if (it.type === 'youtube' && it.ytId) {
        showYouTube(it.ytId);
      } else {
        showIframe(it.url);
      }
      updateStatus();
      renderQueue();
      scheduleAutoNext();
    }

    function removeIndex(i){
      if (i < 0 || i >= playlist.length) return;
      const wasCurrent = (i === currentIdx);
      playlist.splice(i, 1);
      if (playlist.length === 0) {
        currentIdx = -1;
        clearScreen();
      } else if (i < currentIdx) {
        currentIdx--;
      } else if (wasCurrent) {
        if (currentIdx >= playlist.length) currentIdx = playlist.length - 1;
        playIndex(currentIdx);
        return;
      }
      updateStatus();
      renderQueue();
    }

    function loadList(replace){
      const rawList = parseInput(urlsEl.value);
      const items = rawList.map(raw => normalizeForIframe(raw)).filter(Boolean);
      if (replace) {
        playlist = items;
        currentIdx = -1;
      } else {
        playlist = playlist.concat(items);
      }
      renderQueue();
      if (playlist.length > 0 && currentIdx === -1) {
        playIndex(0);
      }
    }

    appendBtn.addEventListener('click', () => { loadList(false); urlsEl.value = ''; });
    prevBtn.addEventListener('click', () => {
      if (playlist.length === 0) return;
      const prev = currentIdx - 1;
      if (prev >= 0) playIndex(prev);
      else if (loopEl.checked) playIndex(playlist.length - 1);
    });
    nextBtn.addEventListener('click', () => {
      const ni = getNextIndex();
      if (ni >= 0) playIndex(ni);
    });
    autoNextEl.addEventListener('change', scheduleAutoNext);
    loopEl.addEventListener('change', scheduleAutoNext);
    addAutoplayEl.addEventListener('change', () => {
      if (currentIdx >= 0) playIndex(currentIdx);
    });

    renderQueue();
  })();
  </script>
</body>
</html>`))
