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

	// Dump export: returns JSON object { "nickname": [urls...] }
	r.Get("/dump.json", func(w http.ResponseWriter, r *http.Request) {
		ch := make(chan [][]byte, 1)
		hub.snapshotReq <- ch
		history := <-ch
		d := BuildDumpFromHistory(history)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(d)
	})

	// Dump import: accepts the same JSON and broadcasts adds
	r.Post("/dump/import", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		d, err := ParseDump(r.Body)
		if err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		added := 0
		now := time.Now().UnixMilli()
		for nick, arr := range d {
			for _, url := range arr {
				msg := map[string]interface{}{"t": "ytq-add", "url": url, "by": nick, "ts": now}
				b, _ := json.Marshal(msg)
				hub.broadcast <- b
				added++
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]int{"added": added})
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
  <title>YouTube Chat</title>
  <style>
    :root{
      --chrome:#f3f4f6; --chrome-line:#d1d5db; --panel:#ffffff; --ink:#111827; --muted:#6b7280;
      --btn:#f9fafb; --btn-line:#d1d5db; --btn-hover:#eef2f7; --accent:#2563eb;
    }
    *{ box-sizing:border-box }
    body{ margin:0; background:var(--chrome); color:var(--ink); font-family:system-ui, -apple-system, Segoe UI, Roboto, sans-serif }
    .wrap{ margin:0 auto; padding:16px }
    .title{ font-weight:800; margin:0 0 8px 0; font-size:14px; color:var(--muted) }
    .btn{ background:var(--btn); border:1px solid var(--btn-line); border-radius:8px; padding:10px 12px; cursor:pointer; font-weight:700; color:#111; min-width:44px; min-height:38px; text-align:center }
    .btn:hover{ background:var(--btn-hover) }
    .input{ border:1px solid var(--btn-line); border-radius:8px; padding:10px 12px; background:#fff; min-height:38px; min-width:220px }
    .app-title{ font-weight:800; font-size:28px; margin:0 0 6px 0 }
    .main{ margin-top:16px; display:grid; grid-template-columns: 2fr 1fr; grid-template-areas: 'player chat'; gap:16px }
    .col-player{ grid-area: player }
    .col-chat{ grid-area: chat; min-height:0; overflow:hidden }
    .panel{ background:var(--panel); border:1px solid var(--chrome-line); border-radius:10px; padding:10px; display:flex; flex-direction:column }
    .player{ position:relative; width:100%; aspect-ratio:16/9; background:#000; border:1px solid var(--chrome-line); border-radius:8px; overflow:hidden }
    .status{ margin-top:8px; color:var(--muted); font-size:12px }
    .queue{ margin-top:6px }
    .queue-list{ border:1px solid var(--chrome-line); border-radius:8px; padding:8px; background:#fafafa; min-height:56px; max-height:240px; overflow:auto }
    .queue-tools{ margin-top:4px; margin-bottom:4px; font-size:12px; color:var(--muted); text-align:right }
    .link{ color:var(--accent); text-decoration:underline; cursor:pointer }
    .queue-item{ display:flex; align-items:center; justify-content:space-between; gap:8px; padding:6px 4px; border-bottom:1px dashed #e5e7eb; cursor:pointer }
    .queue-item:last-child{ border-bottom:none }
    .queue-item .meta{ color:var(--muted); font-size:12px }
    .queue-item.active{ background:#eef2ff }
    .btn-icon{ background:var(--btn); border:1px solid var(--btn-line); border-radius:6px; padding:4px 8px; cursor:pointer; }
    .chat{ display:flex; flex-direction:column; height:100% }
    .chat-head{ display:flex; gap:12px; align-items:center; margin-bottom:10px }
    .chat-log{ flex:1 1 auto; min-height:0; overflow:auto; border:1px solid var(--chrome-line); border-radius:8px; padding:8px; background:#fafafa }
    .chat-msg{ margin:0 0 6px 0; line-height:1.35 }
    .chat-msg .who{ font-weight:700 }
    .chat-msg .time{ color:var(--muted); font-size:11px; margin-left:6px }
    .chat-input{ display:flex; gap:12px; margin-top:10px }
    .chat-input input{ flex:1 }
    .yt-add{ margin-top:12px }
    .yt-row{ display:flex; gap:12px; align-items:center; flex-wrap:wrap }
    .yt-actions-right{ margin-left:auto; display:flex; gap:12px; align-items:center }
    .queue-empty{ color:var(--muted); font-size:13px; padding:8px; text-align:center }
    @media (max-width: 900px){
      .main{ grid-template-columns:1fr; grid-template-areas: 'player' 'chat' }
      .btn{ min-height:42px }
      .input{ min-height:42px }
    }
  </style>
  </head>
<body>
  <div class="wrap">
    <div class="app-title">YouTube Chat</div>

    <div class="main">
      <div class="panel col-player" style="width: 1500px; height: 1080px;">
        <div id="ytPlayer" class="player"></div>
        <div id="ytStatus" class="status"></div>
        <div class="yt-add">
          <div class="yt-row">
            <input id="ytUrl" class="input" type="url" placeholder="https://youtu.be/… 또는 https://www.youtube.com/watch?v=…" />
            <button class="btn" id="ytPlay" title="Broadcast and play">Play ▶</button>
            <div class="yt-actions-right">
              <button id="qPrev" class="btn" title="이전 곡">Prev ◀</button>
              <button id="qNext" class="btn" title="다음 곡">Next ▶</button>
              <button id="qClear" class="btn" title="목록 비우기">Clear</button>
            </div>
          </div>
        </div>
        <div class="queue">
          <div class="queue-tools">
            <a id="qExportLink" class="link" title="URL 목록을 JSON으로 다운로드">Export JSON</a>
            <span style="margin:0 6px; color:#d1d5db">|</span>
            <a id="qImportLink" class="link" title="JSON 파일에서 URL 목록 업로드">Import JSON</a>
            <input id="qImportFile" type="file" accept="application/json,.json" style="display:none" />
          </div>
          <div id="qList" class="queue-list"></div>
        </div>
      </div>
      <div class="panel chat col-chat">
        <div class="chat-head">
          <div class="title" style="margin:0">채팅</div>
          <div style="flex:1"></div>
          <input id="nick" class="input" type="text" placeholder="anon" style="min-width:140px" />
          <button id="roll" class="btn" title="랜덤 닉네임">🎲</button>
        </div>
        <div id="log" class="chat-log"></div>
        <div class="chat-input">
          <input id="msg" class="input" type="text" placeholder="메시지 입력 후 Enter" />
          <button id="send" class="btn">Send</button>
        </div>
      </div>
    </div>
  </div>

  <!-- YouTube IFrame API Loader -->
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
    const qList = document.getElementById('qList');
    const qPrev = document.getElementById('qPrev');
    const qNext = document.getElementById('qNext');
    const qClear = document.getElementById('qClear');
    const qExport = document.getElementById('qExportLink');
    const qImport = document.getElementById('qImportLink');
    const qImportFile = document.getElementById('qImportFile');

    // Constrain chat panel to left panel height; scroll inside chat-log
    try {
      const leftPanel = document.querySelector('.col-player');
      const rightPanel = document.querySelector('.col-chat');
      if (leftPanel && rightPanel && 'ResizeObserver' in window) {
        const sync = () => {
          rightPanel.style.maxHeight = leftPanel.getBoundingClientRect().height + 'px';
        };
        const ro = new ResizeObserver(sync);
        ro.observe(leftPanel);
        sync();
        window.addEventListener('resize', sync);
      }
    } catch (_) {}

    // Nickname persistence
    const stored = localStorage.getItem('nick');
    if(stored) nickEl.value = stored;
    function saveNick(){ try{ localStorage.setItem('nick', (nickEl.value||'anon').trim()); }catch(_){} }
    nickEl.addEventListener('change', saveNick);
    rollBtn.addEventListener('click', ()=>{ nickEl.value = randomNick(); saveNick(); nickEl.focus(); });

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
    function sendChat(text){ const name = (nickEl.value||'anon').trim() || 'anon'; send({t:'chat', name, text, ts: Date.now()}); }
    function sendQAdd(url, id){ const by=(nickEl.value||'anon').trim()||'anon'; send({t:'ytq-add', url, id, by, ts: Date.now()}); }
    function sendQAddBy(url, id, by){ by=(by||'anon').trim()||'anon'; send({t:'ytq-add', url, id, by, ts: Date.now()}); }
    function sendQClear(){ send({t:'ytq-clear', ts: Date.now()}); }
    function sendQDel(idx, id){ send({t:'ytq-del', idx, id, ts: Date.now()}); }
    // server import helper
    async function uploadJSONText(text){
      try{
        const res = await fetch('dump/import', {method:'POST', headers:{'Content-Type':'application/json'}, body:text});
        if(!res.ok){ throw new Error('upload failed'); }
        return true;
      }catch(_){ alert('업로드에 실패했습니다.'); return false; }
    }

    // --- YouTube player state ---
    let ytPlayerObj = null;
    let ytMountId = null;

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
      ytStatus.innerHTML = 'URL: <a target="_blank" rel="noopener" href="' + (original?original:'#') + '">' + (original||'') + '</a>';

      YT_API_READY.then(()=>{
        try { if (ytPlayerObj) { ytPlayerObj.destroy(); ytPlayerObj = null; } } catch(_){}
        ytPlayer.innerHTML = '';
        ytMountId = 'ytMount_' + Date.now();
        const mount = document.createElement('div');
        mount.id = ytMountId;
        mount.style.width = '100%';
        mount.style.height = '100%';
        ytPlayer.appendChild(mount);

        ytPlayerObj = new YT.Player(ytMountId, {
          width: '100%',
          height: '100%',
          videoId: id,
          playerVars: { autoplay: 1, rel: 0, playsinline: 1 },
          events: {
            onReady: (e) => { try { e.target.playVideo(); } catch(_){} },
            onStateChange: (e) => {
              // ✅ 자동재생: 처음(위, 0) → 아래(1,2,...)로 진행
              if (e.data === YT.PlayerState.ENDED) {
                if (currentIdx + 1 < playlist.length) {
                  playIndex(currentIdx + 1);
                }
              }
            }
          }
        });
      });
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

    // Queue state (거꾸로 정렬: 최신이 위)
    const playlist = [];
    let currentIdx = -1;

    let startTimer = null;
    function scheduleStartFromTop(){
      if (startTimer) clearTimeout(startTimer);
      startTimer = setTimeout(() => {
        if (currentIdx === -1 && playlist.length > 0) {
          playIndex(0); // 최신(맨 위)부터 시작
        }
      }, 120);
    }

    function renderQueue(){
      qList.innerHTML = '';
      if (playlist.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'queue-empty';
        empty.textContent = 'No items yet — please add a YouTube URL.';
        qList.appendChild(empty);
        return;
      }
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

    function playIndex(i){
      if(i<0 || i>=playlist.length) return;
      currentIdx = i;
      const it = playlist[i];
      showYouTube(it.id, it.url);
      renderQueue();
    }

    // Receive
    sock.onmessage = (ev)=>{
      try{
        const m = JSON.parse(ev.data);
        switch(m.t){
          case 'yt': {
            showYouTube(m.id || parseYouTubeId(m.url||''), m.url||'');
            break;
          }
          case 'ytq-add': {
            const id = m.id || parseYouTubeId(m.url||'');
            if(!id) break;

            // 거꾸로 정렬: 새 항목을 앞에 추가(최신이 위)
            playlist.unshift({ id, url: m.url||'', by: m.by||'anon', ts: m.ts||Date.now() });

            // 재생 중이면 현재 곡 유지(앞에 끼어들어 +1 보정)
            if (currentIdx !== -1) currentIdx++;

            renderQueue();

            // 초기 진입: 히스토리 수신 종료 후 한 번만 맨 위에서 시작
            scheduleStartFromTop();
            break;
          }
          case 'ytq-clear': {
            playlist.length = 0; currentIdx = -1; renderQueue(); ytPlayer.innerHTML=''; ytStatus.textContent='';
            break;
          }
          case 'ytq-del': {
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
          }
          case 'chat': {
            addMsg(m.name||'익명', m.text||'', m.ts||Date.now());
            break;
          }
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

    qPrev.addEventListener('click', ()=>{ if(currentIdx - 1 >= 0){ playIndex(currentIdx - 1); } });
    qNext.addEventListener('click', ()=>{ if(currentIdx + 1 < playlist.length){ playIndex(currentIdx + 1); } });
    qClear.addEventListener('click', ()=>{ if(confirm('재생 목록을 모두 비울까요?')) sendQClear(); });

    // Export/Import JSON via server endpoints (format: { "nickname": [urls...] })
    qExport.addEventListener('click', ()=>{
      fetch('dump.json').then(r=>{
        if(!r.ok) throw new Error('download error');
        return r.text();
      }).then(text=>{
        const blob = new Blob([text], {type:'application/json'});
        const a = document.createElement('a');
        const ts = new Date(); const pad=(n)=>String(n).padStart(2,'0');
        const name = 'youtube-urls-' + ts.getFullYear() + pad(ts.getMonth()+1) + pad(ts.getDate()) + '-' + pad(ts.getHours()) + pad(ts.getMinutes()) + pad(ts.getSeconds()) + '.json';
        a.href = URL.createObjectURL(blob); a.download = name; document.body.appendChild(a); a.click(); URL.revokeObjectURL(a.href); a.remove();
      }).catch(()=>{ alert('내보내기에 실패했습니다.'); });
    });

    qImport.addEventListener('click', ()=>{ qImportFile.click(); });
    qImportFile.addEventListener('change', ()=>{
      const f = qImportFile.files && qImportFile.files[0];
      if(!f) return;
      const reader = new FileReader();
      reader.onload = ()=>{
        try{
          const text = String(reader.result||'');
          let obj = JSON.parse(text);
          if (!obj || typeof obj !== 'object' || Array.isArray(obj)) { throw new Error('object expected'); }
          // Flatten {nick: [urls...], ...]
          const pairs = [];
          for (const [nick, arr] of Object.entries(obj)){
            if (!Array.isArray(arr)) continue;
            const urls = arr.map(v=> typeof v==='string'? v : (v&&typeof v.url==='string'? v.url: '')).filter(Boolean);
            for(const u of urls){ pairs.push([nick, u]); }
          }
          if (!pairs.length) { alert('유효한 URL이 없습니다.'); return; }
          uploadJSONText(text);
        }catch(e){
          alert('가져오기에 실패했습니다. 유효한 JSON 파일인지 확인하세요.');
        } finally {
          qImportFile.value = '';
        }
      };
      reader.onerror = ()=>{ alert('파일을 읽지 못했습니다.'); qImportFile.value=''; };
      reader.readAsText(f);
    });

    // Responsive layout only: size/ratio controls removed
  })();
  </script>
</body>
</html>`))
