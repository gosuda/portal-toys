const lobbyPanel = document.getElementById('lobby');
const gamePanel = document.getElementById('game-area');
const boardElement = document.getElementById('game-board');
const scoreBlackEl = document.getElementById('score-black');
const scoreWhiteEl = document.getElementById('score-white');
const statusBlackEl = document.getElementById('status-black');
const statusWhiteEl = document.getElementById('status-white');
const turnIndicator = document.getElementById('turn-indicator');
const connectionStatus = document.getElementById('connection-status');
const scoreCardBlack = document.getElementById('score-card-black');
const scoreCardWhite = document.getElementById('score-card-white');

const SIZE = 8;
const BLACK = 1;
const WHITE = 2;
let currentPlayer = BLACK;
let board = [];
let isMultiplayer = false;
let myPlayerId = 0; 
let gameActive = false;
let pollInterval = null;
let currentMode = 'othello'; 
let currentDifficulty = 'medium';

// --- Auth State ---
let currentUser = JSON.parse(localStorage.getItem('user')) || null;
let authMode = 'login'; // 'login' or 'register'
let bettingGuestId = localStorage.getItem('betting_guest_id');
if (!bettingGuestId) {
    bettingGuestId = `g${Date.now()}${Math.floor(Math.random()*10000)}`;
    localStorage.setItem('betting_guest_id', bettingGuestId);
}
let sessionGuestId = localStorage.getItem('session_guest_id');
if (!sessionGuestId) {
    sessionGuestId = `s${Date.now()}${Math.floor(Math.random()*10000)}`;
    localStorage.setItem('session_guest_id', sessionGuestId);
}
let bettingWasm = null;

async function initBettingWasm() {
    if (bettingWasm) return bettingWasm;
    try {
        const res = await fetch('/static/betting_logic.wasm');
        if (!res.ok) throw new Error('wasm fetch failed');
        const { instance } = await WebAssembly.instantiate(await res.arrayBuffer());
        bettingWasm = instance.exports;
    } catch (e) {
        console.warn('WASM unavailable, fallback to JS:', e);
        bettingWasm = {
            wasm_betting_single_delta: (amount, oddsX1000, success) => {
                const odds = oddsX1000 / 1000;
                if (!success) return -amount;
                const profitRate = odds > 1 ? (odds - 1) : (odds > 0 ? odds : 1);
                const profit = Math.floor(amount * profitRate);
                return profit > 0 ? profit : amount;
            },
            wasm_betting_can_wager: (currentPoints, amount) => (currentPoints - amount >= -1000 ? 1 : 0),
            wasm_betting_project_points: (currentPoints, delta) => Math.max(-1000, currentPoints + delta)
        };
    }
    return bettingWasm;
}

function sessionIdentityQuery() {
    const userId = currentUser ? currentUser.user_id : 0;
    return `user_id=${userId}&guest_id=${encodeURIComponent(sessionGuestId)}`;
}

