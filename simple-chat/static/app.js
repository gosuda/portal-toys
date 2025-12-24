/// <reference path="app.d.ts" />
// @ts-check
const log = /** @type {HTMLDivElement} */ (document.getElementById('log'));
const resizer = /** @type {HTMLDivElement} */ (document.getElementById('resizer'));
const user = /** @type {HTMLInputElement} */ (document.getElementById('user'));
const cmd = /** @type {HTMLTextAreaElement} */ (document.getElementById('cmd'));
const roll = /** @type {HTMLButtonElement} */ (document.getElementById('roll'));
const promptEl = /** @type {HTMLSpanElement} */ (document.getElementById('prompt'));
const usersCount = /** @type {HTMLSpanElement} */ (document.getElementById('users-count'));
const newMessageBubble = /** @type {HTMLDivElement} */ (document.getElementById('new-message-bubble'));
const imageUploadBtn = /** @type {HTMLButtonElement} */ (document.getElementById('image-upload-btn'));
const imageInput = /** @type {HTMLInputElement} */ (document.getElementById('image-input'));
const imageModal = /** @type {HTMLDivElement} */ (document.getElementById('image-modal'));
const modalImage = /** @type {HTMLImageElement} */ (document.getElementById('modal-image'));
const usersModal = /** @type {HTMLDivElement} */ (document.getElementById('users-modal'));
const usersModalOverlay = /** @type {HTMLDivElement} */ (document.getElementById('users-modal-overlay'));
const usersModalClose = /** @type {HTMLButtonElement} */ (document.getElementById('users-modal-close'));
const usersList = /** @type {HTMLDivElement} */ (document.getElementById('users-list'));

// Persisted chat height
const CHAT_HEIGHT_KEY = 'chatHeightPx';
/**
 * @param {number} px
 * @returns {number}
 */
function clampChatHeight(px) {
  const minH = 200; // minimum px
  const maxH = Math.min(window.innerHeight - 140, 1200); // leave space for prompt/termbar
  return Math.max(minH, Math.min(maxH, Math.floor(px)));
}
/**
 * @param {number} px
 */
function applyChatHeight(px) {
  const clamped = clampChatHeight(px);
  log.style.height = clamped + 'px';
  try { localStorage.setItem(CHAT_HEIGHT_KEY, String(clamped)); } catch (_) { }
}
// Initialize height from localStorage (desktop only). On mobile, we start from CSS 50vh but still allow custom.
try {
  const saved = localStorage.getItem(CHAT_HEIGHT_KEY);
  if (saved) {
    const val = parseInt(saved, 10);
    if (!isNaN(val) && val > 0) {
      log.style.height = clampChatHeight(val) + 'px';
    }
  }
} catch (_) { }

// Drag-to-resize (mouse, touch, and pen via Pointer Events)
(function enableResize() {
  if (!resizer) return;
  let startY = 0;
  let startH = 0;
  let dragging = false;

  /** @param {any} e */
  function onPointerDown(e) {
    dragging = true;
    startY = e.clientY || (e.touches && e.touches[0] && e.touches[0].clientY) || 0;
    startH = log.getBoundingClientRect().height;
    document.body.style.userSelect = 'none';
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp, { once: true });
  }
  /** @param {any} e */
  function onPointerMove(e) {
    if (!dragging) return;
    const currentY = e.clientY || (e.touches && e.touches[0] && e.touches[0].clientY) || 0;
    const delta = currentY - startY;
    const next = clampChatHeight(startH + delta);
    log.style.height = next + 'px';
  }
  function onPointerUp() {
    dragging = false;
    document.body.style.userSelect = '';
    const h = log.getBoundingClientRect().height;
    applyChatHeight(h);
    window.removeEventListener('pointermove', onPointerMove);
  }

  // Support legacy mouse/touch if PointerEvent not available
  if (window.PointerEvent) {
    resizer.addEventListener('pointerdown', onPointerDown);
  } else {
    resizer.addEventListener('mousedown', (e) => { e.preventDefault(); onPointerDown(e); });
    resizer.addEventListener('touchstart', (e) => { onPointerDown(e); }, { passive: true });
    window.addEventListener('mousemove', onPointerMove);
    window.addEventListener('mouseup', onPointerUp, { once: true });
  }

  // Double-click to reset height to default
  resizer.addEventListener('dblclick', () => {
    // Use CSS variable default: desktop 420px, mobile 50vh -> translate to px
    const fallback = Math.max(300, Math.floor(window.innerHeight * 0.5));
    log.style.height = fallback + 'px';
    applyChatHeight(fallback);
  });

  // Re-clamp on resize/orientation change
  window.addEventListener('resize', () => {
    const h = log.getBoundingClientRect().height;
    log.style.height = clampChatHeight(h) + 'px';
  });
})();

// Store current online users and admin status
/** @type {Array<{name: string, uid?: string}|string>} */
let onlineUsers = [];
let isCurrentUserAdmin = false;

// Smart scroll functions
function isScrolledToBottom() {
  const threshold = 50;
  return (log.scrollHeight - log.scrollTop - log.clientHeight) < threshold;
}

function scrollToBottom() {
  log.scrollTop = log.scrollHeight;
  newMessageBubble.classList.remove('show');
}

/**
 * @param {string} username
 * @param {string} text
 */
function showNewMessageBubble(username, text) {
  const maxLen = 30;
  const preview = text.length > maxLen ? text.substring(0, maxLen) + '...' : text;
  newMessageBubble.innerHTML = sanitizeNickname(username) + ': ' + escapeHTML(preview);
  newMessageBubble.classList.add('show');
}

