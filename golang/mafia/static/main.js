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
const phaseLabelEl = document.getElementById('phase-label');
const phaseTimerEl = document.getElementById('phase-timer');

const phaseNames = { lobby: '로비', night: '밤', day: '낮', vote: '투표', defense: '최후 변론' };
const phaseDurations = { night: 25, day: 40, vote: 15, defense: 10 };

let socket;
let selectedTarget = '';
let currentPhase = 'lobby';
let currentHost = '';
let myNickname = '';
let phaseTimerHandle = null;
let phaseDeadline = 0;
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

function updatePhaseIndicator(phase, shouldStartTimer = true) {
  const label = phaseNames[phase] || phase || '대기';
  if (phaseLabelEl) {
    phaseLabelEl.textContent = label;
  }
  if (!shouldStartTimer) {
    stopPhaseTimer();
    return;
  }
  const duration = phaseDurations[phase];
  if (!duration) {
    stopPhaseTimer();
    return;
  }
  phaseDeadline = Date.now() + duration * 1000;
  renderPhaseTimer();
  if (phaseTimerHandle) {
    clearInterval(phaseTimerHandle);
  }
  phaseTimerHandle = setInterval(renderPhaseTimer, 500);
}

function renderPhaseTimer() {
  if (!phaseDeadline || !phaseTimerEl) {
    if (phaseTimerEl) {
      phaseTimerEl.textContent = '--';
    }
    return;
  }
  const remaining = Math.max(0, Math.ceil((phaseDeadline - Date.now()) / 1000));
  phaseTimerEl.textContent = `${remaining}s`;
  if (remaining <= 0) {
    stopPhaseTimer();
  }
}

function stopPhaseTimer() {
  if (phaseTimerHandle) {
    clearInterval(phaseTimerHandle);
    phaseTimerHandle = null;
  }
  phaseDeadline = 0;
  if (phaseTimerEl) {
    phaseTimerEl.textContent = '--';
  }
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

  myNickname = nickname;
  selectedTarget = '';
  updateSelectedDisplay();
  updatePhaseIndicator('lobby', false);
  stopPhaseTimer();

  const base = buildWsBase(wsModeEl.value.trim());
  const url = `${base}/ws?room=${encodeURIComponent(room)}&user=${encodeURIComponent(nickname)}`;
  socket = new WebSocket(url);
  socket.addEventListener('open', () => setStatus('Connected', 'ok'));
  socket.addEventListener('close', () => {
    setStatus('Disconnected', 'warn');
    stopPhaseTimer();
  });
  socket.addEventListener('error', err => {
    console.error(err);
    setStatus('Error', 'error');
    stopPhaseTimer();
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
      renderRoster(data.state);
      break;
    case 'role':
      log(data.body || '역할 알림', 'role');
      break;
    case 'phase':
      currentPhase = data.phase || currentPhase;
      log(`Phase → ${currentPhase}: ${data.body || ''}`);
      updatePhaseIndicator(currentPhase, true);
      break;
    case 'state':
      log(`상태 업데이트: ${JSON.stringify(data.state)}`);
      if (data.state && data.state.phase) {
        currentPhase = data.state.phase;
        updatePhaseIndicator(currentPhase, false);
      }
      break;
    default:
      log(`이벤트 (${data.type}): ${data.body || ''}`);
  }
}

function renderRoster(state) {
  const players = Array.isArray(state) ? state : (state && Array.isArray(state.players) ? state.players : []);
  currentHost = state && typeof state.host === 'string' ? state.host : '';
  rosterEl.innerHTML = '';
  if (!players.includes(selectedTarget)) {
    selectedTarget = '';
  }
  updateSelectedDisplay();
  players.forEach(name => {
    const btn = document.createElement('button');
    const isHost = name === currentHost;
    const isSelf = name === myNickname;
    btn.textContent = isHost ? '[HOST] ' + name : name;
    btn.dataset.selected = String(name === selectedTarget);
    btn.dataset.self = String(isSelf);
    btn.classList.toggle('host', isHost);
    btn.classList.toggle('self', isSelf);
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
    if (btn.dataset.hostOnly === 'true' && myNickname !== currentHost) {
      alert('방장만 사용할 수 있습니다.');
      return;
    }
    switch (btn.dataset.action) {
      case 'start':
        send('start');
        break;
      case 'kick':
        if (!selectedTarget) {
          alert('먼저 강퇴할 대상을 선택하세요.');
          return;
        }
        if (selectedTarget === myNickname) {
          alert('자기 자신은 강퇴할 수 없습니다.');
          return;
        }
        send('admin', { action: 'kick', target: selectedTarget });
        break;
      default:
        break;
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