async function logGameSession(sessionType, mode, difficulty = '', roomId = 0) {
    const payload = {
        session_type: sessionType,
        mode,
        difficulty,
        room_id: roomId,
        guest_id: sessionGuestId,
        user_id: currentUser ? currentUser.user_id : 0
    };
    try {
        await fetch('/sessions/log', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
    } catch (e) {
        console.error(e);
    }
}


function getNicknameColor(wins, losses) {
    const total = wins + losses;
    if (total === 0) return 'var(--text-primary)';
    const winRate = wins / total;
    // High win rate (1.0) -> Red (0, 100, 50) in HSL
    // Low win rate (0.0) -> Purple (280, 100, 50) in HSL
    const hue = 280 - (winRate * 280);
    return `hsl(${hue}, 80%, 60%)`;
}

function updateAuthUI() {
    const authControls = document.getElementById('auth-controls');
    const userDisplay = document.getElementById('user-display');
    const displayUsername = document.getElementById('display-username');

    if (currentUser) {
        authControls.classList.add('hidden');
        userDisplay.classList.remove('hidden');
        displayUsername.innerText = currentUser.username;
        // Fetch user info to get latest stats for color
        fetchUserInfo(currentUser.user_id);
    } else {
        authControls.classList.remove('hidden');
        userDisplay.classList.add('hidden');
    }
}

async function fetchUserInfo(userId) {
    try {
        const res = await fetch(`/user_info?user_id=${userId}`);
        if (res.ok) {
            const data = await res.json();
            const color = getNicknameColor(data.wins, data.losses);
            document.getElementById('display-username').style.color = color;
        }
    } catch (e) { console.error(e); }
}

function showAuthModal() {
    document.getElementById('auth-modal').classList.remove('hidden');
    authMode = 'login';
    updateAuthModalUI();
}

function hideAuthModal() {
    document.getElementById('auth-modal').classList.add('hidden');
}

function toggleAuthMode() {
    authMode = (authMode === 'login') ? 'register' : 'login';
    updateAuthModalUI();
}

function updateAuthModalUI() {
    const title = document.getElementById('auth-title');
    const submitBtn = document.getElementById('auth-submit');
    const switchLink = document.querySelector('.auth-switch');

    if (authMode === 'login') {
        title.innerText = 'Login';
        submitBtn.innerText = 'Login';
        switchLink.innerHTML = `Don't have an account? <a href="#" onclick="toggleAuthMode()">Register</a>`;
    } else {
        title.innerText = 'Register';
        submitBtn.innerText = 'Register';
        switchLink.innerHTML = `Already have an account? <a href="#" onclick="toggleAuthMode()">Login</a>`;
    }
    document.getElementById('auth-error').classList.add('hidden');
}

async function handleAuthSubmit() {
    const user = document.getElementById('auth-username').value;
    const pass = document.getElementById('auth-password').value;
    const errorEl = document.getElementById('auth-error');

    if (!user || !pass) {
        errorEl.innerText = "Please fill all fields";
        errorEl.classList.remove('hidden');
        return;
    }

    try {
        const endpoint = authMode === 'login' ? '/login' : '/register';
        const res = await fetch(endpoint, {
            method: 'POST',
            body: JSON.stringify({ username: user, password: pass })
        });

        const data = await res.json();
        if (res.ok) {
            if (authMode === 'login') {
                currentUser = data;
                localStorage.setItem('user', JSON.stringify(data));
                updateAuthUI();
                hideAuthModal();
                refreshSessionLists();
            } else {
                alert("Registration successful! Please login.");
                authMode = 'login';
                updateAuthModalUI();
            }
        } else {
            errorEl.innerText = data.error || "An error occurred";
            errorEl.classList.remove('hidden');
        }
    } catch (e) {
        errorEl.innerText = "Connection failed";
        errorEl.classList.remove('hidden');
    }
}

function logout() {
    currentUser = null;
    localStorage.removeItem('user');
    updateAuthUI();
    refreshSessionLists();
}

// --- View Management ---
function showView(viewId) {
    document.querySelectorAll('.view-container').forEach(v => v.classList.add('hidden'));
    const targetView = document.getElementById(viewId);
    if (!targetView) return;
    targetView.classList.remove('hidden');

    document.querySelectorAll('.nav-links a').forEach(a => a.classList.remove('active'));
    const map = {
        lobby: 'nav-play',
        rankings: 'nav-rank',
        'betting-zone': 'nav-betting'
    };
    const navId = map[viewId] || 'nav-play';
    const activeLink = document.getElementById(navId);
    if (activeLink) activeLink.classList.add('active');
}


async function refreshSessionLists() {
    const singleEl = document.getElementById('single-session-list');
    const multiEl = document.getElementById('multi-session-list');
    try {
        const singleRes = await fetch(`/sessions?type=singleplayer&limit=8&${sessionIdentityQuery()}`);
        const singleData = await singleRes.json();
        const singleSessions = Array.isArray(singleData.sessions) ? singleData.sessions : [];
        if (!singleSessions.length) {
            singleEl.innerText = 'No singleplayer session yet';
        } else {
            singleEl.innerText = singleSessions.map((s) => `${s.mode}${s.difficulty ? ` (${s.difficulty})` : ''}`).join(', ');
        }

        const multiRes = await fetch(`/sessions?type=multiplayer&limit=8&${sessionIdentityQuery()}`);
        const multiData = await multiRes.json();
        const rooms = Array.isArray(multiData.sessions) ? multiData.sessions : [];
        if (!rooms.length) {
            multiEl.innerText = 'No multiplayer room yet';
        } else {
            multiEl.innerText = rooms.map((r) => `#${r.room_id || 0} ${r.mode}`).join(' | ');
        }
    } catch (e) {
        singleEl.innerText = 'Failed to load singleplayer sessions';
        multiEl.innerText = 'Failed to load multiplayer sessions';
    }
}

async function showRankings() {
    showView('rankings');
    const body = document.getElementById('rankings-body');
    body.innerHTML = '<tr><td colspan="5" style="text-align:center">Loading...</td></tr>';

    try {
        const res = await fetch('/rankings');
        const data = await res.json();
        body.innerHTML = '';
        data.forEach((user, index) => {
            const tr = document.createElement('tr');
            const total = user.wins + user.losses;
            const rate = total > 0 ? ((user.wins / total) * 100).toFixed(1) + '%' : '0%';
            const color = getNicknameColor(user.wins, user.losses);
            
            tr.innerHTML = `
                <td><span class="rank-val">#${index + 1}</span></td>
                <td><span style="font-weight:700; color:${color}">${user.username}</span></td>
                <td>${user.wins}</td>
                <td>${user.losses}</td>
                <td>${rate}</td>
            `;
            body.appendChild(tr);
        });
    } catch (e) {
        body.innerHTML = '<tr><td colspan="5" style="text-align:center; color:red">Failed to load rankings</td></tr>';
    }
}

// Update UI on load
document.addEventListener('DOMContentLoaded', () => {
    updateAuthUI();
    refreshSessionLists();
    initBettingWasm();
    const bettingNav = document.getElementById('nav-betting');
    if (bettingNav) {
        bettingNav.addEventListener('click', (e) => {
            e.preventDefault();
            showBettingZone();
        });
    }
    const mpAmount = document.getElementById('mp-bet-amount');
    const mpRoom = document.getElementById('mp-bet-room');
    if (mpAmount) mpAmount.addEventListener('input', updateMultiplayerPreview);
    if (mpRoom) mpRoom.addEventListener('input', loadMultiplayerBetHistory);
});

const directions = [
    [-1, -1], [-1, 0], [-1, 1],
    [0, -1],           [0, 1],
    [1, -1],  [1, 0],  [1, 1]
];

// --- Core Game Logic ---

function initGame(mode) {
    currentMode = mode;
    board = Array(SIZE).fill(null).map(() => Array(SIZE).fill(0));
    
    if (mode === 'othello') {
        board[3][3] = WHITE;
        board[3][4] = BLACK;
        board[4][3] = BLACK;
        board[4][4] = WHITE;
    }
    
    currentPlayer = BLACK;
    gameActive = true;
    
    // UI Switch
    lobbyPanel.classList.add('hidden');
    gamePanel.classList.remove('hidden');
    
    renderBoard();
    updateUI();
}

function exitGame(skipNotify = false) {
    if (isMultiplayer) {
        if (!skipNotify) {
            const roomId = document.getElementById('room-input').value;
            const userId = currentUser ? currentUser.user_id : 0;
            fetch(`/leave?room=${roomId}&player_id=${myPlayerId}&user_id=${userId}`, { method: 'POST' }).catch(console.error);
        }
        isMultiplayer = false;
        myPlayerId = 0;
    }
    gameActive = false;
    if (pollInterval) clearInterval(pollInterval);
    gamePanel.classList.add('hidden');
    lobbyPanel.classList.remove('hidden');
    connectionStatus.innerText = "";
}

function getValidMoves(player, targetBoard = board) {
    if (currentMode === 'reversi') {
        let pieces = 0;
        for(let r=0;r<SIZE;r++) for(let c=0;c<SIZE;c++) if(targetBoard[r][c]!==0) pieces++;
        if (pieces < 4) {
            const moves = [];
            const center = [ [3,3], [3,4], [4,3], [4,4] ];
            for (let p of center) {
                if (targetBoard[p[0]][p[1]] === 0) moves.push({r: p[0], c: p[1]});
            }
            return moves;
        }
    }

    const moves = [];
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            if (isValidMove(r, c, player, targetBoard)) {
                moves.push({r, c});
            }
        }
    }
    return moves;
}

