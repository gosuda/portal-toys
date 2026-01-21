const boardElement = document.getElementById('game-board');
const scoreBlackElement = document.getElementById('score-black');
const scoreWhiteElement = document.getElementById('score-white');
const turnIndicator = document.getElementById('turn-indicator');
const connectionStatus = document.getElementById('connection-status');
const lobbyButtons = document.querySelectorAll('.mode-select button');

const SIZE = 8;
const BLACK = 1;
const WHITE = 2;
let currentPlayer = BLACK;
let board = [];
let isMultiplayer = false;
let myPlayerId = 0; // 0 for local, 1 (Black) or 2 (White) for online
let gameActive = false;
let pollInterval = null;

// Directions: [row, col]
const directions = [
    [-1, -1], [-1, 0], [-1, 1],
    [0, -1],           [0, 1],
    [1, -1],  [1, 0],  [1, 1]
];

// --- Core Game Logic ---

function initGame() {
    board = Array(SIZE).fill(null).map(() => Array(SIZE).fill(0));
    // Initial setup
    board[3][3] = WHITE;
    board[3][4] = BLACK;
    board[4][3] = BLACK;
    board[4][4] = WHITE;
    
    currentPlayer = BLACK;
    gameActive = true;
    renderBoard();
    updateUI();
}

function getValidMoves(player) {
    const moves = [];
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            if (isValidMove(r, c, player)) {
                moves.push({r, c});
            }
        }
    }
    return moves;
}

function isValidMove(r, c, player) {
    if (board[r][c] !== 0) return false;

    const opponent = player === BLACK ? WHITE : BLACK;
    
    for (const [dr, dc] of directions) {
        let nr = r + dr;
        let nc = c + dc;
        let piecesToFlip = 0;

        while (nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && board[nr][nc] === opponent) {
            nr += dr;
            nc += dc;
            piecesToFlip++;
        }

        if (piecesToFlip > 0 && nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && board[nr][nc] === player) {
            return true;
        }
    }
    return false;
}

function makeMoveLocal(r, c) {
    if (!gameActive) return;
    if (isMultiplayer && currentPlayer !== myPlayerId) return; // Not my turn

    if (!isValidMove(r, c, currentPlayer)) return;

    // In multiplayer, we just send the move, we don't apply it locally until confirmed (or we apply optimistically)
    // For simplicity/sync: Send move, wait for state update.
    if (isMultiplayer) {
        sendMove(r, c);
        return; 
    }

    applyMove(r, c, currentPlayer);
    
    const opponent = currentPlayer === BLACK ? WHITE : BLACK;
    currentPlayer = opponent;
    
    checkTurn(opponent);
}

function applyMove(r, c, player) {
    board[r][c] = player;
    const opponent = player === BLACK ? WHITE : BLACK;

    for (const [dr, dc] of directions) {
        let nr = r + dr;
        let nc = c + dc;
        let path = [];

        while (nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && board[nr][nc] === opponent) {
            path.push({r: nr, c: nc});
            nr += dr;
            nc += dc;
        }

        if (path.length > 0 && nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && board[nr][nc] === player) {
            for (const cell of path) {
                board[cell.r][cell.c] = player;
            }
        }
    }
    renderBoard();
    updateUI();
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
            alert(`Game Over! Winner: ${winner}`);
        } else {
            if (!isMultiplayer) {
                alert((player === BLACK ? "Black" : "White") + " has no moves! Passing turn.");
                currentPlayer = opponent;
                renderBoard();
                updateUI();
            } else {
                // In multiplayer, server handles passing logic usually, but here we might need to handle it 
                // However, our server is simple state store. 
                // If I have no moves, I should "pass".
                // For this simple example, we rely on server state.
            }
        }
    }
}

function countPieces() {
    let black = 0;
    let white = 0;
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            if (board[r][c] === BLACK) black++;
            else if (board[r][c] === WHITE) white++;
        }
    }
    return { black, white };
}