// Click bubble to scroll to bottom
newMessageBubble.addEventListener('click', scrollToBottom);

function setPrompt() {
  const nick = (user.value || 'anon').replace(/\s+/g, '') || 'anon';
  promptEl.innerHTML = sanitizeNickname(nick) + '<span style="color:var(--fg)">@chat:~$</span>';
}
function randomNick() {
  // Short nickname: one word + 4-digit number
  const words = ['gopher', 'rust', 'unix', 'kernel', 'docker', 'kube', 'vim', 'emacs', 'tmux', 'nvim', 'git', 'linux', 'bsd', 'wasm', 'grpc', 'lambda', 'pointer', 'monad', 'null', 'byte', 'packet', 'devops', 'cli'];
  const w = words[Math.floor(Math.random() * words.length)];
  const num = Math.floor(Math.random() * 9000) + 1000; // 4-digit
  return w + '-' + num;
}
// Stable client UID per browser (per origin) - stored in localStorage
function genUID() { try { return (crypto.randomUUID && crypto.randomUUID()) || '' } catch (_) { return '' } }
function fallbackUID() { return Math.random().toString(36).slice(2) + Date.now().toString(36) }

const UID_STORAGE_KEY = 'simple-chat-uid';
const TOKEN_STORAGE_KEY = 'simple-chat-token';
/** @type {string|null} */
let clientUID = null;
/** @type {string|null} */
let clientToken = null;

// Try to load existing UID and token from localStorage
try {
  clientUID = localStorage.getItem(UID_STORAGE_KEY);
  clientToken = localStorage.getItem(TOKEN_STORAGE_KEY);
} catch (e) { }

// Generate new UID if not found or invalid
if (!clientUID || clientUID.length < 8) {
  clientUID = genUID() || fallbackUID();
  clientToken = null; // New UID means no token yet
  // Save to localStorage
  try {
    localStorage.setItem(UID_STORAGE_KEY, clientUID);
    localStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch (e) { }
}

// Restore nickname or initialize randomly
let savedNick = null;
try { savedNick = localStorage.getItem('nick'); } catch (_) { }
if (savedNick) {
  const oldPattern = /^[a-z]+-[a-z]+-[0-9a-z]{2,}$/i.test(savedNick);
  if (oldPattern) {
    user.value = randomNick();
    try { localStorage.setItem('nick', user.value); } catch (_) { }
  } else {
    user.value = savedNick;
  }
} else {
  user.value = randomNick();
  try { localStorage.setItem('nick', user.value); } catch (_) { }
}
setPrompt();

// Stable color per nickname (expanded palette)
const PALETTE = [
  '#60a5fa', '#22c55e', '#f59e0b', '#ef4444', '#a78bfa', '#14b8a6', '#eab308', '#f472b6', '#8b5cf6', '#06b6d4',
  '#34d399', '#fb7185', '#c084fc', '#f97316', '#84cc16', '#10b981', '#38bdf8', '#f43f5e', '#e879f9', '#fde047',
  '#93c5fd', '#4ade80', '#fca5a5', '#a3e635', '#67e8f9', '#f0abfc', '#fbbf24', '#86efac'
];
/** @param {string} s */
function hashNick(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) { h = ((h << 5) - h) + s.charCodeAt(i); h |= 0; }
  return (h >>> 0);
}
/** @param {string} nick */
function colorFor(nick) {
  const idx = hashNick(nick || 'anon') % PALETTE.length;
  return PALETTE[idx];
}
let lastUserCount = 0;
/**
 * @param {Array<OnlineUser|string>} userList
 * @param {boolean} [isAdmin]
 */
function renderRoster(userList, isAdmin) {
  // userList is now array of {name, uid} objects (or legacy string array)
  onlineUsers = userList || [];
  isCurrentUserAdmin = !!isAdmin;
  const count = onlineUsers.length;
  logWS('INFO', 'Roster updated: ' + count + ' users online, isAdmin: ' + isCurrentUserAdmin, userList);
  if (usersCount) {
    usersCount.textContent = String(count);
    // Flash effect when user count changes
    if (count !== lastUserCount) {
      const pill = document.querySelector('.userspill');
      if (pill) {
        pill.classList.add('flash');
        setTimeout(() => pill.classList.remove('flash'), 500);
      }
      lastUserCount = count;
    }
  }
}