function isValidMove(r, c, player, targetBoard = board) {
    if (targetBoard[r][c] !== 0) return false;

    if (currentMode === 'reversi') {
        let pieces = 0;
        for(let r=0;r<SIZE;r++) for(let c=0;c<SIZE;c++) if(targetBoard[r][c]!==0) pieces++;
        if (pieces < 4) {
             return (r>=3 && r<=4 && c>=3 && c<=4);
        }
    }

    const opponent = player === BLACK ? WHITE : BLACK;
    
    for (const [dr, dc] of directions) {
        let nr = r + dr;
        let nc = c + dc;
        let piecesToFlip = 0;

        while (nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && targetBoard[nr][nc] === opponent) {
            nr += dr;
            nc += dc;
            piecesToFlip++;
        }

        if (piecesToFlip > 0 && nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && targetBoard[nr][nc] === player) {
            return true;
        }
    }
    return false;
}

function makeMoveLocal(r, c) {
    if (!gameActive) return;
    if (isMultiplayer && currentPlayer !== myPlayerId) return; 
    if (!isMultiplayer && currentPlayer === WHITE) return; 

    if (!isValidMove(r, c, currentPlayer)) return;

    if (isMultiplayer) {
        sendMove(r, c);
        // Optimistic UI update could go here, but for now we wait for server
        return; 
    }

    applyMove(r, c, currentPlayer);
    
    const opponent = currentPlayer === BLACK ? WHITE : BLACK;
    currentPlayer = opponent;
    
    if (checkTurn(opponent)) {
        if (!isMultiplayer && opponent === WHITE) {
            updateUI(); // Show "Bot thinking"
            setTimeout(botTurn, 600);
        }
    }
}

