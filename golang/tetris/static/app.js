// Game constants
const COLS = 10;
const ROWS = 20;
const BLOCK_SIZE = 30;
const COLORS = [null, '#FF0D72', '#0DC2FF', '#0DFF72', '#F538FF', '#FF8E0D', '#FFE138', '#3877FF'];
const SHAPES = [
    [[1,1,1,1]], [[2,0,0],[2,2,2]], [[0,0,3],[3,3,3]], [[4,4],[4,4]],
    [[0,5,5],[5,5,0]], [[0,6,0],[6,6,6]], [[7,7,0],[0,7,7]]
];

// State
let ws = null;
let playerId = 'player_' + Math.random().toString(36).substr(2, 9);
let nickname = '';
let currentScreen = 'lobby';
let currentRoomId = null;
let isReady = false;
let isPlaying = false;
let isSpectator = false;
let waitingForGameStart = false;

// Game state
let board, currentPiece, nextPiece, score, level, lines, gameOver, paused, lastTime, dropCounter, dropInterval;

// DOM elements
const lobbyScreen = document.getElementById('lobbyScreen');
const roomScreen = document.getElementById('roomScreen');
const gameScreen = document.getElementById('gameScreen');
const lobbyStatus = document.getElementById('lobbyStatus');

const roomNameInput = document.getElementById('roomNameInput');
const nicknameInput = document.getElementById('nicknameInput');
const maxPlayersSelect = document.getElementById('maxPlayersSelect');
const createRoomBtn = document.getElementById('createRoomBtn');
const refreshRoomsBtn = document.getElementById('refreshRoomsBtn');
const roomList = document.getElementById('roomList');

const roomTitle = document.getElementById('roomTitle');
const leaveRoomBtn = document.getElementById('leaveRoomBtn');
const roomPlayerList = document.getElementById('roomPlayerList');
const readyBtn = document.getElementById('readyBtn');
const startGameBtn = document.getElementById('startGameBtn');
const chatMessages = document.getElementById('chatMessages');
const chatInput = document.getElementById('chatInput');
const sendChatBtn = document.getElementById('sendChatBtn');

const myCanvas = document.getElementById('myCanvas');
const myCtx = myCanvas.getContext('2d');
const opponentCanvas = document.getElementById('opponentCanvas');
const opponentCtx = opponentCanvas.getContext('2d');
const nextCanvas = document.getElementById('nextCanvas');
const nextCtx = nextCanvas.getContext('2d');
const scoreEl = document.getElementById('score');
const levelEl = document.getElementById('level');
const linesEl = document.getElementById('lines');
const myOverlay = document.getElementById('myOverlay');
const myOverlayText = document.getElementById('myOverlayText');
const gamePlayerList = document.getElementById('gamePlayerList');
const gameChatMessages = document.getElementById('gameChatMessages');
const gameChatInput = document.getElementById('gameChatInput');
const sendGameChatBtn = document.getElementById('sendGameChatBtn');
const player1Info = document.getElementById('player1Info');
const player2Info = document.getElementById('player2Info');

init();

function init() {
    // Load saved nickname from localStorage
    const savedNickname = localStorage.getItem('tetris_nickname');
    if (savedNickname) {
        nicknameInput.value = savedNickname;
        nickname = savedNickname;
    }

    connectWebSocket();
    setupEventListeners();
    showScreen('lobby');
}

function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const basePath = location.pathname.endsWith('/') ? location.pathname : (location.pathname + '/');
    ws = new WebSocket(protocol + '//' + window.location.host + basePath + 'ws');

    ws.onopen = () => {
        lobbyStatus.textContent = '✓ Connected';
        lobbyStatus.className = 'status connected';
        refreshRooms();
    };

    ws.onclose = () => {
        lobbyStatus.textContent = '✗ Disconnected - Reconnecting...';
        lobbyStatus.className = 'status disconnected';
        setTimeout(connectWebSocket, 2000);
    };

    ws.onmessage = (event) => {
        const msg = JSON.parse(event.data);
        handleMessage(msg);
    };
}

