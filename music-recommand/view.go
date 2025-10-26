package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// drawHub manages active websocket clients and broadcasts messages to them.
type drawHub struct {
	clients    map[*wsClient]bool
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *wsClient
	// history for late joiners
	history     [][]byte
	maxHistory  int
	snapshotReq chan chan [][]byte
}

func newDrawHub() *drawHub {
	h := &drawHub{
		clients:     make(map[*wsClient]bool),
		broadcast:   make(chan []byte, 128),
		register:    make(chan *wsClient, 32),
		unregister:  make(chan *wsClient, 32),
		history:     make([][]byte, 0, 256),
		maxHistory:  5000,
		snapshotReq: make(chan chan [][]byte, 8),
	}
	go h.run()
	return h
}

func (h *drawHub) run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
		case ch := <-h.snapshotReq:
			// reply with a shallow copy of history
			snap := make([][]byte, len(h.history))
			copy(snap, h.history)
			ch <- snap
		case msg := <-h.broadcast:
			// update history (respect clear)
			var peek struct {
				T string `json:"t"`
			}
			if err := json.Unmarshal(msg, &peek); err == nil && peek.T == "clear" {
				h.history = h.history[:0]
			} else {
				if len(h.history) >= h.maxHistory {
					copy(h.history[0:], h.history[1:])
					h.history[len(h.history)-1] = nil
					h.history = h.history[:len(h.history)-1]
				}
				b := make([]byte, len(msg))
				copy(b, msg)
				h.history = append(h.history, b)
			}
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					delete(h.clients, c)
					close(c.send)
				}
			}
		}
	}
}

type wsClient struct {
	hub  *drawHub
	conn *websocket.Conn
	send chan []byte
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (c *wsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(1 << 20)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.hub.broadcast <- message
	}
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// NewHandler sets up HTTP routes and serves the chat + YouTube UI and websocket endpoint.
func NewHandler(name string, hub *drawHub) http.Handler {
	r := chi.NewRouter()

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Name string
		}{Name: name}
		_ = indexPage.Execute(w, data)
	})

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &wsClient{hub: hub, conn: conn, send: make(chan []byte, 64)}
		hub.register <- client
		go client.writePump()

		// Replay history to new client so late joiners see prior messages.
		go func(c *wsClient) {
			ch := make(chan [][]byte, 1)
			hub.snapshotReq <- ch
			backlog := <-ch
			for _, m := range backlog {
				c.send <- m
			}
		}(client)

		client.readPump()
	})

	return r
}