function applyMove(r, c, player, targetBoard = board) {
    targetBoard[r][c] = player;
    
    if (currentMode === 'reversi') {
        let pieces = 0;
        for(let r=0;r<SIZE;r++) for(let c=0;c<SIZE;c++) if(targetBoard[r][c]!==0) pieces++;
        if (pieces <= 4) { 
             if (targetBoard === board) {
                 renderBoard();
                 updateUI();
             }
             return;
        }
    }

    const opponent = player === BLACK ? WHITE : BLACK;

    for (const [dr, dc] of directions) {
        let nr = r + dr;
        let nc = c + dc;
        let path = [];

        while (nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && targetBoard[nr][nc] === opponent) {
            path.push({r: nr, c: nc});
            nr += dr;
            nc += dc;
        }

        if (path.length > 0 && nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && targetBoard[nr][nc] === player) {
            for (const cell of path) {
                targetBoard[cell.r][cell.c] = player;
            }
        }
    }
    if (targetBoard === board) {
        renderBoard();
        updateUI();
    }
}

function checkTurn(player) {
    const moves = getValidMoves(player);
    if (moves.length === 0) {
        const opponent = player === BLACK ? WHITE : BLACK;
        const opponentMoves = getValidMoves(opponent);
        if (opponentMoves.length === 0) {
            gameActive = false;
            const scores = countPieces();
            let winner = scores.black > scores.white ? "Black Wins" : (scores.white > scores.black ? "White Wins" : "Tie");
            turnIndicator.innerText = "Game Over - " + winner;
            return false;
        } else {
            if (!isMultiplayer) {
                // Pass turn
                currentPlayer = opponent;
                if (opponent === WHITE) {
                    setTimeout(botTurn, 600);
                }
                renderBoard();
                updateUI();
            }
            return false;
        }
    }
    return true;
}

function countPieces(targetBoard = board) {
    let black = 0;
    let white = 0;
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            if (targetBoard[r][c] === BLACK) black++;
            else if (targetBoard[r][c] === WHITE) white++;
        }
    }
    return { black, white };
}

function updateUI() {
    const scores = countPieces();
    
    // Animate numbers if possible (simple replacement for now)
    scoreBlackEl.innerText = scores.black;
    scoreWhiteEl.innerText = scores.white;
    
    // Highlight Active Player
    if (currentPlayer === BLACK) {
        scoreCardBlack.classList.add('active');
        scoreCardWhite.classList.remove('active');
        statusBlackEl.innerText = isMultiplayer && myPlayerId === BLACK ? "Your Turn" : "Thinking...";
        statusWhiteEl.innerText = "Waiting";
    } else {
        scoreCardBlack.classList.remove('active');
        scoreCardWhite.classList.add('active');
        statusWhiteEl.innerText = isMultiplayer && myPlayerId === WHITE ? "Your Turn" : "Thinking...";
        statusBlackEl.innerText = "Waiting";
    }

    let turnText = `${currentPlayer === BLACK ? "Black" : "White"}'s Turn`;
    if (isMultiplayer) {
        if (currentPlayer === myPlayerId) turnText = "Your Turn";
        else turnText = "Opponent's Turn";
    } else if (currentPlayer === WHITE) {
        turnText = "AI is thinking...";
    }
    turnIndicator.innerText = turnText;
}