function updateUI() {
    const scores = countPieces();
    scoreBlackElement.innerText = `Black: ${scores.black}`;
    scoreWhiteElement.innerText = `White: ${scores.white}`;
    
    let turnText = `Current Turn: ${currentPlayer === BLACK ? "Black" : "White"}`;
    if (isMultiplayer) {
        turnText += (currentPlayer === myPlayerId ? " (YOU)" : " (Opponent)");
    }
    turnIndicator.innerText = turnText;
    turnIndicator.style.color = currentPlayer === BLACK ? "white" : "#f1c40f";
}

// --- Heuristics & Hints ---

// Static weights for 8x8 Othello
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

function evaluateMove(r, c) {
    // Basic evaluation based on position
    return weights[r][c];
}

function getHintClass(score) {
    if (score >= 50) return 'hint-great'; // Corners
    if (score > 0) return 'hint-good';    // Safe edges/center
    if (score < -10) return 'hint-bad';   // X-squares/C-squares
    return ''; // Neutral
}

// --- Rendering ---

function renderBoard() {
    boardElement.innerHTML = '';
    const validMoves = (gameActive && (!isMultiplayer || currentPlayer === myPlayerId)) 
                       ? getValidMoves(currentPlayer) 
                       : [];

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
                // Show hints
                const move = validMoves.find(m => m.r === r && m.c === c);
                if (move) {
                    const hint = document.createElement('div');
                    hint.className = 'valid-move';
                    
                    // Add AI hint
                    const score = evaluateMove(r, c);
                    const hintClass = getHintClass(score);
                    if (hintClass) hint.classList.add(hintClass);
                    
                    cell.appendChild(hint);
                }
            }
            boardElement.appendChild(cell);
        }
    }
}

// --- Multiplayer / Network ---

function startLocalGame() {
    isMultiplayer = false;
    myPlayerId = 0;
    if (pollInterval) clearInterval(pollInterval);
    connectionStatus.innerText = "Mode: Local Play";
    initGame();
}

async function startMultiplayerGame() {
    isMultiplayer = true;
    connectionStatus.innerText = "Connecting to server...";
    lobbyButtons.forEach(b => b.disabled = true);

    try {
        const res = await fetch('/join?room=1', { method: 'POST' });
        if (!res.ok) throw new Error("Failed to join");
        const data = await res.json();
        
        myPlayerId = data.player_id; // 1 or 2
        connectionStatus.innerText = `Connected! You are ${myPlayerId === 1 ? "Black" : "White"}. Waiting for opponent...`;
        
        // Start polling
        pollInterval = setInterval(pollState, 1000);
        
    } catch (e) {
        connectionStatus.innerText = "Error: " + e.message;
        lobbyButtons.forEach(b => b.disabled = false);
    }
}

async function pollState() {
    try {
        const res = await fetch('/state?room=1');
        if (!res.ok) return;
        const data = await res.json();
        
        if (data.status === "waiting") {
            connectionStatus.innerText = `Waiting for opponent... (You are ${myPlayerId === 1 ? "Black" : "White"})`;
            return;
        }

        if (data.status === "active") {
            connectionStatus.innerText = `Game Active! You are ${myPlayerId === 1 ? "Black" : "White"}`;
            // Sync board
            updateBoardFromState(data.board);
            currentPlayer = data.turn;
            gameActive = true;
            renderBoard();
            updateUI();
        }
        
        if (data.status === "finished") {
             connectionStatus.innerText = "Game Over!";
             updateBoardFromState(data.board);
             gameActive = false;
             clearInterval(pollInterval);
             renderBoard();
             updateUI();
        }

    } catch (e) {
        console.error("Poll error", e);
    }
}

function updateBoardFromState(flatBoard) {
    // Server sends flat array of 64 ints
    let idx = 0;
    for (let r = 0; r < SIZE; r++) {
        for (let c = 0; c < SIZE; c++) {
            board[r][c] = flatBoard[idx++];
        }
    }
}

async function sendMove(r, c) {
    try {
        const res = await fetch('/move?room=1', { 
            method: 'POST',
            body: JSON.stringify({ r, c, player: myPlayerId })
        });
        if (res.ok) {
            // Immediate poll to update UI faster
            pollState(); 
        }
    } catch (e) {
        console.error("Move error", e);
    }
}

// Default to local on load
// startLocalGame();