function handleMessage(msg) {
    switch(msg.type) {
        case 'roomList':
            displayRoomList(msg.rooms);
            break;
        case 'roomJoined':
            currentRoomId = msg.roomId;
            showScreen('room');
            break;
        case 'roomState':
            updateRoomState(msg);
            // If game started and we were waiting, now actually start the game
            if (waitingForGameStart && msg.room && msg.room.inGame) {
                waitingForGameStart = false;
                actuallyStartGame();
            }
            break;
        case 'gameStart':
            // Set flag and wait for roomState update with isPlaying info
            waitingForGameStart = true;
            showScreen('game');
            initGameState();
            break;
        case 'gameEnded':
            // Game ended (player left), return to room
            if (msg.error) {
                alert(msg.error);
            }
            isReady = false;
            isPlaying = false;
            readyBtn.textContent = 'Ready';
            readyBtn.style.background = '#667eea';
            showScreen('room');
            break;
        case 'chat':
            addChatMessage(msg);
            break;
        case 'error':
            alert(msg.error);
            break;
    }
}

function setupEventListeners() {
    createRoomBtn.onclick = createRoom;
    refreshRoomsBtn.onclick = refreshRooms;
    leaveRoomBtn.onclick = leaveRoom;
    readyBtn.onclick = toggleReady;
    startGameBtn.onclick = requestStartGame;

    // Save nickname to localStorage when it changes
    nicknameInput.oninput = () => {
        const nick = nicknameInput.value.trim();
        if (nick) {
            localStorage.setItem('tetris_nickname', nick);
        }
    };

    sendChatBtn.onclick = () => sendChat(chatInput);
    chatInput.onkeypress = (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            sendChat(chatInput);
        }
    };

    sendGameChatBtn.onclick = () => sendChat(gameChatInput);
    gameChatInput.onkeypress = (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            sendChat(gameChatInput);
        }
    };

    document.addEventListener('keydown', handleKeyDown, { passive: false, capture: false });

    // Mobile controls
    setupMobileControls();
}

function setupMobileControls() {
    const mobileLeft = document.getElementById('mobileLeft');
    const mobileRight = document.getElementById('mobileRight');
    const mobileDown = document.getElementById('mobileDown');
    const mobileRotate = document.getElementById('mobileRotate');
    const mobileHardDrop = document.getElementById('mobileHardDrop');

    // Prevent default touch behavior
    [mobileLeft, mobileRight, mobileDown, mobileRotate, mobileHardDrop].forEach(btn => {
        if (btn) {
            btn.addEventListener('touchstart', (e) => e.preventDefault());
        }
    });

    if (mobileLeft) {
        mobileLeft.addEventListener('click', () => {
            if (currentScreen === 'game' && isPlaying && !gameOver && !paused) {
                move(-1);
            }
        });
    }

    if (mobileRight) {
        mobileRight.addEventListener('click', () => {
            if (currentScreen === 'game' && isPlaying && !gameOver && !paused) {
                move(1);
            }
        });
    }

    if (mobileDown) {
        mobileDown.addEventListener('click', () => {
            if (currentScreen === 'game' && isPlaying && !gameOver && !paused) {
                drop();
                score += 1;
                updateStats();
            }
        });
    }

    if (mobileRotate) {
        mobileRotate.addEventListener('click', () => {
            if (currentScreen === 'game' && isPlaying && !gameOver && !paused) {
                rotate();
            }
        });
    }

    if (mobileHardDrop) {
        mobileHardDrop.addEventListener('click', () => {
            if (currentScreen === 'game' && isPlaying && !gameOver && !paused) {
                hardDrop();
            }
        });
    }
}

function showScreen(screen) {
    lobbyScreen.classList.add('hidden');
    roomScreen.classList.add('hidden');
    gameScreen.classList.add('hidden');

    if (screen === 'lobby') lobbyScreen.classList.remove('hidden');
    else if (screen === 'room') roomScreen.classList.remove('hidden');
    else if (screen === 'game') gameScreen.classList.remove('hidden');

    currentScreen = screen;
}