// --- Bot AI Logic ---
const weights = [
    [100, -20, 10,  5,  5, 10, -20, 100],
    [-20, -50, -2, -2, -2, -2, -50, -20],
    [ 10,  -2, -1, -1, -1, -1,  -2,  10],
    [  5,  -2, -1, -1, -1, -1,  -2,   5],
    [  5,  -2, -1, -1, -1, -1,  -2,   5],
    [ 10,  -2, -1, -1, -1, -1,  -2,  10],
    [-20, -50, -2, -2, -2, -2, -50, -20],
    [100, -20, 10,  5,  5, 10, -20, 100]
];

const precisionWeights = [
    [100, -25,  10,   5,   5,  10, -25, 100],
    [-25, -40,  -3,  -3,  -3,  -3, -40, -25],
    [ 10,  -3,   2,   2,   2,   2,  -3,  10],
    [  5,  -3,   2,   1,   1,   2,  -3,   5],
    [  5,  -3,   2,   1,   1,   2,  -3,   5],
    [ 10,  -3,   2,   2,   2,   2,  -3,  10],
    [-25, -40,  -3,  -3,  -3,  -3, -40, -25],
    [100, -25,  10,   5,   5,  10, -25, 100]
];

function evaluateBoard(targetBoard, player) {
    let score = 0;
    const opponent = (player === BLACK) ? WHITE : BLACK;
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            if (targetBoard[r][c] === player) score += precisionWeights[r][c];
            else if (targetBoard[r][c] === opponent) score -= precisionWeights[r][c];
        }
    }
    return score;
}

function minimax(targetBoard, depth, isMaximizing, player) {
    const activePlayer = isMaximizing ? player : (player === BLACK ? WHITE : BLACK);
    const moves = getValidMoves(activePlayer, targetBoard);

    if (depth === 0 || moves.length === 0) {
        return evaluateBoard(targetBoard, player);
    }

    if (isMaximizing) {
        let maxEval = -Infinity;
        for (const move of moves) {
            const newBoard = targetBoard.map(row => [...row]);
            applyMove(move.r, move.c, player, newBoard);
            const evalScore = minimax(newBoard, depth - 1, false, player);
            maxEval = Math.max(maxEval, evalScore);
        }
        return maxEval;
    } else {
        let minEval = Infinity;
        const opponent = (player === BLACK) ? WHITE : BLACK;
        for (const move of moves) {
            const newBoard = targetBoard.map(row => [...row]);
            applyMove(move.r, move.c, opponent, newBoard);
            const evalScore = minimax(newBoard, depth - 1, true, player);
            minEval = Math.min(minEval, evalScore);
        }
        return minEval;
    }
}

function getBestMoveHeuristic(moves) {
    let bestMove = moves[0];
    let bestScore = -Infinity;
    for (const m of moves) {
        let score = weights[m.r][m.c];
        if (score > bestScore) {
            bestScore = score;
            bestMove = m;
        }
    }
    return bestMove;
}

function botTurn() {
    if (!gameActive || isMultiplayer) return;
    
    const moves = getValidMoves(WHITE);
    if (moves.length === 0) return;

    let move;
    if (currentDifficulty === 'easy') {
        move = Math.random() < 0.2 ? moves[Math.floor(Math.random() * moves.length)] : getBestMoveHeuristic(moves);
    } else if (currentDifficulty === 'medium') {
        move = getBestMoveHeuristic(moves);
    } else {
        let bestScore = -Infinity;
        move = moves[0];
        for (const m of moves) {
            const nextBoard = board.map(row => [...row]);
            applyMove(m.r, m.c, WHITE, nextBoard);
            const score = minimax(nextBoard, 3, false, WHITE);
            if (score > bestScore) {
                bestScore = score;
                move = m;
            }
        }
    }

    applyMove(move.r, move.c, WHITE);
    currentPlayer = BLACK;
    checkTurn(BLACK);
}

// --- Render ---

function renderBoard() {
    // Only update changed cells to prevent flicker (Virtual DOM-lite)
    // Actually, simpler is to just rebuild, but with CSS animations it looks fine.
    // To enable CSS animations (pop-in), we need to not destroy elements if not needed.
    
    // For this prototype, I'll clear and rebuild because mapping DOM to Array indices is fast enough.
    boardElement.innerHTML = '';
    
    const canPlay = gameActive && (!isMultiplayer || currentPlayer === myPlayerId) && (isMultiplayer || currentPlayer === BLACK);
    const validMoves = canPlay ? getValidMoves(currentPlayer) : [];

    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            const cell = document.createElement('div');
            cell.className = 'cell';
            cell.onclick = () => makeMoveLocal(r, c);

            const val = board[r][c];
            if (val !== 0) {
                const disc = document.createElement('div');
                disc.className = `disc ${val === BLACK ? 'black' : 'white'}`;
                cell.appendChild(disc);
            } else {
                const move = validMoves.find(m => m.r === r && m.c === c);
                if (move) {
                    const hint = document.createElement('div');
                    hint.className = 'valid-move';
                    cell.appendChild(hint);
                }
            }
            boardElement.appendChild(cell);
        }
    }
}

