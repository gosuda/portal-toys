const logEl = document.getElementById('log');
const rosterEl = document.getElementById('roster');
const statusEl = document.getElementById('status');
const chatInput = document.getElementById('chat-text');
const chatBtn = document.getElementById('chat-send');
const connectForm = document.getElementById('connect-form');
const nicknameEl = document.getElementById('nickname');
const roomEl = document.getElementById('room');
const wsUrlEl = document.getElementById('ws-url');
const voteTargetEl = document.getElementById('vote-target');
const actionTargetEl = document.getElementById('action-target');
const controlButtons = document.querySelectorAll('#controls button');

let socket;

function log(message, author = 'system') {
  const entry = document.createElement('div');
  entry.className = 'log-entry';
  entry.innerHTML = `<strong>[${author}]</strong> ${message}`;
  logEl.appendChild(entry);
  logEl.scrollTop = logEl.scrollHeight;
}

function setStatus(text, level = 'neutral') {
  statusEl.textContent = text;
  statusEl.dataset.level = level;
}

function connect(evt) {
  evt.preventDefault();
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.close();
  }
  const nickname = nicknameEl.value.trim();
  const room = roomEl.value.trim();
  if (!nickname || !room) {
    alert('닉네임과 방 이름을 입력하세요.');
    return;
  }
  const base = wsUrlEl.value.trim() || `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}`;
  const url = `${base}/ws?room=${encodeURIComponent(room)}&user=${encodeURIComponent(nickname)}`;
  socket = new WebSocket(url);
  socket.addEventListener('open', () => setStatus('Connected', 'ok'));
  socket.addEventListener('close', () => setStatus('Disconnected', 'warn'));
  socket.addEventListener('error', err => {
    console.error(err);
    setStatus('Error', 'error');
  });
  socket.addEventListener('message', evt => handleMessage(evt.data));
}

function send(type, payload = {}) {
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    alert('먼저 연결하세요.');
    return;
  }
  socket.send(JSON.stringify({ type, ...payload }));
}

function handleMessage(raw) {
  let data;
  try {
    data = JSON.parse(raw);
  } catch (err) {
    log(`(invalid json) ${raw}`);
    return;
  }
  switch (data.type) {
    case 'log':
      log(data.body || '');
      break;
    case 'chat':
      log(data.body || '', data.author || 'player');
      break;
    case 'roster':
      renderRoster(data.state || []);
      break;
    case 'role':
      log(data.body || '역할 알림', 'role');
      break;
    case 'phase':
      log(`Phase → ${data.phase}: ${data.body || ''}`);
      break;
    case 'state':
      log(`상태 업데이트: ${JSON.stringify(data.state)}`);
      break;
    default:
      log(`이벤트 (${data.type}): ${data.body || ''}`);
  }
}

function renderRoster(players) {
  rosterEl.innerHTML = '';
  players.forEach(name => {
    const li = document.createElement('li');
    li.textContent = name;
    rosterEl.appendChild(li);
  });
}

connectForm.addEventListener('submit', connect);
chatBtn.addEventListener('click', () => {
  const text = chatInput.value.trim();
  if (!text) return;
  send('chat', { text });
  chatInput.value = '';
});
chatInput.addEventListener('keydown', evt => {
  if (evt.key === 'Enter') {
    evt.preventDefault();
    chatBtn.click();
  }
});

controlButtons.forEach(btn => {
  btn.addEventListener('click', () => {
    const action = btn.dataset.action;
    if (action === 'start') {
      send('start');
    } else if (action === 'vote') {
      send('vote', { target: voteTargetEl.value.trim() });
    } else if (action === 'night') {
      send('action', { target: actionTargetEl.value.trim() });
    }
  });
});

setStatus('Disconnected');