function send(msg) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(msg));
    }
}

function createRoom() {
    const roomName = roomNameInput.value.trim();
    nickname = nicknameInput.value.trim();
    const maxPlayers = parseInt(maxPlayersSelect.value);

    if (!roomName || !nickname) {
        alert('Please enter room name and nickname');
        return;
    }

    send({
        type: 'createRoom',
        roomName: roomName,
        playerId: playerId,
        nickname: nickname,
        maxPlayers: maxPlayers
    });
}

function refreshRooms() {
    send({ type: 'getRooms' });
}

function displayRoomList(rooms) {
    roomList.innerHTML = '';
    if (!rooms || rooms.length === 0) {
        roomList.innerHTML = '<p style="text-align:center;color:#6c757d;">No rooms available</p>';
        return;
    }

    rooms.forEach(room => {
        const div = document.createElement('div');
        div.className = 'room-item' + (room.inGame ? ' in-game' : '');
        div.innerHTML = `
            <div class="room-name">${room.name}</div>
            <div class="room-players">${room.playerCount}/${room.maxPlayers} players ${room.inGame ? '(In Game)' : ''}</div>
        `;
        if (!room.inGame && room.playerCount < room.maxPlayers) {
            div.onclick = () => joinRoom(room.id);
        }
        roomList.appendChild(div);
    });
}

function joinRoom(roomId) {
    nickname = nicknameInput.value.trim();
    if (!nickname) {
        alert('Please enter nickname');
        return;
    }

    send({
        type: 'joinRoom',
        roomId: roomId,
        playerId: playerId,
        nickname: nickname
    });
}

function leaveRoom() {
    send({
        type: 'leaveRoom',
        playerId: playerId
    });
    currentRoomId = null;
    isReady = false;
    showScreen('lobby');
    refreshRooms();
}

function toggleReady() {
    isReady = !isReady;
    readyBtn.textContent = isReady ? 'Not Ready' : 'Ready';
    readyBtn.style.background = isReady ? '#dc3545' : '#667eea';

    send({
        type: 'setReady',
        playerId: playerId,
        ready: isReady
    });
}

function requestStartGame() {
    send({
        type: 'startGame',
        playerId: playerId
    });
}

function updateRoomState(msg) {
    if (!msg.room) return;

    roomTitle.textContent = msg.room.name;

    // Check if current player is playing
    const me = msg.players.find(p => p.id === playerId);
    if (me) {
        isPlaying = me.isPlaying;
    }

    // Show/hide start button based on host status
    const isHost = msg.room.hostId === playerId;
    if (isHost) {
        startGameBtn.classList.remove('hidden');
        readyBtn.classList.add('hidden');
    } else {
        startGameBtn.classList.add('hidden');
        readyBtn.classList.remove('hidden');
    }

    roomPlayerList.innerHTML = '';
    msg.players.forEach(p => {
        const div = document.createElement('div');
        div.className = 'player-item ' + (p.ready ? 'ready' : 'not-ready');
        const hostBadge = p.id === msg.room.hostId ? ' 👑' : '';
        div.innerHTML = `
            <span class="player-nickname">${p.nickname}${p.id === playerId ? ' (You)' : ''}${hostBadge}</span>
            <span class="player-status">${p.ready ? '✓ Ready' : '⏳ Not Ready'}</span>
        `;
        roomPlayerList.appendChild(div);
    });

    if (currentScreen === 'game') {
        updateGamePlayerList(msg.players);
        updateMatchInfo(msg.players);
        // Draw both players' boards
        drawPlayerBoards(msg.players);
    }
}