// --- Multiplayer ---

function startLocalGame(mode) {
    isMultiplayer = false;
    myPlayerId = 0;
    currentDifficulty = document.getElementById('difficulty-select').value;
    logGameSession('singleplayer', mode, currentDifficulty, 0);
    refreshSessionLists();
    if (pollInterval) clearInterval(pollInterval);
    initGame(mode);
}

async function startMultiplayerGame(mode) {
    const roomId = document.getElementById('room-input').value;
    const userId = currentUser ? currentUser.user_id : 0;
    // status feedback
    const btn = (event && event.target) ? event.target : null;
    const originalText = btn ? btn.innerText : "";
    if (btn) {
        btn.innerText = "Connecting...";
        btn.disabled = true;
    }

    try {
        const res = await fetch(`/join?room=${roomId}&mode=${mode}&user_id=${userId}`, { method: 'POST' });
        if (res.status === 403) throw new Error("Room is full");
        if (!res.ok) throw new Error("Connection failed");
        
        const data = await res.json();
        
        myPlayerId = data.player_id;
        currentMode = data.mode;
        isMultiplayer = true;

        board = Array(SIZE).fill(null).map(() => Array(SIZE).fill(0));
        
        lobbyPanel.classList.add('hidden');
        gamePanel.classList.remove('hidden');
        
        pollInterval = setInterval(() => pollState(roomId), 1000);
        pollState(roomId);
        logGameSession('multiplayer', currentMode, '', parseInt(roomId, 10) || 0);
        refreshSessionLists();
        
    } catch (e) {
        alert(e.message);
    } finally {
        if (btn) {
            btn.innerText = originalText;
            btn.disabled = false;
        }
    }
}

async function pollState(roomId) {
    try {
        const res = await fetch(`/state?room=${roomId}`);
        if (!res.ok) return;
        const data = await res.json();
        
        if (data.status === "timed_out") {
            alert("Game timed out due to 10 minutes of inactivity.");
            exitGame();
            return;
        }

        if (gameActive && data.status === "waiting") {
            alert("Opponent has left the game. Room reset.");
            exitGame(true);
            return;
        }

        if (data.status === "active" || data.status === "finished") {
            updateBoardFromState(data.board);
            currentPlayer = data.turn;
            gameActive = (data.status === "active");
            renderBoard();
            updateUI();
            
            if (data.status === "finished") {
                clearInterval(pollInterval);
                turnIndicator.innerText = "Game Over!";
            }
        }
    } catch (e) { console.error(e); }
}

function updateBoardFromState(flatBoard) {
    // Check if board changed to avoid unnecessary renders if we did vdom
    // For now just update model
    let idx = 0;
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            board[r][c] = flatBoard[idx++];
        }
    }
}

async function sendMove(r, c) {
    const roomId = document.getElementById('room-input').value;
    try {
        await fetch(`/move?room=${roomId}`, { 
            method: 'POST',
            body: JSON.stringify({ r, c, player: myPlayerId })
        });
        pollState(roomId);
    } catch (e) { console.error(e); }
}


function showBettingZone() {
    showView('betting-zone');
    loadBettingZone();
}