// Show users modal
function showUsersModal() {
  // Populate users list
  usersList.innerHTML = '';
  if (onlineUsers.length === 0) {
    usersList.innerHTML = '<div class="users-list-item">No users online</div>';
  } else {
    onlineUsers.forEach((onlineUser) => {
      const item = document.createElement('div');
      item.className = 'users-list-item';

      // Handle both new format {name, uid} and old format (string)
      if (typeof onlineUser === 'object' && onlineUser.name) {
        // New format with UID
        const nameSpan = document.createElement('div');
        nameSpan.style.fontWeight = 'bold';
        // Admin sees red circle indicator
        if (isCurrentUserAdmin) {
          nameSpan.innerHTML = '<span style="color:#ef4444;margin-right:6px;">●</span>' + sanitizeNickname(onlineUser.name);
        } else {
          nameSpan.innerHTML = sanitizeNickname(onlineUser.name);
        }

        item.appendChild(nameSpan);

        // Only show UID if admin and uid is available
        if (isCurrentUserAdmin && onlineUser.uid) {
          const uidSpan = document.createElement('div');
          uidSpan.style.fontSize = '11px';
          uidSpan.style.color = 'var(--muted)';
          uidSpan.style.marginTop = '4px';
          uidSpan.style.fontFamily = 'monospace';
          uidSpan.style.cursor = 'pointer';
          uidSpan.style.userSelect = 'all';
          uidSpan.textContent = 'UID: ' + onlineUser.uid;
          uidSpan.title = 'Click to copy UID';

          // Click to copy UID
          uidSpan.addEventListener('click', (e) => {
            e.stopPropagation();
            if (navigator.clipboard && navigator.clipboard.writeText) {
              navigator.clipboard.writeText(onlineUser.uid || '').then(() => {
                const originalText = uidSpan.textContent;
                uidSpan.textContent = 'UID copied!';
                uidSpan.style.color = 'var(--accent)';
                setTimeout(() => {
                  uidSpan.textContent = originalText;
                  uidSpan.style.color = 'var(--muted)';
                }, 1500);
              }).catch(() => {
                alert('Failed to copy UID: ' + onlineUser.uid);
              });
            } else {
              // Fallback for older browsers
              alert('UID: ' + onlineUser.uid);
            }
          });

          item.appendChild(uidSpan);
        }
      } else {
        // Legacy format (just username string)
        item.innerHTML = sanitizeNickname(/** @type {string} */(onlineUser));
      }

      usersList.appendChild(item);
    });
  }
  usersModal.classList.add('show');
  usersModalOverlay.classList.add('show');
}

// Hide users modal
function hideUsersModal() {
  usersModal.classList.remove('show');
  usersModalOverlay.classList.remove('show');
}

// Click on online count to show modal
document.querySelector('.userspill')?.addEventListener('click', showUsersModal);

// Close modal on close button
usersModalClose.addEventListener('click', hideUsersModal);

// Close modal on overlay click
usersModalOverlay.addEventListener('click', hideUsersModal);
// Batch DOM updates for better performance
/** @type {HTMLDivElement[]} */
let pendingAppends = [];
/** @type {PendingMessage[]} */
let pendingMessages = [];
/** @type {ReturnType<typeof setTimeout>|null} */
let appendTimer = null;

// Track seen messages to avoid duplicates on reconnect
const seenMessages = new Set();
/** @type {string[]} */
const seenMessageKeys = []; // Keep track of order for cleanup
const maxSeenMessages = 300;

/**
 * @param {{ts: string, user?: string, text?: string}} msg
 * @returns {string}
 */
function getMsgKey(msg) {
  return msg.ts + '|' + (msg.user || '') + '|' + (msg.text || '');
}