function updateGamePlayerList(players) {
    gamePlayerList.innerHTML = '';
    players.forEach(p => {
        const div = document.createElement('div');
        div.className = 'player-item';
        const status = p.isPlaying ? '🎮 Playing' : '👁️ Spectating';
        div.innerHTML = `
            <span class="player-nickname">${p.nickname}</span>
            <span class="player-status">${status} | ${p.score}</span>
        `;
        gamePlayerList.appendChild(div);
    });
}

function updateMatchInfo(players) {
    const playing = players.filter(p => p.isPlaying);
    if (playing.length >= 2) {
        player1Info.querySelector('.player-name').textContent = playing[0].nickname;
        player1Info.querySelector('.player-score').textContent = `Score: ${playing[0].score}`;
        player2Info.querySelector('.player-name').textContent = playing[1].nickname;
        player2Info.querySelector('.player-score').textContent = `Score: ${playing[1].score}`;
    }
}

function drawPlayerBoards(players) {
    const playing = players.filter(p => p.isPlaying);

    const myGameLabel = document.getElementById('myGameLabel');
    const opponentLabel = document.getElementById('opponentGameLabel');

    if (isPlaying) {
        // I'm playing: show my game and opponent's game
        myGameLabel.textContent = 'Your Game';
        const opponent = playing.find(p => p.id !== playerId);

        if (opponent) {
            opponentLabel.textContent = opponent.nickname;

            // Clear and draw opponent canvas
            opponentCtx.fillStyle = '#1a1a2e';
            opponentCtx.fillRect(0, 0, opponentCanvas.width, opponentCanvas.height);

            if (opponent.board) {
                opponent.board.forEach((row, y) => {
                    row.forEach((value, x) => {
                        if (value) {
                            drawBlock(opponentCtx, x, y, value);
                        }
                    });
                });
            }
        }
    } else {
        // I'm spectating: show both players
        if (playing.length >= 2) {
            myGameLabel.textContent = 'Player 1';
            opponentLabel.textContent = 'Player 2';

            // Draw player 1 on my canvas
            myCtx.fillStyle = '#1a1a2e';
            myCtx.fillRect(0, 0, myCanvas.width, myCanvas.height);

            if (playing[0].board) {
                playing[0].board.forEach((row, y) => {
                    row.forEach((value, x) => {
                        if (value) {
                            drawBlock(myCtx, x, y, value);
                        }
                    });
                });
            }

            // Draw player 2 on opponent canvas
            opponentCtx.fillStyle = '#1a1a2e';
            opponentCtx.fillRect(0, 0, opponentCanvas.width, opponentCanvas.height);

            if (playing[1].board) {
                playing[1].board.forEach((row, y) => {
                    row.forEach((value, x) => {
                        if (value) {
                            drawBlock(opponentCtx, x, y, value);
                        }
                    });
                });
            }
        }
    }
}

function drawSpectatorView() {
    if (!isPlaying && currentScreen === 'game') {
        // Keep refreshing to show live updates
        requestAnimationFrame(drawSpectatorView);
    }
}

function sendChat(inputEl) {
    const text = inputEl.value.trim();
    if (!text) return;

    send({
        type: 'chat',
        playerId: playerId,
        text: text
    });

    inputEl.value = '';
}

function addChatMessage(msg) {
    const div = document.createElement('div');
    div.className = 'chat-message';
    const time = new Date(msg.timestamp * 1000).toLocaleTimeString();
    div.innerHTML = `
        <span class="chat-sender">${msg.nickname}:</span>
        <span class="chat-text">${msg.text}</span>
        <span class="chat-time">${time}</span>
    `;

    if (currentScreen === 'room') {
        chatMessages.appendChild(div);
        chatMessages.scrollTop = chatMessages.scrollHeight;
    } else if (currentScreen === 'game') {
        gameChatMessages.appendChild(div);
        gameChatMessages.scrollTop = gameChatMessages.scrollHeight;
    }
}