async function loadBettingZone() {
    try {
        await initBettingWasm();
        const userId = currentUser ? currentUser.user_id : 0;
        const enterRes = await fetch(`/betting/enter?user_id=${userId}&guest_id=${bettingGuestId}`);
        if (!enterRes.ok) throw new Error('betting-enter-failed');
        const enterData = await enterRes.json();
        const currentPoints = Number(enterData.points);
        document.getElementById('betting-points').innerText = currentPoints;

        const slotsRes = await fetch('/betting/slots');
        if (!slotsRes.ok) throw new Error('betting-slots-failed');
        const slotsData = await slotsRes.json();
        const rankingRes = await fetch('/betting/rankings');
        if (!rankingRes.ok) throw new Error('betting-rankings-failed');
        const rankingData = await rankingRes.json();
        const container = document.getElementById('betting-slots');
        const rankBody = document.getElementById('betting-rankings-body');
        container.innerHTML = '';
        rankBody.innerHTML = '';

        const slots = Array.isArray(slotsData.slots) ? slotsData.slots : [];
        if (slots.length === 0) {
            const empty = document.createElement('div');
            empty.style.opacity = '0.8';
            empty.innerText = 'No betting slots right now.';
            container.appendChild(empty);
            return;
        }

        slots.forEach((slot) => {
            const slotId = Number(slot.slot_id);
            const oddsWin = Number(slot.odds_win);
            const oddsLose = Number(slot.odds_lose);
            const oddsDraw = Number(slot.odds_draw);

            if (!Number.isFinite(slotId) || !Number.isFinite(oddsWin) || !Number.isFinite(oddsLose) || !Number.isFinite(oddsDraw)) {
                return;
            }

            const row = document.createElement('div');
            row.style.border = '1px solid var(--border-color)';
            row.style.padding = '0.75rem';
            row.style.borderRadius = '0.6rem';
            row.innerHTML = `
                <div style="display:flex;justify-content:space-between;flex-wrap:wrap;gap:0.5rem;">
                    <strong>Slot #${slotId} (${slot.difficulty || 'unknown'})</strong>
                    <span>odds W:${oddsWin.toFixed(2)} / L:${oddsLose.toFixed(2)} / D:${oddsDraw.toFixed(2)}</span>
                </div>
                <div style="margin-top:0.5rem;display:flex;gap:0.4rem;align-items:center;flex-wrap:wrap;">
                    <input type="number" min="1" value="100" id="bet-amount-${slotId}" style="max-width:100px;">
                    <button class="btn btn-primary btn-sm" onclick="placeBet(${slotId}, 'win')">Win</button>
                    <button class="btn btn-outline btn-sm" onclick="placeBet(${slotId}, 'lose')">Lose</button>
                    <button class="btn btn-outline btn-sm" onclick="placeBet(${slotId}, 'draw')">Draw</button>
                </div>
                <div id="bet-preview-${slotId}" style="margin-top:0.5rem;font-size:0.82rem;color:var(--text-secondary);"></div>
            `;
            container.appendChild(row);
            const amountInput = row.querySelector(`#bet-amount-${slotId}`);
            const preview = row.querySelector(`#bet-preview-${slotId}`);
            const updatePreview = () => {
                const amount = Number(amountInput.value || 0);
                if (amount <= 0) {
                    preview.innerText = '금액 입력 필요';
                    return;
                }
                const deltaWin = bettingWasm.wasm_betting_single_delta(amount, Math.round(oddsWin * 1000), 1);
                const projectedWin = bettingWasm.wasm_betting_project_points(currentPoints, deltaWin);
                const canBet = bettingWasm.wasm_betting_can_wager(currentPoints, amount) === 1;
                preview.innerText = canBet
                    ? `Win 적중시 Δ${deltaWin >= 0 ? '+' : ''}${deltaWin}, 예상 포인트 ${projectedWin}`
                    : '현재 포인트 기준으로 이 금액은 베팅 불가';
            };
            amountInput.addEventListener('input', updatePreview);
            updatePreview();
        });

        if (!container.children.length) {
            const invalid = document.createElement('div');
            invalid.style.opacity = '0.8';
            invalid.innerText = 'Betting slots are temporarily unavailable.';
            container.appendChild(invalid);
        }

        const rows = Array.isArray(rankingData.rankings) ? rankingData.rankings : [];
        if (!rows.length) {
            rankBody.innerHTML = '<tr><td colspan="3" style="padding:8px;">No data</td></tr>';
        } else {
            rows.forEach((row, idx) => {
                const tr = document.createElement('tr');
                tr.innerHTML = `
                    <td style="padding:8px;">#${idx + 1}</td>
                    <td style="padding:8px;">${row.identity}</td>
                    <td style="padding:8px;">${row.points}</td>
                `;
                rankBody.appendChild(tr);
            });
        }
        await loadMultiplayerBetHistory();
        updateMultiplayerPreview();
    } catch (e) {
        console.error('loadBettingZone failed:', e);
        alert('Failed to load betting zone');
    }
}

