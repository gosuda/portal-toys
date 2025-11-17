const logEl = document.getElementById('log');
const rosterEl = document.getElementById('roster');
const statusEl = document.getElementById('status');
const chatInput = document.getElementById('chat-text');
const chatBtn = document.getElementById('chat-send');
const connectForm = document.getElementById('connect-form');
const nicknameEl = document.getElementById('nickname');
const roomEl = document.getElementById('room');
const wsModeEl = document.getElementById('ws-mode');
const controlButtons = document.querySelectorAll('#controls button');
const selectedTargetEl = document.getElementById('selected-target');

let socket;
let selectedTarget = '';
let currentPhase = 'lobby';

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
  const base = buildWsBase(wsModeEl.value.trim());
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
      currentPhase = data.phase || currentPhase;
      log(`Phase → ${currentPhase}: ${data.body || ''}`);
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
  if (!players.includes(selectedTarget)) {
    selectedTarget = '';
    updateSelectedDisplay();
  }
  players.forEach(name => {
    const btn = document.createElement('button');
    btn.textContent = name;
    btn.dataset.selected = String(name === selectedTarget);
    btn.addEventListener('click', () => handlePlayerInteraction(name));
    rosterEl.appendChild(btn);
  });
}

function updateSelectedDisplay() {
  if (selectedTarget) {
    selectedTargetEl.textContent = `선택된 대상: ${selectedTarget}`;
  } else {
    selectedTargetEl.textContent = '선택된 플레이어 없음';
  }
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

function handlePlayerInteraction(name) {
  selectedTarget = name;
  updateSelectedDisplay();
  if (currentPhase === 'vote' || currentPhase === 'defense') {
    send('vote', { target: name });
  } else if (currentPhase === 'night') {
    send('action', { target: name });
  }
}

controlButtons.forEach(btn => {
  btn.addEventListener('click', () => {
    if (btn.dataset.action === 'start') {
      send('start');
    }
  });
});

setStatus('Disconnected');
function buildWsBase(mode) {
  if (mode === 'online') {
    return `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}`;
  }
  return `${location.protocol === 'https:' ? 'wss' : 'ws'}://127.0.0.1:${location.port || 80}`;
}