// Game functions
function actuallyStartGame() {
    const myGameLabel = document.getElementById('myGameLabel');
    if (isPlaying) {
        myGameLabel.textContent = 'Your Game';
        myOverlayText.textContent = 'Get Ready!';
        myOverlay.classList.remove('hidden');

        setTimeout(() => {
            myOverlay.classList.add('hidden');
            lastTime = performance.now();
            requestAnimationFrame(gameLoop);
        }, 2000);
    } else {
        myGameLabel.textContent = 'Player 1';
        myOverlayText.textContent = 'Spectating...';
        myOverlay.classList.remove('hidden');
        // Start drawing spectator view
        requestAnimationFrame(drawSpectatorView);
    }
}

function initGameState() {
    board = Array(ROWS).fill(null).map(() => Array(COLS).fill(0));
    score = 0;
    level = 1;
    lines = 0;
    gameOver = false;
    paused = false;
    dropInterval = 1000;
    dropCounter = 0;

    currentPiece = createPiece();
    nextPiece = createPiece();

    updateStats();
    drawNext();
}

function createPiece() {
    const shape = SHAPES[Math.floor(Math.random() * SHAPES.length)];
    return {
        shape: shape,
        x: Math.floor(COLS / 2) - Math.floor(shape[0].length / 2),
        y: 0
    };
}

function drawBlock(ctx, x, y, colorIndex) {
    const color = COLORS[colorIndex];
    if (!color) return;

    ctx.fillStyle = color;
    ctx.fillRect(x * BLOCK_SIZE, y * BLOCK_SIZE, BLOCK_SIZE, BLOCK_SIZE);
    ctx.strokeStyle = 'rgba(0, 0, 0, 0.3)';
    ctx.lineWidth = 2;
    ctx.strokeRect(x * BLOCK_SIZE, y * BLOCK_SIZE, BLOCK_SIZE, BLOCK_SIZE);
}

function drawBoard() {
    myCtx.fillStyle = '#1a1a2e';
    myCtx.fillRect(0, 0, myCanvas.width, myCanvas.height);

    for (let row = 0; row < ROWS; row++) {
        for (let col = 0; col < COLS; col++) {
            if (board[row][col]) {
                drawBlock(myCtx, col, row, board[row][col]);
            }
        }
    }
}

function drawPiece() {
    currentPiece.shape.forEach((row, y) => {
        row.forEach((value, x) => {
            if (value) {
                drawBlock(myCtx, currentPiece.x + x, currentPiece.y + y, value);
            }
        });
    });
}

function drawNext() {
    nextCtx.fillStyle = '#1a1a2e';
    nextCtx.fillRect(0, 0, nextCanvas.width, nextCanvas.height);

    if (nextPiece) {
        nextPiece.shape.forEach((row, y) => {
            row.forEach((value, x) => {
                if (value) {
                    const drawX = x * BLOCK_SIZE + 15;
                    const drawY = y * BLOCK_SIZE + 15;
                    nextCtx.fillStyle = COLORS[value];
                    nextCtx.fillRect(drawX, drawY, BLOCK_SIZE, BLOCK_SIZE);
                    nextCtx.strokeStyle = 'rgba(0, 0, 0, 0.3)';
                    nextCtx.lineWidth = 2;
                    nextCtx.strokeRect(drawX, drawY, BLOCK_SIZE, BLOCK_SIZE);
                }
            });
        });
    }
}

function collide(piece) {
    for (let y = 0; y < piece.shape.length; y++) {
        for (let x = 0; x < piece.shape[y].length; x++) {
            if (piece.shape[y][x]) {
                const newX = piece.x + x;
                const newY = piece.y + y;
                if (newX < 0 || newX >= COLS || newY >= ROWS) return true;
                if (newY >= 0 && board[newY][newX]) return true;
            }
        }
    }
    return false;
}

function merge() {
    currentPiece.shape.forEach((row, y) => {
        row.forEach((value, x) => {
            if (value) {
                const boardY = currentPiece.y + y;
                const boardX = currentPiece.x + x;
                if (boardY >= 0) board[boardY][boardX] = value;
            }
        });
    });
}