async function placeBet(slotId, outcome) {
    const amount = parseInt(document.getElementById(`bet-amount-${slotId}`).value || '0', 10);
    if (amount <= 0) {
        alert('Enter valid amount');
        return;
    }

    const payload = {
        slot_id: slotId,
        outcome,
        amount,
        guest_id: bettingGuestId,
        user_id: currentUser ? currentUser.user_id : 0
    };

    const res = await fetch('/betting/place', {
        method: 'POST',
        body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
        alert(data.error || 'Bet failed');
        return;
    }

    alert(`${data.success ? 'Success' : 'Fail'} | result=${data.result} | delta=${data.delta} | points=${data.points}`);
    document.getElementById('betting-points').innerText = data.points;
    loadBettingZone();
}

async function placeMultiplayerBet(targetPlayer) {
    const roomInput = document.getElementById('mp-bet-room');
    const amountInput = document.getElementById('mp-bet-amount');
    const roomId = parseInt(roomInput.value || document.getElementById('room-input').value || '0', 10);
    const amount = parseInt(amountInput.value || '0', 10);
    if (roomId <= 0 || amount <= 0) {
        alert('Enter valid room and amount');
        return;
    }

    const payload = {
        room_id: roomId,
        target_player: targetPlayer,
        amount,
        guest_id: bettingGuestId,
        user_id: currentUser ? currentUser.user_id : 0
    };
    const res = await fetch('/betting/multiplayer/place', {
        method: 'POST',
        body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
        alert(data.error || 'Multiplayer bet failed');
        return;
    }
    document.getElementById('betting-points').innerText = data.points;
    alert(`Room ${data.room_id} | P${data.target_player} | amount=${data.amount} | points=${data.points}`);
    loadBettingZone();
}

function updateMultiplayerPreview() {
    const points = Number(document.getElementById('betting-points').innerText || '0');
    const amount = Number(document.getElementById('mp-bet-amount')?.value || '0');
    const previewEl = document.getElementById('mp-bet-preview');
    if (!previewEl) return;
    if (amount <= 0) {
        previewEl.innerText = '베팅 금액을 입력하세요.';
        return;
    }
    const canBet = bettingWasm && bettingWasm.wasm_betting_can_wager(points, amount) === 1;
    const nextPoints = bettingWasm ? bettingWasm.wasm_betting_project_points(points, -amount) : (points - amount);
    previewEl.innerText = canBet
        ? `베팅 직후 예상 포인트: ${nextPoints} (정산 후 변동)`
        : '이 금액은 허용 최소 포인트(-1000)를 초과합니다.';
}

async function loadMultiplayerBetHistory() {
    const roomId = parseInt(document.getElementById('mp-bet-room')?.value || '0', 10);
    const userId = currentUser ? currentUser.user_id : 0;
    const q = `/betting/multiplayer/history?user_id=${userId}&guest_id=${encodeURIComponent(bettingGuestId)}${roomId > 0 ? `&room_id=${roomId}` : ''}`;
    const res = await fetch(q);
    const data = await res.json();
    const body = document.getElementById('mp-bet-history-body');
    body.innerHTML = '';
    const rows = Array.isArray(data.bets) ? data.bets : [];
    if (!rows.length) {
        body.innerHTML = '<tr><td colspan="4" style="padding:6px;">No bets</td></tr>';
        return;
    }
    rows.forEach((bet) => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
            <td style="padding:6px;">${bet.room_id}</td>
            <td style="padding:6px;">P${bet.target_player}</td>
            <td style="padding:6px;">${bet.amount}</td>
            <td style="padding:6px;">${bet.settled ? 'Yes' : 'No'}</td>
        `;
        body.appendChild(tr);
    });
}

// --- Theme Management ---
const themeToggle = document.getElementById('theme-toggle');
const html = document.documentElement;

function setTheme(theme) {
    html.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
}

// Initial Theme Load
const savedTheme = localStorage.getItem('theme');
if (savedTheme) {
    setTheme(savedTheme);
} else if (window.matchMedia('(prefers-color-scheme: light)').matches) {
    setTheme('light');
}

themeToggle.addEventListener('click', () => {
    const currentTheme = html.getAttribute('data-theme');
    setTheme(currentTheme === 'light' ? 'dark' : 'light');
});

// Initial state injection
if (window.initialState) {
    const s = window.initialState;
    if (s.roomId) {
        document.getElementById('room-input').value = s.roomId;
        currentMode = s.mode;
        currentPlayer = s.turn;
        
        if (s.board && s.board.length > 0) {
            updateBoardFromState(s.board);
        }
        
        lobbyPanel.classList.add('hidden');
        gamePanel.classList.remove('hidden');
        gameActive = s.status === 'active';
        isMultiplayer = true;
        
        renderBoard(); // Hydrate board
        updateUI();
        
        pollInterval = setInterval(() => pollState(s.roomId), 1000);
    }
}
