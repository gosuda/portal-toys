const lobbyPanel = document.getElementById('lobby');
const gamePanel = document.getElementById('game-area');
const boardElement = document.getElementById('game-board');
const scoreBlackEl = document.querySelector('#score-black .score-value');
const scoreWhiteEl = document.querySelector('#score-white .score-value');
const turnIndicator = document.getElementById('turn-indicator');
const connectionStatus = document.getElementById('connection-status');

const SIZE = 8;
const BLACK = 1;
const WHITE = 2;
let currentPlayer = BLACK;
let board = [];
let isMultiplayer = false;
let myPlayerId = 0; 
let gameActive = false;
let pollInterval = null;
let currentMode = 'othello'; // 'othello' or 'reversi'

// Directions: [row, col]
const directions = [
    [-1, -1], [-1, 0], [-1, 1],
    [0, -1],           [0, 1],
    [1, -1],  [1, 0],  [1, 1]
];

let currentDifficulty = 'medium';

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

function exitGame() {
    gameActive = false;
    if (pollInterval) clearInterval(pollInterval);
    gamePanel.classList.add('hidden');
    lobbyPanel.classList.remove('hidden');
    connectionStatus.innerText = "";
}

function getValidMoves(player, targetBoard = board) {
    // Special setup phase for Reversi Mode (Turn 1-4)
    if (currentMode === 'reversi') {
        let pieces = 0;
        for(let r=0;r<SIZE;r++) for(let c=0;c<SIZE;c++) if(targetBoard[r][c]!==0) pieces++;
        if (pieces < 4) {
            // Allow placement in center 2x2
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

    // Special setup phase for Reversi Mode
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
    if (!isMultiplayer && currentPlayer === WHITE) return; // Wait for bot

    if (!isValidMove(r, c, currentPlayer)) return;

    if (isMultiplayer) {
        sendMove(r, c);
        return; 
    }

    applyMove(r, c, currentPlayer);
    
    const opponent = currentPlayer === BLACK ? WHITE : BLACK;
    currentPlayer = opponent;
    
    if (checkTurn(opponent)) {
        if (!isMultiplayer && opponent === WHITE) {
            setTimeout(botTurn, 600);
        }
    }
}

function applyMove(r, c, player, targetBoard = board) {
    targetBoard[r][c] = player;
    
    // Special setup phase for Reversi Mode: No flips
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
            let winner = scores.black > scores.white ? "Black" : (scores.white > scores.black ? "White" : "Tie");
            setTimeout(() => alert(`Game Over! Winner: ${winner}`), 100);
            return false;
        } else {
            if (!isMultiplayer) {
                setTimeout(() => {
                    alert((player === BLACK ? "Black" : "White") + " has no moves! Passing turn.");
                    currentPlayer = opponent;
                    if (opponent === WHITE) {
                        setTimeout(botTurn, 600);
                    }
                    renderBoard();
                    updateUI();
                }, 100);
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
    scoreBlackEl.innerText = scores.black;
    scoreWhiteEl.innerText = scores.white;
    
    let turnText = `${currentPlayer === BLACK ? "Black" : "White"}'s Turn`;
    if (isMultiplayer) {
        if (currentPlayer === myPlayerId) turnText = "YOUR TURN (" + (myPlayerId===BLACK?"Black":"White") + ")";
        else turnText = "OPPONENT'S TURN";
    } else if (currentPlayer === WHITE) {
        turnText = "BOT IS THINKING...";
    }
    turnIndicator.innerText = turnText;
    turnIndicator.style.color = currentPlayer === BLACK ? "#bdc3c7" : "#f1c40f"; 
}

// --- Bot AI Logic ---

function botTurn() {
    if (!gameActive || isMultiplayer) return;
    
    const moves = getValidMoves(WHITE);
    if (moves.length === 0) return;

    let move;
    if (currentDifficulty === 'easy') {
        move = moves[Math.floor(Math.random() * moves.length)];
    } else if (currentDifficulty === 'medium') {
        move = getBestMoveHeuristic(moves);
    } else {
        move = getBestMoveMinimax(moves);
    }

    applyMove(move.r, move.c, WHITE);
    currentPlayer = BLACK;
    checkTurn(BLACK);
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

function getBestMoveMinimax(moves) {
    let bestMove = moves[0];
    let bestValue = -Infinity;
    
    for (const m of moves) {
        const boardCopy = board.map(row => [...row]);
        applyMove(m.r, m.c, WHITE, boardCopy);
        let value = minimax(boardCopy, 3, -Infinity, Infinity, false);
        if (value > bestValue) {
            bestValue = value;
            bestMove = m;
        }
    }
    return bestMove;
}

function minimax(targetBoard, depth, alpha, beta, isMaximizing) {
    if (depth === 0) {
        return evaluateBoard(targetBoard);
    }

    const moves = getValidMoves(isMaximizing ? WHITE : BLACK, targetBoard);
    if (moves.length === 0) {
        const opponentMoves = getValidMoves(isMaximizing ? BLACK : WHITE, targetBoard);
        if (opponentMoves.length === 0) return evaluateBoard(targetBoard);
        return minimax(targetBoard, depth - 1, alpha, beta, !isMaximizing);
    }

    if (isMaximizing) {
        let maxEval = -Infinity;
        for (const m of moves) {
            const copy = targetBoard.map(row => [...row]);
            applyMove(m.r, m.c, WHITE, copy);
            let evalValue = minimax(copy, depth - 1, alpha, beta, false);
            maxEval = Math.max(maxEval, evalValue);
            alpha = Math.max(alpha, evalValue);
            if (beta <= alpha) break;
        }
        return maxEval;
    } else {
        let minEval = Infinity;
        for (const m of moves) {
            const copy = targetBoard.map(row => [...row]);
            applyMove(m.r, m.c, BLACK, copy);
            let evalValue = minimax(copy, depth - 1, alpha, beta, true);
            minEval = Math.min(minEval, evalValue);
            beta = Math.min(beta, evalValue);
            if (beta <= alpha) break;
        }
        return minEval;
    }
}

function evaluateBoard(targetBoard) {
    let score = 0;
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            if (targetBoard[r][c] === WHITE) score += weights[r][c];
            else if (targetBoard[r][c] === BLACK) score -= weights[r][c];
        }
    }
    return score;
}

// --- Heuristics & Hints ---
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
function evaluateMove(r, c) { return weights[r][c]; }
function getHintClass(score) {
    if (score >= 50) return 'hint-great';
    if (score > 0) return 'hint-good';
    if (score < -10) return 'hint-bad';
    return '';
}

function renderBoard() {
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
                    if (currentMode === 'othello') { 
                        const score = evaluateMove(r, c);
                        const hintClass = getHintClass(score);
                        if (hintClass) hint.classList.add(hintClass);
                    } else {
                        hint.classList.add('hint-good'); 
                    }
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
    if (pollInterval) clearInterval(pollInterval);
    initGame(mode);
}

async function startMultiplayerGame(mode) {
    const roomId = document.getElementById('room-input').value;
    connectionStatus.innerText = "Connecting...";

    try {
        const res = await fetch(`/join?room=${roomId}&mode=${mode}`, { method: 'POST' });
        if (res.status === 403) throw new Error("Room is full");
        if (!res.ok) throw new Error("Connection failed");
        
        const data = await res.json();
        
        myPlayerId = data.player_id;
        currentMode = data.mode; // Sync mode from server
        isMultiplayer = true;
        
        // Setup UI
        lobbyPanel.classList.add('hidden');
        gamePanel.classList.remove('hidden');
        
        // Start Polling
        pollInterval = setInterval(() => pollState(roomId), 1000);
        pollState(roomId); // Initial poll
        
    } catch (e) {
        connectionStatus.innerText = "Error: " + e.message;
    }
}

async function pollState(roomId) {
    try {
        const res = await fetch(`/state?room=${roomId}`);
        if (!res.ok) return;
        const data = await res.json();
        
        // Sync Mode if not set (re-entry)
        if (!currentMode) currentMode = data.mode;

        if (data.status === "waiting") {
             // Show waiting overlay or status
             turnIndicator.innerText = "Waiting for Opponent...";
        } else if (data.status === "active") {
            updateBoardFromState(data.board);
            currentPlayer = data.turn;
            gameActive = true;
            renderBoard();
            updateUI();
        } else if (data.status === "finished") {
             updateBoardFromState(data.board);
             gameActive = false;
             clearInterval(pollInterval);
             renderBoard();
             updateUI();
             alert("Game Over!");
        }
    } catch (e) { console.error(e); }
}

function updateBoardFromState(flatBoard) {
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