function rotate() {
    const newShape = currentPiece.shape[0].map((_, i) =>
        currentPiece.shape.map(row => row[i]).reverse()
    );
    const rotated = { ...currentPiece, shape: newShape };

    let offset = 0;
    while (collide(rotated)) {
        rotated.x += offset;
        offset = -(offset + (offset > 0 ? 1 : -1));
        if (offset > currentPiece.shape[0].length) return;
    }
    currentPiece = rotated;
}

function clearLines() {
    let linesCleared = 0;
    outer: for (let row = ROWS - 1; row >= 0; row--) {
        for (let col = 0; col < COLS; col++) {
            if (!board[row][col]) continue outer;
        }
        board.splice(row, 1);
        board.unshift(Array(COLS).fill(0));
        linesCleared++;
        row++;
    }

    if (linesCleared > 0) {
        lines += linesCleared;
        score += [0, 100, 300, 500, 800][linesCleared] * level;
        level = Math.floor(lines / 10) + 1;
        dropInterval = Math.max(100, 1000 - (level - 1) * 100);
        updateStats();
        // State will be sent by drop() function
    }
}

function updateStats() {
    scoreEl.textContent = score;
    levelEl.textContent = level;
    linesEl.textContent = lines;
}

function sendGameState() {
    send({
        type: 'gameState',
        playerId: playerId,
        players: [{
            score: score,
            level: level,
            gameOver: gameOver
        }],
        board: board
    });
}

function drop() {
    currentPiece.y++;
    if (collide(currentPiece)) {
        currentPiece.y--;
        merge();
        clearLines();

        currentPiece = nextPiece;
        nextPiece = createPiece();

        if (collide(currentPiece)) {
            endGame();
        }
        drawNext();

        // Send state immediately after piece lands for real-time updates
        sendGameState();
    }
    dropCounter = 0;
}

function hardDrop() {
    while (!collide(currentPiece)) {
        currentPiece.y++;
        score += 2;
    }
    currentPiece.y--;
    drop();
    updateStats();
}

function move(dir) {
    currentPiece.x += dir;
    if (collide(currentPiece)) {
        currentPiece.x -= dir;
    }
}

function endGame() {
    gameOver = true;
    sendGameState();
    myOverlayText.textContent = 'Game Over!';
    myOverlay.classList.remove('hidden');
}

function gameLoop(time = 0) {
    if (paused || gameOver || !isPlaying) return;

    const deltaTime = time - lastTime;
    lastTime = time;
    dropCounter += deltaTime;

    if (dropCounter > dropInterval) {
        drop();
    }

    drawBoard();
    drawPiece();
    requestAnimationFrame(gameLoop);
}

function handleKeyDown(e) {
    // Safety check for browser extension compatibility
    if (!e || !e.code) return;

    // Don't handle game controls if typing in any input field
    const activeEl = document.activeElement;
    if (!activeEl) return;

    if (activeEl === gameChatInput ||
        activeEl === chatInput ||
        activeEl === nicknameInput ||
        activeEl === roomNameInput ||
        activeEl.tagName === 'INPUT' ||
        activeEl.tagName === 'TEXTAREA') {
        return;
    }

    if (currentScreen !== 'game' || !isPlaying || gameOver) return;

    if (e.code === 'KeyP') {
        paused = !paused;
        if (!paused) {
            lastTime = performance.now();
            requestAnimationFrame(gameLoop);
        }
        return;
    }

    if (paused) return;

    switch (e.code) {
        case 'ArrowLeft':
            e.preventDefault();
            move(-1);
            break;
        case 'ArrowRight':
            e.preventDefault();
            move(1);
            break;
        case 'ArrowDown':
            e.preventDefault();
            drop();
            score += 1;
            updateStats();
            break;
        case 'ArrowUp':
            e.preventDefault();
            rotate();
            break;
        case 'Space':
            e.preventDefault();
            hardDrop();
            break;
    }

    drawBoard();
    drawPiece();
}