/** @param {ChatMessage} msg */
function append(msg) {
  // Handle token event from server
  if (msg.event === 'token' && msg.token) {
    clientToken = msg.token;
    try {
      localStorage.setItem(TOKEN_STORAGE_KEY, /** @type {string} */(clientToken));
    } catch (e) { }
    return;
  }

  if (msg.event === 'roster') {
    // Prefer new userList format with UIDs, fallback to legacy users array
    const userList = msg.userList || msg.users || [];
    logWS('DEBUG', 'Roster event received with ' + userList.length + ' users, isAdmin: ' + msg.isAdmin, userList);
    renderRoster(userList, msg.isAdmin);
    return;
  }

  if (msg.event === 'rename' || msg.event === 'joined' || msg.event === 'left') {
    // Don't show rename/join/left events
    return;
  }

  // Skip duplicate messages (same timestamp + user + text)
  const msgKey = getMsgKey(msg);
  if (seenMessages.has(msgKey)) {
    return;
  }
  seenMessages.add(msgKey);
  seenMessageKeys.push(msgKey);

  // Cleanup old entries from Set to prevent memory leak
  while (seenMessageKeys.length > maxSeenMessages) {
    const oldKey = seenMessageKeys.shift();
    seenMessages.delete(oldKey);
  }

  const div = document.createElement('div');
  div.className = 'line';
  const ts = new Date(msg.ts).toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const nick = (msg.user || 'anon');
  const color = colorFor(nick);

  let isActualMessage = false;

  {
    div.innerHTML = '<span class="ts">[' + ts + ']</span> <span class="usr" style="color:' + color + '">' +
      sanitizeNickname(nick) + '</span>: ' + linkifyText(msg.text || '');
    isActualMessage = true;
  }

  pendingAppends.push(div);

  // Store actual chat messages for bubble
  if (isActualMessage && msg.text) {
    pendingMessages.push({ username: nick, text: msg.text });
  }

  // Debounce DOM updates - batch multiple messages together
  if (appendTimer) clearTimeout(appendTimer);
  appendTimer = setTimeout(() => {
    const wasAtBottom = isScrolledToBottom();

    const fragment = document.createDocumentFragment();
    pendingAppends.forEach(d => fragment.appendChild(d));
    log.appendChild(fragment);

    // Trim old messages to keep DOM size manageable
    const maxDOMMessages = 200;
    const messageLines = log.querySelectorAll('.line');
    if (messageLines.length > maxDOMMessages) {
      const toRemove = messageLines.length - maxDOMMessages;
      for (let i = 0; i < toRemove; i++) {
        if (messageLines[i] && messageLines[i].parentNode === log) {
          log.removeChild(messageLines[i]);
        }
      }
    }

    if (wasAtBottom) {
      log.scrollTop = log.scrollHeight;
      newMessageBubble.classList.remove('show');
    } else {
      if (pendingMessages.length > 0) {
        const latestMsg = pendingMessages[pendingMessages.length - 1];
        showNewMessageBubble(latestMsg.username, latestMsg.text);
      }
    }

    pendingAppends = [];
    pendingMessages = [];
  }, 0);
}
/** @param {string} s */
function escapeHTML(s) {
  // Block alert() function
  s = s.replace(/alert\(/gi, '');
  /** @type {{[key: string]: string}} */
  const entities = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' };
  return s.replace(/[&<>"]/g, c => entities[c] || c);
}

// Convert URLs in text to clickable links and display images
// Also renders markdown-like code blocks
/** @param {string} text */
function linkifyText(text) {
  // Check if this is an image message
  if (text.startsWith('[IMAGE]')) {
    const base64Image = text.substring(7); // Remove '[IMAGE]' prefix
    const imgId = 'img-' + Math.random().toString(36).substr(2, 9);
    return '<div class="image-placeholder" data-img-id="' + imgId + '">[Image - Click to view]</div>' +
      '<div class="image-controls" style="display:none" data-img-id="' + imgId + '">' +
      '<button class="toggle-img" data-img-id="' + imgId + '">Hide</button>' +
      '<button class="fullscreen-img" data-img-id="' + imgId + '">Fullscreen</button>' +
      '</div>' +
      '<img id="' + imgId + '" src="' + base64Image + '" alt="Uploaded image" class="chat-image" />';
  }

  // Check if text contains code blocks - if so, use renderMarkdown
  const backtick = String.fromCharCode(96);
  if (text.includes(backtick + backtick + backtick) || text.includes(backtick)) {
    // renderMarkdown handles escaping internally for code blocks
    let result = renderMarkdownWithLinks(text);
    // Schedule syntax highlighting after DOM update
    setTimeout(highlightAllCodeBlocks, 0);
    return result;
  }

  // No code blocks - use simple escape and linkify
  const escaped = escapeHTML(text);
  // URL regex pattern
  const urlPattern = /(\b(https?|ftp):\/\/[-A-Z0-9+&@#\/%?=~_|!:,.;]*[-A-Z0-9+&@#\/%=~_|])/gim;
  // Convert newlines to <br> for multi-line messages
  return escaped.replace(/\n/g, '<br>').replace(urlPattern, '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>');
}

// Render markdown code blocks and also linkify URLs in non-code parts
/** @param {string} text */
function renderMarkdownWithLinks(text) {
  // Use renderMarkdown from markdown.js if available
  if (typeof renderMarkdown === 'function') {
    let result = renderMarkdown(text);
    // Linkify URLs in non-code parts (outside of code-block-wrapper and inline-code)
    // Split by code blocks, linkify non-code parts, rejoin
    const parts = result.split(/(<div class="code-block-wrapper">[\s\S]*?<\/div>|<span class="inline-code">[\s\S]*?<\/span>)/g);
    const urlPattern = /(\b(https?|ftp):\/\/[-A-Z0-9+&@#\/%?=~_|!:,.;]*[-A-Z0-9+&@#\/%=~_|])/gim;
    result = parts.map(part => {
      if (part.startsWith('<div class="code-block-wrapper">') || part.startsWith('<span class="inline-code">')) {
        return part;
      }
      // Convert newlines to <br> and linkify
      return part.replace(/\n/g, '<br>').replace(urlPattern, '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>');
    }).join('');
    return result;
  }
  // Fallback if markdown.js not loaded
  const escaped = escapeHTML(text);
  const urlPattern = /(\b(https?|ftp):\/\/[-A-Z0-9+&@#\/%?=~_|!:,.;]*[-A-Z0-9+&@#\/%=~_|])/gim;
  return escaped.replace(/\n/g, '<br>').replace(urlPattern, '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>');
}

// Sanitize nickname: allow HTML/XSS but remove project class names
/** @param {string} html */
function sanitizeNickname(html) {
  try {
    const temp = document.createElement('div');
    temp.innerHTML = html;

    // List of project class names to remove
    const blockedClasses = [
      'wrap', 'term', 'termbar', 'termbar-left', 'termbar-center', 'term-actions',
      'dots', 'dot', 'red', 'yellow', 'green',
      'nick', 'userspill', 'screen', 'new-message-bubble', 'promptline',
      'ts', 'usr', 'line', 'event'
    ];

    // Recursively remove blocked class names
    /** @param {Node} node */
    function cleanClasses(node) {
      if (node.nodeType === Node.ELEMENT_NODE) {
        const el = /** @type {Element} */ (node);
        // Check if element has class attribute
        if (el.hasAttribute('class')) {
          const classes = (el.getAttribute('class') || '').split(/\s+/);
          const filteredClasses = classes.filter(/** @param {string} cls */ cls => !blockedClasses.includes(cls));

          if (filteredClasses.length > 0) {
            el.setAttribute('class', filteredClasses.join(' '));
          } else {
            el.removeAttribute('class');
          }
        }

        // Recursively clean children
        Array.from(node.childNodes).forEach(child => cleanClasses(child));
      }
    }

    cleanClasses(temp);
    return temp.innerHTML;
  } catch (e) {
    console.error('Sanitize error:', e);
    return html; // Return as-is if error
  }
}

// WebSocket connection management with auto-reconnect
const wsProto = location.protocol === 'https:' ? 'wss' : 'ws';
const basePath = location.pathname.endsWith('/') ? location.pathname : (location.pathname + '/');
const wsURL = wsProto + '://' + location.host + basePath + 'ws';

/** @type {WebSocket|null} */
let ws = null;
/** @type {ReturnType<typeof setTimeout>|null} */
let reconnectTimer = null;
let reconnectAttempts = 0;
/** @type {ReturnType<typeof setInterval>|null} */
let heartbeatTimer = null;
let connectionStartTime = 0;
let lastMessageTime = 0;
let messageCount = 0;
let heartbeatCount = 0;
const maxReconnectDelay = 30000; // 30 seconds max
const initialReconnectDelay = 1000; // 1 second initial
const heartbeatInterval = 20000; // 20 seconds - send heartbeat to keep connection alive (prevents proxy timeouts)

/**
 * @param {string} level
 * @param {string} message
 * @param {any} [data]
 */
function logWS(level, message, data) {
  // Logging disabled for production
}

function getReconnectDelay() {
  // Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s, 30s...
  const delay = Math.min(initialReconnectDelay * Math.pow(2, reconnectAttempts), maxReconnectDelay);
  logWS('DEBUG', 'Reconnect delay calculated: ' + delay + 'ms (attempt ' + reconnectAttempts + ')');
  return delay;
}

/**
 * @param {boolean} connected
 * @param {boolean} [reconnecting]
 */
function updateConnectionStatus(connected, reconnecting = false) {
  logWS('INFO', 'Connection status update: connected=' + connected + ', reconnecting=' + reconnecting);

  const greenDot = /** @type {HTMLElement|null} */ (document.querySelector('.dot.green'));
  const yellowDot = /** @type {HTMLElement|null} */ (document.querySelector('.dot.yellow'));
  const redDot = /** @type {HTMLElement|null} */ (document.querySelector('.dot.red'));

  if (greenDot) {
    greenDot.style.opacity = connected ? '1' : '0.3';
  }
  if (redDot) {
    redDot.style.opacity = (!connected && !reconnecting) ? '1' : '0.3';
  }
  if (yellowDot && reconnecting) {
    // Pulse animation for reconnecting state
    yellowDot.style.animation = 'pulse 1.5s ease-in-out infinite';
    yellowDot.style.opacity = '1';
  } else if (yellowDot) {
    yellowDot.style.animation = '';
    yellowDot.style.opacity = '1';
  }
}

/**
 * @param {string} message
 */
function showConnectionMessage(message) {
  // Show a temporary connection status message in the chat
  const div = document.createElement('div');
  div.className = 'line event';
  div.style.color = 'var(--muted)';
  div.textContent = '⚠ ' + message;
  log.appendChild(div);
  if (isScrolledToBottom()) {
    log.scrollTop = log.scrollHeight;
  }
}

function startHeartbeat() {
  logWS('INFO', 'Starting heartbeat timer (interval: ' + heartbeatInterval + 'ms)');
  if (heartbeatTimer) {
    logWS('WARN', 'Clearing existing heartbeat timer');
    clearInterval(heartbeatTimer);
  }
  heartbeatCount = 0;
  heartbeatTimer = setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      heartbeatCount++;
      logWS('DEBUG', 'Sending heartbeat #' + heartbeatCount);
      try {
        // Send empty message as heartbeat (text is empty, so server won't broadcast)
        ws.send(JSON.stringify({ user: (user.value || 'anon'), text: '', uid: clientUID, token: clientToken }));
        logWS('DEBUG', 'Heartbeat sent successfully');
      } catch (e) {
        logWS('ERROR', 'Heartbeat failed', e);
      }
    } else {
      logWS('WARN', 'Heartbeat skipped: WebSocket not OPEN (state: ' + (ws ? ws.readyState : 'null') + ')');
    }
  }, heartbeatInterval);
}

function stopHeartbeat() {
  logWS('INFO', 'Stopping heartbeat timer (sent ' + heartbeatCount + ' heartbeats)');
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}

function connectWebSocket() {
  logWS('INFO', '=== connectWebSocket() called ===');
  logWS('DEBUG', 'Current state: reconnectAttempts=' + reconnectAttempts + ', reconnectTimer=' + (reconnectTimer ? 'active' : 'null'));

  if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
    logWS('WARN', 'Already connected or connecting, skipping. State: ' + ws.readyState);
    return;
  }

  connectionStartTime = Date.now();
  messageCount = 0;
  logWS('INFO', 'Creating new WebSocket connection to: ' + wsURL);

  try {
    ws = new WebSocket(wsURL);
    logWS('INFO', 'WebSocket object created, readyState: ' + ws.readyState);
  } catch (e) {
    logWS('ERROR', 'Failed to create WebSocket', e);
    return;
  }

  ws.onopen = () => {
    const connectionTime = Date.now() - connectionStartTime;
    logWS('INFO', '✓ WebSocket OPENED (took ' + connectionTime + 'ms)');
    reconnectAttempts = 0;
    updateConnectionStatus(true);
    startHeartbeat();

    logWS('DEBUG', 'Sending initial user info message');
    try {
      if (ws) ws.send(JSON.stringify({ user: (user.value || 'anon'), text: '', uid: clientUID, token: clientToken }));
      logWS('DEBUG', 'Initial message sent successfully');
    } catch (e) {
      logWS('ERROR', 'Failed to send initial message', e);
    }
  };

  ws.onmessage = (e) => {
    messageCount++;
    lastMessageTime = Date.now();
    const timeSinceConnection = lastMessageTime - connectionStartTime;
    logWS('DEBUG', 'Message #' + messageCount + ' received (connection age: ' + Math.round(timeSinceConnection / 1000) + 's)');

    try {
      const msg = JSON.parse(e.data);
      append(msg);
    } catch (err) {
      logWS('ERROR', 'Failed to parse message', err);

      // Try to find the problematic position
      const parseErr = /** @type {Error} */ (err);
      const errorMatch = parseErr.message?.match(/position (\d+)/);
      if (errorMatch) {
        const pos = parseInt(errorMatch[1]);
        const start = Math.max(0, pos - 50);
        const end = Math.min(e.data.length, pos + 50);
        const context = e.data.substring(start, end);
        logWS('ERROR', 'Error at position ' + pos + ', context:', context);
        logWS('ERROR', 'Character at error:', e.data.charCodeAt(pos) + ' (' + e.data.charAt(pos) + ')');
      }

      logWS('ERROR', 'Full message length:', e.data.length);
      logWS('ERROR', 'Message preview:', e.data.substring(0, 200));

      // Don't crash - just skip this message
      showConnectionMessage('메시지 파싱 오류 발생 (건너뜀)');
    }
  };

  ws.onerror = (err) => {
    const timeSinceConnection = Date.now() - connectionStartTime;
    logWS('ERROR', '✗ WebSocket ERROR (connection age: ' + Math.round(timeSinceConnection / 1000) + 's)', err);
  };

  ws.onclose = (e) => {
    const timeSinceConnection = Date.now() - connectionStartTime;
    logWS('INFO', '✗ WebSocket CLOSED (connection age: ' + Math.round(timeSinceConnection / 1000) + 's)');
    logWS('INFO', 'Close details: code=' + e.code + ', reason="' + e.reason + '", wasClean=' + e.wasClean);
    logWS('INFO', 'Stats: received ' + messageCount + ' messages, sent ' + heartbeatCount + ' heartbeats');

    updateConnectionStatus(false, false);
    stopHeartbeat();

    // Always attempt to reconnect regardless of close code
    const delay = getReconnectDelay();
    reconnectAttempts++;
    logWS('INFO', 'Scheduling reconnect in ' + delay + 'ms (attempt #' + reconnectAttempts + ')');
    updateConnectionStatus(false, true);

    if (reconnectTimer) {
      logWS('WARN', 'Clearing existing reconnect timer');
      clearTimeout(reconnectTimer);
    }
    reconnectTimer = setTimeout(() => {
      logWS('DEBUG', 'Reconnect timer fired');
      reconnectTimer = null;
      connectWebSocket();
    }, delay);
  };
}

// Initial connection
logWS('INFO', '========== Application Starting ==========');
logWS('INFO', 'Client UID: ' + clientUID);
logWS('INFO', 'WebSocket URL: ' + wsURL);
logWS('INFO', 'Heartbeat interval: ' + heartbeatInterval + 'ms');
connectWebSocket();

function send() {
  const payload = { user: (user.value || 'anon'), text: cmd.value.trim(), uid: clientUID, token: clientToken };
  if (!payload.text) {
    logWS('DEBUG', 'send() called with empty text, ignoring');
    return;
  }

  logWS('INFO', 'Attempting to send message: "' + payload.text.substring(0, 50) + (payload.text.length > 50 ? '...' : '') + '"');

  // Check connection and reconnect if needed
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    logWS('ERROR', 'Cannot send: WebSocket not OPEN (state: ' + (ws ? ws.readyState : 'null') + ')');
    showConnectionMessage('연결되지 않았습니다. 연결 후 다시 시도하세요.');
    return;
  }

  try {
    ws.send(JSON.stringify(payload));
    logWS('INFO', 'Message sent successfully');
    cmd.value = '';
  } catch (e) {
    logWS('ERROR', 'Failed to send message', e);
    showConnectionMessage('메시지 전송 실패');
  }
}

// Handle image upload
imageUploadBtn.addEventListener('click', () => {
  imageInput.click();
});

// Resize image before uploading
/**
 * @param {File} file
 * @param {number} maxWidth
 * @param {number} maxHeight
 * @param {number} quality
 * @param {(base64: string) => void} callback
 */
function resizeImage(file, maxWidth, maxHeight, quality, callback) {
  const reader = new FileReader();
  reader.onload = (e) => {
    const img = new Image();
    img.onload = () => {
      const canvas = document.createElement('canvas');
      let width = img.width;
      let height = img.height;

      // Calculate new dimensions
      if (width > height) {
        if (width > maxWidth) {
          height = height * (maxWidth / width);
          width = maxWidth;
        }
      } else {
        if (height > maxHeight) {
          width = width * (maxHeight / height);
          height = maxHeight;
        }
      }

      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');
      if (ctx) ctx.drawImage(img, 0, 0, width, height);

      // Convert to base64 with quality compression
      const resizedBase64 = canvas.toDataURL('image/jpeg', quality);
      callback(resizedBase64);
    };
    img.onerror = () => {
      alert('Failed to load image');
    };
    img.src = /** @type {string} */ (e.target?.result);
  };
  reader.onerror = () => {
    alert('Failed to read file');
  };
  reader.readAsDataURL(file);
}

/**
 * Upload an image file to the chat
 * @param {File} file - The image file to upload
 * @param {() => void} [onComplete] - Optional callback after upload completes
 */
function uploadImage(file, onComplete) {
  // Check WebSocket connection
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    alert('Not connected to server');
    if (onComplete) onComplete();
    return;
  }

  // Show uploading status
  cmd.placeholder = '[Uploading image...]';
  cmd.disabled = true;

  // Resize image (max 800x800, quality 0.7)
  resizeImage(file, 800, 800, 0.7, (resizedBase64) => {
    try {
      // Check final size (base64 encoded)
      if (resizedBase64.length > 500000) { // ~500KB limit
        alert('Image is too large even after compression. Please use a smaller image.');
        cmd.placeholder = 'type a message and press Enter';
        cmd.disabled = false;
        if (onComplete) onComplete();
        return;
      }

      // Send image as text with special prefix
      const payload = {
        user: (user.value || 'anon'),
        text: '[IMAGE]' + resizedBase64,
        uid: clientUID,
        token: clientToken
      };
      if (ws) ws.send(JSON.stringify(payload));

      // Reset input
      cmd.placeholder = 'type a message and press Enter';
      cmd.disabled = false;
      if (onComplete) onComplete();
    } catch (e) {
      console.error('Failed to send image:', e);
      const sendErr = /** @type {Error} */ (e);
      alert('Failed to send image: ' + sendErr.message);
      cmd.placeholder = 'type a message and press Enter';
      cmd.disabled = false;
      if (onComplete) onComplete();
    }
  });
}

imageInput.addEventListener('change', (e) => {
  const target = /** @type {HTMLInputElement} */ (e.target);
  const file = target.files?.[0];
  if (!file) return;

  // Check if file is an image
  if (!file.type.startsWith('image/')) {
    alert('Please select an image file');
    imageInput.value = '';
    return;
  }

  // Check file size (max 10MB before resize)
  if (file.size > 10 * 1024 * 1024) {
    alert('Image size must be less than 10MB');
    imageInput.value = '';
    return;
  }

  uploadImage(file, () => {
    imageInput.value = '';
  });
});

// Debounced notify of nickname changes to server so roster updates without sending a chat
/** @type {ReturnType<typeof setTimeout>|null} */
let nickTimer = null;
user.addEventListener('input', () => {
  try { localStorage.setItem('nick', user.value); } catch (_) { }
  setPrompt();
  if (ws && ws.readyState === WebSocket.OPEN) {
    if (nickTimer) clearTimeout(nickTimer);
    nickTimer = setTimeout(() => {
      try { if (ws) ws.send(JSON.stringify({ user: (user.value || 'anon'), text: '', uid: clientUID, token: clientToken })); } catch (_) { }
    }, 300);
  }
});

roll.addEventListener('click', () => {
  user.value = randomNick();
  try { localStorage.setItem('nick', user.value); } catch (_) { }
  setPrompt();
  user.focus();
});

// Handle IME composition properly to avoid duplicated last character
// on Enter when using Korean/Japanese/Chinese input methods.
// Shift+Enter inserts newline, Enter sends message.
cmd.addEventListener('keydown', e => {
  if (e.isComposing || e.keyCode === 229) { return; }
  if (e.key === 'Enter') {
    if (e.shiftKey) {
      // Allow default behavior (newline insertion)
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    send();
    // Reset textarea height after sending
    cmd.style.height = 'auto';
    // Ensure focus stays on the message input on mobile
    setTimeout(() => cmd.focus(), 0);
  }
});

// Auto-resize textarea as content grows
cmd.addEventListener('input', () => {
  cmd.style.height = 'auto';
  cmd.style.height = Math.min(cmd.scrollHeight, 120) + 'px';
});

// Handle paste event for images from clipboard
cmd.addEventListener('paste', (e) => {
  const clipboardData = e.clipboardData;
  if (!clipboardData) return;

  // Check if clipboard contains image
  const items = clipboardData.items;
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    if (item.type.startsWith('image/')) {
      e.preventDefault(); // Prevent default paste behavior

      const file = item.getAsFile();
      if (!file) return;

      uploadImage(file);
      return; // Only handle first image
    }
  }
});

// Handle drag and drop for images
const dropZone = log; // Use chat log as drop zone
let dragCounter = 0;

dropZone.addEventListener('dragenter', (e) => {
  e.preventDefault();
  e.stopPropagation();
  dragCounter++;
  if (e.dataTransfer?.types.includes('Files')) {
    dropZone.classList.add('drag-over');
  }
});

dropZone.addEventListener('dragleave', (e) => {
  e.preventDefault();
  e.stopPropagation();
  dragCounter--;
  if (dragCounter === 0) {
    dropZone.classList.remove('drag-over');
  }
});

dropZone.addEventListener('dragover', (e) => {
  e.preventDefault();
  e.stopPropagation();
});

dropZone.addEventListener('drop', (e) => {
  e.preventDefault();
  e.stopPropagation();
  dragCounter = 0;
  dropZone.classList.remove('drag-over');

  const files = e.dataTransfer?.files;
  if (!files || files.length === 0) return;

  // Find first image file
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    if (file.type.startsWith('image/')) {
      // Check file size (max 10MB before resize)
      if (file.size > 10 * 1024 * 1024) {
        alert('Image size must be less than 10MB');
        return;
      }
      uploadImage(file);
      return; // Only handle first image
    }
  }

  alert('Please drop an image file');
});
// Global click handler for images
document.addEventListener('click', (e) => {
  const target = /** @type {HTMLElement|null} */ (e.target);
  if (!target) return;

  // If clicked on image placeholder - show image and controls
  if (target.classList?.contains('image-placeholder')) {
    const imgId = target.getAttribute('data-img-id');
    const img = /** @type {HTMLElement|null} */ (document.getElementById(imgId || ''));
    const controls = /** @type {HTMLElement|null} */ (document.querySelector('.image-controls[data-img-id="' + imgId + '"]'));
    if (img && controls) {
      img.style.display = 'block';
      controls.style.display = 'flex';
      target.style.display = 'none';
    }
    e.preventDefault();
    return;
  }

  // If clicked on toggle button - hide image and show placeholder
  if (target.classList?.contains('toggle-img')) {
    const imgId = target.getAttribute('data-img-id');
    const img = /** @type {HTMLElement|null} */ (document.getElementById(imgId || ''));
    const controls = /** @type {HTMLElement|null} */ (document.querySelector('.image-controls[data-img-id="' + imgId + '"]'));
    const placeholder = /** @type {HTMLElement|null} */ (document.querySelector('.image-placeholder[data-img-id="' + imgId + '"]'));
    if (img && controls && placeholder) {
      img.style.display = 'none';
      controls.style.display = 'none';
      placeholder.style.display = 'inline-block';
    }
    e.preventDefault();
    return;
  }

  // If clicked on fullscreen button - show modal
  if (target.classList?.contains('fullscreen-img')) {
    const imgId = target.getAttribute('data-img-id');
    const img = /** @type {HTMLImageElement|null} */ (document.getElementById(imgId || ''));
    if (img) {
      modalImage.src = img.src;
      imageModal.classList.add('show');
    }
    e.preventDefault();
    return;
  }
});

// Close modal when clicking on it
imageModal.addEventListener('click', () => {
  imageModal.classList.remove('show');
  modalImage.src = '';
});

// Auto-scroll to bottom when input is focused (mobile)
// dvh unit handles viewport resizing automatically, we just need to scroll
cmd.addEventListener('focus', () => {
  if (window.innerWidth <= 640) {
    setTimeout(() => {
      log.scrollTop = log.scrollHeight;
    }, 300); // Wait for keyboard animation
  }
});

// Also scroll when visualViewport resizes (keyboard opens)
if (window.visualViewport) {
  window.visualViewport.addEventListener('resize', () => {
    if (window.innerWidth <= 640) {
      setTimeout(() => {
        log.scrollTop = log.scrollHeight;
      }, 100);
    }
  });
}

// Page lifecycle event logging and reconnection handling
// Default behavior: keep the WebSocket connected even when the tab is hidden.
// (You can restore the old "disconnect on hidden" behavior by setting
// localStorage.simpleChatCloseOnHidden = "1" in the browser console.)
const closeOnHidden = (localStorage.getItem('simpleChatCloseOnHidden') === '1');
let disconnectedByVisibility = false;

document.addEventListener('visibilitychange', () => {
  if (document.hidden) {
    logWS('INFO', '>>> Page HIDDEN (screen off or tab switched)');
    if (closeOnHidden) {
      // Optional: disconnect WebSocket to save battery/resources (legacy behavior)
      if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
        disconnectedByVisibility = true;
        stopHeartbeat();
        ws.close(1000, 'page hidden');
        logWS('INFO', 'WebSocket closed due to page hidden (closeOnHidden=1)');
      }
      // Clear any pending reconnect timer
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    } else {
      logWS('INFO', 'Keeping WebSocket open while page is hidden (closeOnHidden=0)');
    }
  } else {
    logWS('INFO', '<<< Page VISIBLE (screen on or tab switched back)');
    // Reconnect WebSocket when page becomes visible again
    if (disconnectedByVisibility || !ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
      logWS('INFO', 'Reconnecting WebSocket after page visible');
      disconnectedByVisibility = false;
      // Clear any pending reconnect timer
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      // Reset reconnect attempts for immediate retry
      reconnectAttempts = 0;
      connectWebSocket();
    }
  }
});

window.addEventListener('focus', () => {
  logWS('INFO', '>>> Window FOCUSED');
  logWS('DEBUG', 'WebSocket state on focus: ' + (ws ? ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED'][ws.readyState] : 'null'));

  // Also check connection on window focus (for mobile lock screen scenarios)
  if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
    logWS('WARN', 'Connection lost while window was unfocused, reconnecting immediately');
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    reconnectAttempts = 0;
    connectWebSocket();
  }
});

window.addEventListener('blur', () => {
  logWS('INFO', '<<< Window BLURRED');
});

window.addEventListener('beforeunload', () => {
  logWS('INFO', '========== Page UNLOADING ==========');
  logWS('INFO', 'Final stats: messages=' + messageCount + ', heartbeats=' + heartbeatCount);
  if (ws) {
    ws.close();
  }
});

window.addEventListener('online', () => {
  logWS('INFO', '🌐 Network ONLINE');

  // Network came back online - immediately try to reconnect
  if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
    logWS('INFO', 'Network restored, reconnecting immediately');
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    reconnectAttempts = 0;
    connectWebSocket();
  }
});

window.addEventListener('offline', () => {
  logWS('WARN', '🌐 Network OFFLINE');
});

// Expose debug function
window.debugWS = () => {
  console.log('========== DEBUG INFO ==========');
  console.log('WebSocket:', ws);
  console.log('ReadyState:', ws ? ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED'][ws.readyState] : 'null');
  console.log('Reconnect attempts:', reconnectAttempts);
  console.log('Reconnect timer:', reconnectTimer ? 'scheduled' : 'none');
  console.log('Heartbeat timer:', heartbeatTimer ? 'active' : 'stopped');
  console.log('Messages received:', messageCount);
  console.log('Heartbeats sent:', heartbeatCount);
  console.log('Connection age:', connectionStartTime ? Math.round((Date.now() - connectionStartTime) / 1000) + 's' : 'N/A');
  console.log('Last message:', lastMessageTime ? Math.round((Date.now() - lastMessageTime) / 1000) + 's ago' : 'N/A');
  console.log('--- Roster Info ---');
  console.log('Online users count:', onlineUsers.length);
  console.log('Online users:', onlineUsers);
  console.log('Displayed count:', usersCount ? usersCount.textContent : 'N/A');
  console.log('================================');
};

logWS('INFO', 'All event listeners registered');

// Focus command line on load
setTimeout(() => cmd.focus(), 0);