var indexPage = template.Must(template.New("paint").Parse(`<!doctype html>
<html lang="ko">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>RelayDNS — YouTube Chat</title>
  <style>
    :root{
      --chrome:#f3f4f6; --chrome-line:#d1d5db; --panel:#ffffff; --ink:#111827; --muted:#6b7280;
      --btn:#f9fafb; --btn-line:#d1d5db; --btn-hover:#eef2f7; --accent:#2563eb;
    }
    *{ box-sizing:border-box }
    body{ margin:0; background:var(--chrome); color:var(--ink); font-family:system-ui, -apple-system, Segoe UI, Roboto, sans-serif }
    .wrap{ max-width:1100px; margin:0 auto; padding:16px }
    .ribbon{ background:var(--panel); border:1px solid var(--chrome-line); border-radius:10px; padding:10px; box-shadow:0 1px 0 rgba(0,0,0,0.03) }
    .row{ display:flex; gap:12px; align-items:stretch; flex-wrap:wrap }
    .group{ border-right:1px solid var(--chrome-line); padding-right:12px; margin-right:12px; display:flex; gap:8px; align-items:center; flex-wrap:wrap }
    .group:last-child{ border-right:none; margin-right:0; padding-right:0 }
    .title{ font-weight:800; margin:0 0 8px 0; font-size:14px; color:var(--muted) }
    .btn{ background:var(--btn); border:1px solid var(--btn-line); border-radius:8px; padding:10px 12px; cursor:pointer; font-weight:700; color:#111; min-width:44px; min-height:38px; text-align:center }
    .btn:hover{ background:var(--btn-hover) }
    .input{ border:1px solid var(--btn-line); border-radius:8px; padding:10px 12px; background:#fff; min-height:38px; min-width:220px }
    .main{ margin-top:12px; display:grid; grid-template-columns: 2fr 1fr; grid-template-areas: 'player chat'; gap:12px }
    .col-player{ grid-area: player }
    .col-chat{ grid-area: chat }
    .panel{ background:var(--panel); border:1px solid var(--chrome-line); border-radius:10px; padding:10px }
    .player{ position:relative; width:100%; aspect-ratio:16/9; background:#000; border:1px solid var(--chrome-line); border-radius:8px; overflow:hidden }
    .status{ margin-top:8px; color:var(--muted); font-size:12px }
    .queue{ margin-top:8px }
    .queue-head{ display:flex; align-items:center; justify-content:space-between; gap:8px; margin-bottom:6px }
    .queue-actions{ display:flex; gap:8px; align-items:center }
    .queue-list{ border:1px solid var(--chrome-line); border-radius:8px; padding:8px; background:#fafafa; max-height:220px; overflow:auto }
    .queue-item{ display:flex; align-items:center; justify-content:space-between; gap:8px; padding:6px 4px; border-bottom:1px dashed #e5e7eb; cursor:pointer }
    .queue-item:last-child{ border-bottom:none }
    .queue-item .meta{ color:var(--muted); font-size:12px }
    .queue-item.active{ background:#eef2ff }
    .btn-icon{ background:var(--btn); border:1px solid var(--btn-line); border-radius:6px; padding:4px 8px; cursor:pointer; }
    .chat{ display:flex; flex-direction:column; height:100% }
    .chat-head{ display:flex; gap:8px; align-items:center; margin-bottom:8px }
    .chat-log{ flex:1; overflow:auto; border:1px solid var(--chrome-line); border-radius:8px; padding:8px; background:#fafafa; max-height:60vh }
    .chat-msg{ margin:0 0 6px 0; line-height:1.35 }
    .chat-msg .who{ font-weight:700 }
    .chat-msg .time{ color:var(--muted); font-size:11px; margin-left:6px }
    .chat-input{ display:flex; gap:8px; margin-top:8px }
    .chat-input input{ flex:1 }
    @media (max-width: 900px){
      .main{ grid-template-columns:1fr; grid-template-areas: 'player' 'chat' }
      .btn{ min-height:42px }
      .input{ min-height:42px }
      .chat-log{ height: 50vh }
    }
  </style>
  </head>
<body>
  <div class="wrap">
    <div class="ribbon">
      <div class="row">
        <div class="group">
          <div class="title">YouTube</div>
          <input id="ytUrl" class="input" type="url" placeholder="https://youtu.be/… 또는 https://www.youtube.com/watch?v=…" />
          <button class="btn" id="ytPlay" title="Broadcast and play">Play ▶</button>
        </div>
        <div class="group">
          <div class="title">Player</div>
          <label for="size" style="color:var(--muted); font-size:12px">Size</label>
          <input id="size" type="range" min="1" max="4" value="2" step="1" />
          <label for="ratio" style="color:var(--muted); font-size:12px; margin-left:8px">Ratio</label>
          <select id="ratio" class="input" style="min-width:120px">
            <option value="16/9" selected>16:9</option>
            <option value="4/3">4:3</option>
            <option value="1/1">1:1</option>
            <option value="21/9">21:9</option>
          </select>
        </div>
        <div class="group">
          <div class="title">닉네임</div>
          <input id="nick" class="input" type="text" placeholder="anon" style="min-width:140px" />
          <button id="roll" class="btn" title="랜덤 닉네임">🎲</button>
        </div>
      </div>
    </div>

    <div class="main">
      <div class="panel col-player">
        <div id="ytPlayer" class="player"></div>
        <div id="ytStatus" class="status"></div>
        <div class="queue">
          <div class="queue-head">
            <div class="title" style="margin:0">재생 목록</div>
            <div class="queue-actions">
              <button id="qNext" class="btn" title="다음 곡">Next ▶</button>
              <button id="qClear" class="btn" title="목록 비우기">Clear</button>
            </div>
          </div>
          <div id="qList" class="queue-list"></div>
        </div>
      </div>
      <div class="panel chat col-chat">
        <div class="chat-head">
          <div class="title" style="margin:0">채팅</div>
        </div>
        <div id="log" class="chat-log"></div>
        <div class="chat-input">
          <input id="msg" class="input" type="text" placeholder="메시지 입력 후 Enter" />
          <button id="send" class="btn">Send</button>
        </div>
      </div>
    </div>

    <div class="status">RelayDNS — {{.Name}}</div>
  </div>

  <script>
  (function(){
    // Elements
    const ytUrl = document.getElementById('ytUrl');
    const ytPlay = document.getElementById('ytPlay');
    const ytPlayer = document.getElementById('ytPlayer');
    const ytStatus = document.getElementById('ytStatus');
    const nickEl = document.getElementById('nick');
    const rollBtn = document.getElementById('roll');
    const log = document.getElementById('log');
    const msgEl = document.getElementById('msg');
    const sendBtn = document.getElementById('send');
    const sizeEl = document.getElementById('size');
    const ratioEl = document.getElementById('ratio');
    const mainGrid = document.querySelector('.main');
    const qList = document.getElementById('qList');
    const qNext = document.getElementById('qNext');
    const qClear = document.getElementById('qClear');

    // Nickname persistence
    const stored = localStorage.getItem('relaydns_nick');
    if(stored) nickEl.value = stored;
    function saveNick(){ try{ localStorage.setItem('relaydns_nick', (nickEl.value||'anon').trim()); }catch(_){} }
    nickEl.addEventListener('change', saveNick);
    rollBtn.addEventListener('click', ()=>{ nickEl.value = randomNick(); saveNick(); nickEl.focus(); });

    // Nickname helper
    function randomNick(){
      const words = ['gopher','rust','unix','docker','kube','vim','nvim','git','linux','bsd','wasm','grpc','lambda','net','proto','dns','relay','p2p','ipfs','webrtc'];
      const w = words[Math.floor(Math.random()*words.length)];
      const n = String(Math.floor(1000+Math.random()*9000));
      return w + n;
    }

    // WS url helper (works behind /peer/<peerID>/)
    function wsURL(){
      const p = location.protocol==='https:'?'wss':'ws';
      const basePath = location.pathname.endsWith('/') ? location.pathname : (location.pathname + '/');
      return p + '://' + location.host + basePath + 'ws';
    }
    const sock = new WebSocket(wsURL());

    // Messaging
    function send(msg){ if(sock.readyState===1) sock.send(JSON.stringify(msg)); }
    function sendYT(url, id){ send({t:'yt', url, id, ts: Date.now()}); }
    function sendChat(text){ const name = (nickEl.value||'anon').trim() || 'anon'; send({t:'chat', name, text, ts: Date.now()}); }
    function sendQAdd(url, id){ const by=(nickEl.value||'anon').trim()||'anon'; send({t:'ytq-add', url, id, by, ts: Date.now()}); }
    function sendQClear(){ send({t:'ytq-clear', ts: Date.now()}); }
    function sendQDel(idx, id){ send({t:'ytq-del', idx, id, ts: Date.now()}); }

    // YouTube helpers
    function parseYouTubeId(u){
      try{
        const url = new URL(u);
        if(url.hostname === 'youtu.be'){
          return url.pathname.split('/').filter(Boolean)[0] || null;
        }
        if(url.hostname.endsWith('youtube.com')){
          if(url.pathname === '/watch') return url.searchParams.get('v');
          const parts = url.pathname.split('/').filter(Boolean);
          const i = parts.indexOf('embed'); if(i>=0 && parts[i+1]) return parts[i+1];
          if(parts[0]==='shorts' && parts[1]) return parts[1];
        }
        return null;
      }catch{ return null; }
    }
    function showYouTube(id, original){
      if(!id){ ytStatus.textContent = '잘못된 YouTube URL'; return; }
      ytPlayer.innerHTML = '';
      const iframe = document.createElement('iframe');
      iframe.width = '100%'; iframe.height = '100%';
      iframe.allow = 'autoplay; encrypted-media; picture-in-picture';
      iframe.referrerPolicy = 'strict-origin-when-cross-origin';
      iframe.allowFullscreen = true;
      iframe.src = 'https://www.youtube-nocookie.com/embed/' + encodeURIComponent(id) + '?autoplay=1&mute=0&rel=0&playsinline=1';
      ytPlayer.appendChild(iframe);
      ytStatus.innerHTML = 'URL: <a target="_blank" rel="noopener" href="' + (original?original:'#') + '">' + (original||'') + '</a> — 모든 클라이언트에서 재생됩니다 (초기에는 음소거됨).';
    }

    // Chat helpers
    function fmtTime(ts){ try{ const d = new Date(ts); return d.toLocaleTimeString([], {hour12:false, hour:'2-digit', minute:'2-digit', second:'2-digit'}); }catch{ return ''; } }
    const PALETTE = ['#60a5fa','#22c55e','#f59e0b','#ef4444','#a78bfa','#14b8a6','#eab308','#f472b6','#8b5cf6','#06b6d4','#34d399','#fb7185','#c084fc','#f97316','#84cc16','#10b981','#38bdf8','#f43f5e','#e879f9','#fde047','#93c5fd','#4ade80','#fca5a5','#a3e635','#67e8f9','#f0abfc','#fbbf24','#86efac'];
    function hashNick(s){ let h=0; for(let i=0;i<s.length;i++){ h=((h<<5)-h)+s.charCodeAt(i); h|=0; } return h>>>0; }
    function colorFor(nick){ return PALETTE[hashNick(nick||'anon') % PALETTE.length]; }
    function addMsg(name, text, ts){
      const p = document.createElement('p'); p.className = 'chat-msg';
      const who = document.createElement('span'); who.className='who'; who.textContent = name; who.style.color = colorFor(name);
      const time = document.createElement('span'); time.className='time'; time.textContent = fmtTime(ts);
      const body = document.createElement('span'); body.className='body'; body.textContent = ' — ' + text;
      p.appendChild(who); p.appendChild(time); p.appendChild(body);
      log.appendChild(p); log.scrollTop = log.scrollHeight;
    }

    // Queue state
    const playlist = [];
    let currentIdx = -1;
    function renderQueue(){
      qList.innerHTML = '';
      playlist.forEach((it, i)=>{
        const row = document.createElement('div'); row.className='queue-item' + (i===currentIdx?' active':'');
        const left = document.createElement('div'); left.innerHTML = (i===currentIdx?'▶ ':'') + '<strong>' + (it.id||'') + '</strong>';
        const right = document.createElement('div'); right.className='meta';
        const metaText = document.createElement('span'); metaText.textContent = (it.by||'') + ' • ' + new Date(it.ts||Date.now()).toLocaleTimeString();
        const del = document.createElement('button'); del.className='btn-icon'; del.title='삭제'; del.textContent='삭제';
        del.addEventListener('click', (e)=>{ e.stopPropagation(); sendQDel(i, it.id); });
        right.appendChild(metaText); right.appendChild(document.createTextNode(' ')); right.appendChild(del);
        row.appendChild(left); row.appendChild(right);
        row.addEventListener('click', ()=>{ playIndex(i); });
        qList.appendChild(row);
      });
    }
    function playIndex(i){ if(i<0 || i>=playlist.length) return; currentIdx = i; const it = playlist[i]; showYouTube(it.id, it.url); renderQueue(); }

    // Receive
    sock.onmessage = (ev)=>{
      try{ const m = JSON.parse(ev.data);
        switch(m.t){
          case 'yt': showYouTube(m.id || parseYouTubeId(m.url||''), m.url||''); break;
          case 'ytq-add':
            const id = m.id || parseYouTubeId(m.url||'');
            if(!id) break;
            playlist.push({ id, url: m.url||'', by: m.by||'anon', ts: m.ts||Date.now() });
            renderQueue();
            break;
          // Note: no shared playback position; ignore any external 'now/next' commands
          case 'ytq-clear':
            playlist.length = 0; currentIdx = -1; renderQueue(); ytPlayer.innerHTML=''; ytStatus.textContent='';
            break;
          case 'ytq-del':
            let di = (typeof m.idx === 'number') ? m.idx : -1;
            if (!(di >=0 && di < playlist.length) && m.id){ di = playlist.findIndex(it => it.id === m.id); }
            if (di >= 0 && di < playlist.length) {
              const wasCurrent = (di === currentIdx);
              playlist.splice(di, 1);
              if (di < currentIdx) {
                currentIdx--;
              } else if (wasCurrent) {
                if (currentIdx >= playlist.length) currentIdx = playlist.length - 1;
                if (currentIdx >= 0) {
                  const it = playlist[currentIdx]; showYouTube(it.id, it.url);
                } else {
                  ytPlayer.innerHTML=''; ytStatus.textContent='';
                }
              }
              renderQueue();
            }
            break;
          case 'chat': addMsg(m.name||'익명', m.text||'', m.ts||Date.now()); break;
        }
      }catch{}
    };

    // UI events
    ytPlay.addEventListener('click', ()=>{
      const u = ytUrl.value.trim();
      const id = parseYouTubeId(u);
      if(!id){ alert('유효한 YouTube URL을 입력하세요.'); return; }
      sendQAdd(u, id);
      ytUrl.value='';
    });
    ytUrl.addEventListener('keydown', (e)=>{ if(e.key==='Enter'){ ytPlay.click(); } });
    sendBtn.addEventListener('click', ()=>{ const t = msgEl.value.trim(); if(!t) return; sendChat(t); msgEl.value=''; setTimeout(()=>msgEl.focus(),0); });
    msgEl.setAttribute('enterkeyhint','send');
    msgEl.setAttribute('inputmode','text');
    msgEl.addEventListener('keydown', (e)=>{ if(e.isComposing||e.keyCode===229){return;} if(e.key==='Enter'){ e.preventDefault(); sendBtn.click(); } });

    qNext.addEventListener('click', ()=>{ if(currentIdx >= -1 && currentIdx+1 < playlist.length){ playIndex(currentIdx+1); } });
    qClear.addEventListener('click', ()=>{ if(confirm('재생 목록을 모두 비울까요?')) sendQClear(); });

    // Player size controls
    function isMobile(){ return window.matchMedia('(max-width: 900px)').matches; }
    function applySize(){
      const v = parseInt(sizeEl.value,10);
      const fr = Math.max(1, Math.min(4, v));
      if(!mainGrid) return;
      if(isMobile()){
        mainGrid.style.gridTemplateColumns = '1fr';
      } else {
        mainGrid.style.gridTemplateColumns = fr + 'fr 1fr';
      }
      try{ localStorage.setItem('ytchat.size', String(fr)); }catch(_){ }
    }
    function applyRatio(){
      const r = ratioEl.value;
      ytPlayer.style.aspectRatio = r;
      try{ localStorage.setItem('ytchat.ratio', r); }catch(_){ }
    }
    // Load persisted
    try{ const s = parseInt(localStorage.getItem('ytchat.size')||'2',10); if(s){ sizeEl.value=String(s); } }catch(_){ }
    try{ const rr = localStorage.getItem('ytchat.ratio')||'16/9'; ratioEl.value = rr; }catch(_){ }
    applySize(); applyRatio();
    sizeEl.addEventListener('input', applySize);
    // Re-apply responsive layout on viewport changes
    window.addEventListener('resize', ()=>{ applySize(); });
    ratioEl.addEventListener('change', applyRatio);
  })();
  </script>
</body>
</html>`))
