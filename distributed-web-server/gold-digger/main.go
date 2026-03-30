package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	gameTitle            = "99.99% 순금을 노려라!"
	defaultDBPath        = "./gold-digger/data/goldsmiths.db"
	starterWallet        = 500
	sessionLifetime      = 12 * time.Hour
	maxInputSize         = 1 << 20
	minUsernameLength    = 3
	maxUsernameLength    = 24
	minPasswordLength    = 8
	maxPasswordLength    = 64
	maxLeaderboardRecord = 10
)

var (
	betTierTable = map[int]betTier{
		5:  {Label: "+5% / -5%", Win: 0.05, Loss: 0.05},
		10: {Label: "+10% / -15%", Win: 0.10, Loss: 0.15},
		20: {Label: "+20% / -30%", Win: 0.20, Loss: 0.30},
		30: {Label: "+30% / -40%", Win: 0.30, Loss: 0.40},
	}
)

//go:embed web/index.html
var uiHTML string

type server struct {
	db *sql.DB
}

type apiRequest struct {
	Action    string  `json:"action"`
	Username  string  `json:"username"`
	Password  string  `json:"password"`
	Token     string  `json:"token"`
	Guess     float64 `json:"guess"`
	Choice    string  `json:"choice"`
	Stake     int     `json:"stake"`
	MatchID   int64   `json:"matchId"`
	BetTarget string  `json:"betTarget"`
	BetTier   int     `json:"betTier"`
	BetAmount int     `json:"betAmount"`
}

type apiResponse struct {
	Title       string             `json:"title"`
	Status      string             `json:"status"`
	Message     string             `json:"message,omitempty"`
	Token       string             `json:"token,omitempty"`
	Attempt     *attemptPayload    `json:"attempt,omitempty"`
	Leaderboard []leaderboardEntry `json:"leaderboard,omitempty"`
	Wallet      *walletPayload     `json:"wallet,omitempty"`
	Match       *matchPayload      `json:"match,omitempty"`
	Bets        []betPayload       `json:"bets,omitempty"`
	HTML        string             `json:"html,omitempty"`
}

type leaderboardEntry struct {
	Rank      int    `json:"rank"`
	Username  string `json:"username"`
	Total     int    `json:"totalPoints"`
	Wallet    int    `json:"wallet"`
	LastLogin string `json:"lastLogin"`
}

type attemptPayload struct {
	Choice  string  `json:"choice"`
	Stake   int     `json:"stake"`
	Chance  float64 `json:"chance"`
	Result  string  `json:"result"`
	Success bool    `json:"success"`
	Payout  int     `json:"payout"`
	Delta   int     `json:"delta"`
	Balance int     `json:"balance"`
}

type walletPayload struct {
	Balance int `json:"balance"`
}

type matchPayload struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	You           string  `json:"you"`
	Opponent      string  `json:"opponent"`
	YourGuess     float64 `json:"yourGuess"`
	OpponentGuess float64 `json:"opponentGuess"`
	Actual        float64 `json:"actual"`
	YourPoints    int     `json:"yourPoints"`
	OpponentPts   int     `json:"opponentPoints"`
	Winner        string  `json:"winner"`
	Bettable      bool    `json:"bettable"`
}

type betPayload struct {
	MatchID int64  `json:"matchId"`
	Tier    string `json:"tier"`
	Wager   int    `json:"wager"`
	Target  string `json:"target"`
	State   string `json:"state"`
	Delta   int    `json:"delta"`
}

type betTier struct {
	Label string
	Win   float64
	Loss  float64
}

type sessionRecord struct {
	UserID int64
	Token  string
	Expiry time.Time
}

func main() {
	dbPath := strings.TrimSpace(os.Getenv("GOLD_DIGGER_DB"))
	if dbPath == "" {
		dbPath = defaultDBPath
	}
	if err := ensurePath(dbPath); err != nil {
		exitErr(err)
	}
	db, err := openDB(dbPath)
	if err != nil {
		exitErr(err)
	}
	defer db.Close()

	if err := applySchema(db); err != nil {
		exitErr(err)
	}

	srv := &server{db: db}
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 0, 64*1024), maxInputSize)

	for reader.Scan() {
		line := append([]byte(nil), reader.Bytes()...)
		resp := srv.dispatch(line)
		output, err := json.Marshal(resp)
		if err != nil {
			fmt.Println(`{"title":"` + gameTitle + `","status":"error","message":"json encode failure"}`)
			continue
		}
		fmt.Println(string(output))
	}

	if err := reader.Err(); err != nil && !errors.Is(err, io.EOF) {
		exitErr(err)
	}
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "gold-digger fatal: %v\n", err)
	os.Exit(1)
}

func ensurePath(dbPath string) error {
	dir := filepath.Dir(dbPath)
	return os.MkdirAll(dir, 0o755)
}

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func applySchema(db *sql.DB) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT NOT NULL UNIQUE,
            password_hash TEXT NOT NULL,
            wallet_points INTEGER NOT NULL DEFAULT 0,
            last_login TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );`,
		`CREATE TABLE IF NOT EXISTS sessions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            token TEXT NOT NULL UNIQUE,
            expires_at TIMESTAMP NOT NULL,
            FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
        );`,
		`CREATE TABLE IF NOT EXISTS vs_matches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			player_a INTEGER NOT NULL,
            player_b INTEGER,
            status TEXT NOT NULL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            resolved_at TIMESTAMP,
            winner_id INTEGER,
            actual_value REAL,
            a_guess REAL,
            b_guess REAL,
            a_points INTEGER,
            b_points INTEGER,
            FOREIGN KEY(player_a) REFERENCES users(id),
            FOREIGN KEY(player_b) REFERENCES users(id)
        );`,
		`CREATE TABLE IF NOT EXISTS vs_bets (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            match_id INTEGER NOT NULL,
            bettor_id INTEGER NOT NULL,
            target_id INTEGER NOT NULL,
            tier INTEGER NOT NULL,
            wager INTEGER NOT NULL,
            state TEXT NOT NULL DEFAULT 'pending',
            delta INTEGER NOT NULL DEFAULT 0,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            resolved_at TIMESTAMP,
            FOREIGN KEY(match_id) REFERENCES vs_matches(id) ON DELETE CASCADE,
            FOREIGN KEY(bettor_id) REFERENCES users(id) ON DELETE CASCADE,
            FOREIGN KEY(target_id) REFERENCES users(id) ON DELETE CASCADE
        );`,
		`CREATE INDEX IF NOT EXISTS vs_match_idx ON vs_matches(status);`,
		`CREATE INDEX IF NOT EXISTS vs_bet_idx ON vs_bets(match_id, state);`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := ensureWalletColumn(db); err != nil {
		return err
	}
	return ensureRefineTable(db)
}

func ensureWalletColumn(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='wallet_points'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if _, err := db.Exec("ALTER TABLE users ADD COLUMN wallet_points INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}

func ensureRefineTable(db *sql.DB) error {
	ddl := `CREATE TABLE IF NOT EXISTS refine_rounds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		choice TEXT NOT NULL,
		stake INTEGER NOT NULL,
		success_prob REAL NOT NULL,
		success INTEGER NOT NULL,
		payout INTEGER NOT NULL,
		delta INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);`
	if _, err := db.Exec(ddl); err != nil {
		return err
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS refine_round_user_idx ON refine_rounds(user_id)`)
	return err
}

func (s *server) dispatch(line []byte) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	var req apiRequest
	if err := json.Unmarshal(line, &req); err != nil {
		resp.Message = "잘못된 JSON 입력입니다."
		return resp
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "", "ui":
		resp.Status = "ok"
		resp.HTML = uiHTML
		resp.Message = "브라우저에서 붙여넣으면 바로 플레이할 수 있습니다."
		return resp
	case "signup":
		return s.handleSignup(req)
	case "signin":
		return s.handleSignin(req)
	case "refine":
		return s.handleRefine(req)
	case "status":
		return s.handleStatus(req)
	case "vs_queue":
		return s.handleVSQueue(req)
	case "vs_move":
		return s.handleVSMove(req)
	case "vs_bet":
		return s.handleVSBets(req)
	case "vs_status":
		return s.handleVSStatus(req)
	default:
		resp.Message = "지원하지 않는 action 입니다. ui/signup/signin/refine/status/vs_* 명령을 확인하세요."
		return resp
	}
}

func (s *server) handleUI() apiResponse {
	return apiResponse{Title: gameTitle, Status: "ok", HTML: uiHTML}
}

func (s *server) handleSignup(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	username := sanitizeUsername(req.Username)
	if err := validateUsername(username); err != nil {
		resp.Message = err.Error()
		return resp
	}
	if err := validatePassword(req.Password); err != nil {
		resp.Message = err.Error()
		return resp
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		resp.Message = "패스워드 처리에 실패했습니다."
		return resp
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, "INSERT INTO users (username, password_hash) VALUES (?, ?)", username, string(hash))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			resp.Message = "이미 존재하는 사용자명입니다."
			return resp
		}
		resp.Message = "회원 가입에 실패했습니다."
		return resp
	}
	if starterWallet > 0 {
		if userID, err := res.LastInsertId(); err == nil {
			_, _ = s.db.ExecContext(ctx, "UPDATE users SET wallet_points = ? WHERE id = ?", starterWallet, userID)
		}
	}

	resp.Status = "ok"
	resp.Message = fmt.Sprintf("%s 님, 길드에 합류했습니다! 시작 지갑 %dpt가 지급되었습니다.", username, starterWallet)
	return resp
}

func (s *server) handleSignin(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	username := sanitizeUsername(req.Username)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	row := s.db.QueryRowContext(ctx, "SELECT id, password_hash FROM users WHERE username = ?", username)
	var id int64
	var hash string
	if err := row.Scan(&id, &hash); err != nil {
		resp.Message = "아이디 혹은 비밀번호를 확인해 주세요."
		return resp
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		resp.Message = "아이디 혹은 비밀번호를 확인해 주세요."
		return resp
	}

	token, err := generateToken()
	if err != nil {
		resp.Message = "세션 발급에 실패했습니다."
		return resp
	}
	expires := time.Now().UTC().Add(sessionLifetime)
	if _, err := s.db.ExecContext(ctx, "INSERT INTO sessions (user_id, token, expires_at) VALUES (?, ?, ?)", id, token, expires); err != nil {
		resp.Message = "세션 저장에 실패했습니다."
		return resp
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE users SET last_login = CURRENT_TIMESTAMP WHERE id = ?", id)

	balance := s.walletBalance(ctx, id)
	resp.Status = "ok"
	resp.Message = "환영합니다. 정련과 VS를 시작해 보세요."
	resp.Token = token
	resp.Wallet = &walletPayload{Balance: balance}
	return resp
}

func (s *server) handleRefine(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	choice := normalizeOutcomeChoice(req.Choice)
	if choice == "" {
		resp.Message = "정련 성공/실패 중 하나를 선택해 베팅하세요."
		return resp
	}
	if req.Stake <= 0 {
		resp.Message = "베팅 금액은 1 이상이어야 합니다."
		return resp
	}
	session, err := s.lookupSession(req.Token)
	if err != nil {
		resp.Message = err.Error()
		return resp
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.walletBalance(ctx, session.UserID) < req.Stake {
		resp.Message = "지갑 잔액이 부족합니다."
		return resp
	}
	if err := s.adjustWallet(ctx, session.UserID, -req.Stake); err != nil {
		resp.Message = "베팅 금액을 잠글 수 없습니다."
		return resp
	}

	chance := sampleProbability()
	success := rollSuccess(chance)
	result := "success"
	if !success {
		result = "fail"
	}

	var payout, delta int
	if (choice == "success" && success) || (choice == "fail" && !success) {
		payout = calcBetReturn(req.Stake, chance, choice == "success")
		delta = payout - req.Stake
		if err := s.adjustWallet(ctx, session.UserID, payout); err != nil {
			resp.Message = "베팅 정산에 실패했습니다."
			return resp
		}
	} else {
		delta = -req.Stake
	}

	if _, err := s.db.ExecContext(ctx, `INSERT INTO refine_rounds (user_id, choice, stake, success_prob, success, payout, delta) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.UserID, choice, req.Stake, chance, boolToInt(success), payout, delta); err != nil {
		resp.Message = "정련 기록 저장에 실패했습니다."
		return resp
	}

	balance := s.walletBalance(ctx, session.UserID)
	resp.Status = "ok"
	resp.Attempt = &attemptPayload{
		Choice:  choice,
		Stake:   req.Stake,
		Chance:  round2(chance),
		Result:  result,
		Success: success,
		Payout:  payout,
		Delta:   delta,
		Balance: balance,
	}
	resp.Wallet = &walletPayload{Balance: balance}
	if delta >= 0 {
		resp.Message = fmt.Sprintf("정련 %s! +%d pt", displayResult(result), delta)
	} else {
		resp.Message = fmt.Sprintf("정련 %s... %d pt 손실", displayResult(result), -delta)
	}
	return resp
}

func (s *server) handleStatus(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "ok"}
	var session *sessionRecord
	if token := strings.TrimSpace(req.Token); token != "" {
		if sess, err := s.lookupSession(token); err == nil {
			session = sess
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp.Leaderboard = s.fetchLeaderboard(ctx)
	if session != nil {
		resp.Wallet = &walletPayload{Balance: s.walletBalance(ctx, session.UserID)}
		resp.Match = s.matchView(ctx, session.UserID)
		resp.Bets = s.betView(ctx, session.UserID)
		resp.Message = "토큰 사용자가 확인되었습니다. VS 및 베팅 상황을 확인하세요."
	} else {
		resp.Message = "상위 순위를 확인했습니다. 로그인하면 VS/베팅 현황을 볼 수 있습니다."
	}
	return resp
}

func (s *server) handleVSQueue(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	session, err := s.lookupSession(req.Token)
	if err != nil {
		resp.Message = err.Error()
		return resp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	if current := s.matchView(ctx, session.UserID); current != nil && current.Status != "resolved" {
		resp.Message = "이미 매칭 중입니다. vs_status 로 확인하세요."
		return resp
	}

	waitingID, waitingUser := s.findWaitingMatch(ctx, session.UserID)
	if waitingID > 0 {
		_, err = s.db.ExecContext(ctx, "UPDATE vs_matches SET player_b = ?, status = 'active' WHERE id = ?", session.UserID, waitingID)
		if err != nil {
			resp.Message = "매칭 갱신에 실패했습니다."
			return resp
		}
		resp.Status = "ok"
		resp.Message = fmt.Sprintf("%s 님과의 VS가 시작되었습니다! matchId=%d", waitingUser, waitingID)
		resp.Match = s.matchView(ctx, session.UserID)
		return resp
	}

	res, err := s.db.ExecContext(ctx, "INSERT INTO vs_matches (player_a, status) VALUES (?, 'waiting')", session.UserID)
	if err != nil {
		resp.Message = "매칭 대기 등록에 실패했습니다."
		return resp
	}
	id, _ := res.LastInsertId()
	resp.Status = "ok"
	resp.Message = fmt.Sprintf("대기열에 등록되었습니다. matchId=%d", id)
	return resp
}

func (s *server) handleVSStatus(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	session, err := s.lookupSession(req.Token)
	if err != nil {
		resp.Message = err.Error()
		return resp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp.Status = "ok"
	resp.Match = s.matchView(ctx, session.UserID)
	resp.Bets = s.betView(ctx, session.UserID)
	if resp.Match == nil {
		resp.Message = "진행 중인 매치가 없습니다. vs_queue 로 참가하세요."
	} else {
		resp.Message = "현재 VS 상태를 전달했습니다."
	}
	return resp
}

func (s *server) handleVSMove(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	session, err := s.lookupSession(req.Token)
	if err != nil {
		resp.Message = err.Error()
		return resp
	}
	if req.MatchID == 0 {
		resp.Message = "matchId 가 필요합니다."
		return resp
	}
	if req.Guess < 40 || req.Guess > 100 {
		resp.Message = "VS 예측값은 40~100 사이여야 합니다."
		return resp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	row := s.db.QueryRowContext(ctx, "SELECT status, player_a, player_b, a_guess, b_guess FROM vs_matches WHERE id = ?", req.MatchID)
	var status string
	var aID, bID sql.NullInt64
	var aGuess, bGuess sql.NullFloat64
	if err := row.Scan(&status, &aID, &bID, &aGuess, &bGuess); err != nil {
		resp.Message = "매치를 찾을 수 없습니다."
		return resp
	}

	var role string
	if aID.Int64 == session.UserID {
		role = "a"
	} else if bID.Valid && bID.Int64 == session.UserID {
		role = "b"
	} else {
		resp.Message = "참가자가 아닌 매치입니다."
		return resp
	}

	if status == "waiting" {
		resp.Message = "상대를 기다리는 중입니다."
		return resp
	}
	if status == "resolved" {
		resp.Message = "이미 끝난 매치입니다."
		resp.Match = s.matchView(ctx, session.UserID)
		return resp
	}

	column := "a_guess"
	if role == "b" {
		column = "b_guess"
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("UPDATE vs_matches SET %s = ? WHERE id = ?", column), req.Guess, req.MatchID); err != nil {
		resp.Message = "예측 저장에 실패했습니다."
		return resp
	}

	s.tryResolveMatch(ctx, req.MatchID)

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer statusCancel()

	resp.Status = "ok"
	resp.Match = s.matchView(statusCtx, session.UserID)
	if resp.Match != nil && resp.Match.Status == "resolved" {
		resp.Message = "경기가 종료되었습니다!"
	} else {
		resp.Message = "예측이 저장되었습니다. 상대를 기다리는 중입니다."
	}
	return resp
}

func (s *server) handleVSBets(req apiRequest) apiResponse {
	resp := apiResponse{Title: gameTitle, Status: "error"}
	session, err := s.lookupSession(req.Token)
	if err != nil {
		resp.Message = err.Error()
		return resp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	if req.MatchID == 0 {
		resp.Message = "matchId 가 필요합니다."
		return resp
	}
	tier, ok := betTierTable[req.BetTier]
	if !ok {
		resp.Message = "허용된 배당률은 5/10/20/30 입니다."
		return resp
	}
	if req.BetAmount <= 0 {
		resp.Message = "betAmount 는 1 이상이어야 합니다."
		return resp
	}

	match := s.matchDetails(ctx, req.MatchID)
	if match == nil || match.Status != "active" {
		resp.Message = "베팅 가능한 매치가 아닙니다."
		return resp
	}

	targetID := match.PlayerA
	targetName := match.PlayerAName.String
	if strings.EqualFold(req.BetTarget, "b") || strings.EqualFold(req.BetTarget, match.PlayerBName.String) {
		if !match.PlayerB.Valid {
			resp.Message = "상대가 아직 정해지지 않았습니다."
			return resp
		}
		targetID = match.PlayerB.Int64
		targetName = match.PlayerBName.String
	} else if strings.EqualFold(req.BetTarget, "a") || strings.EqualFold(req.BetTarget, match.PlayerAName.String) {
		// already default
	} else if strings.TrimSpace(req.BetTarget) != "" && match.PlayerB.Valid {
		// user might have typed opponent username directly; check both
		if strings.EqualFold(req.BetTarget, match.PlayerAName.String) {
			targetID = match.PlayerA
			targetName = match.PlayerAName.String
		} else if strings.EqualFold(req.BetTarget, match.PlayerBName.String) {
			targetID = match.PlayerB.Int64
			targetName = match.PlayerBName.String
		}
	}
	if targetID == 0 {
		resp.Message = "상대가 아직 정해지지 않았습니다."
		return resp
	}
	if targetID == session.UserID {
		resp.Message = "자기 자신에게는 베팅할 수 없습니다."
		return resp
	}

	if s.walletBalance(ctx, session.UserID) < req.BetAmount {
		resp.Message = "지갑 잔액이 부족합니다."
		return resp
	}

	if err := s.adjustWallet(ctx, session.UserID, -req.BetAmount); err != nil {
		resp.Message = "베팅 금액을 잠글 수 없습니다."
		return resp
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO vs_bets (match_id, bettor_id, target_id, tier, wager) VALUES (?, ?, ?, ?, ?)`,
		req.MatchID, session.UserID, targetID, req.BetTier, req.BetAmount)
	if err != nil {
		_ = s.adjustWallet(ctx, session.UserID, req.BetAmount)
		resp.Message = "베팅 기록 저장에 실패했습니다."
		return resp
	}

	resp.Status = "ok"
	resp.Message = fmt.Sprintf("%s 에게 %d점을 베팅했습니다. (%s)", targetName, req.BetAmount, tier.Label)
	resp.Wallet = &walletPayload{Balance: s.walletBalance(ctx, session.UserID)}
	resp.Bets = s.betView(ctx, session.UserID)
	return resp
}

func (s *server) lookupSession(token string) (*sessionRecord, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("유효한 토큰이 필요합니다. 먼저 signin 해주세요.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	row := s.db.QueryRowContext(ctx, "SELECT user_id, token, expires_at FROM sessions WHERE token = ?", token)
	var sess sessionRecord
	if err := row.Scan(&sess.UserID, &sess.Token, &sess.Expiry); err != nil {
		return nil, errors.New("세션을 찾을 수 없습니다. 다시 signin 해주세요.")
	}
	if time.Now().UTC().After(sess.Expiry) {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
		return nil, errors.New("세션이 만료되었습니다. 다시 signin 해주세요.")
	}
	return &sess, nil
}

func (s *server) totalPoints(ctx context.Context, userID int64) int {
	var total sql.NullInt64
	_ = s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(delta),0) FROM refine_rounds WHERE user_id = ?", userID).Scan(&total)
	if total.Valid {
		return int(total.Int64)
	}
	return 0
}

func (s *server) walletBalance(ctx context.Context, userID int64) int {
	var balance sql.NullInt64
	_ = s.db.QueryRowContext(ctx, "SELECT wallet_points FROM users WHERE id = ?", userID).Scan(&balance)
	if balance.Valid {
		return int(balance.Int64)
	}
	return 0
}

func (s *server) adjustWallet(ctx context.Context, userID int64, delta int) error {
	balance := s.walletBalance(ctx, userID)
	if balance+delta < 0 {
		return errors.New("잔액이 부족합니다.")
	}
	_, err := s.db.ExecContext(ctx, "UPDATE users SET wallet_points = wallet_points + ? WHERE id = ?", delta, userID)
	return err
}

type matchDetail struct {
	ID          int64
	Status      string
	PlayerA     int64
	PlayerB     sql.NullInt64
	PlayerAName sql.NullString
	PlayerBName sql.NullString
	AGuess      sql.NullFloat64
	BGuess      sql.NullFloat64
	Actual      sql.NullFloat64
	APoints     sql.NullInt64
	BPoints     sql.NullInt64
	Winner      sql.NullInt64
}

func (s *server) matchDetails(ctx context.Context, matchID int64) *matchDetail {
	row := s.db.QueryRowContext(ctx, `SELECT m.id, m.status, m.player_a, m.player_b, ua.username, ub.username, m.a_guess, m.b_guess, m.actual_value, m.a_points, m.b_points, m.winner_id
        FROM vs_matches m
        LEFT JOIN users ua ON m.player_a = ua.id
        LEFT JOIN users ub ON m.player_b = ub.id
        WHERE m.id = ?`, matchID)
	var md matchDetail
	if err := row.Scan(&md.ID, &md.Status, &md.PlayerA, &md.PlayerB, &md.PlayerAName, &md.PlayerBName, &md.AGuess, &md.BGuess, &md.Actual, &md.APoints, &md.BPoints, &md.Winner); err != nil {
		return nil
	}
	return &md
}

func (s *server) matchView(ctx context.Context, userID int64) *matchPayload {
	row := s.db.QueryRowContext(ctx, `SELECT m.id FROM vs_matches m WHERE m.status IN ('waiting','active') AND (m.player_a = ? OR m.player_b = ?) ORDER BY m.id DESC LIMIT 1`, userID, userID)
	var matchID int64
	if err := row.Scan(&matchID); err != nil {
		row = s.db.QueryRowContext(ctx, `SELECT m.id FROM vs_matches m WHERE m.status = 'resolved' AND (m.player_a = ? OR m.player_b = ?) ORDER BY m.id DESC LIMIT 1`, userID, userID)
		if err := row.Scan(&matchID); err != nil {
			return nil
		}
	}
	detail := s.matchDetails(ctx, matchID)
	if detail == nil {
		return nil
	}
	payload := &matchPayload{ID: detail.ID, Status: detail.Status}
	playerB := int64(0)
	if detail.PlayerB.Valid {
		playerB = detail.PlayerB.Int64
	}
	if detail.PlayerA == userID {
		payload.You = detail.PlayerAName.String
		payload.Opponent = detail.PlayerBName.String
		if detail.AGuess.Valid {
			payload.YourGuess = round2(detail.AGuess.Float64)
		}
		if detail.BGuess.Valid {
			payload.OpponentGuess = round2(detail.BGuess.Float64)
		}
		if detail.APoints.Valid {
			payload.YourPoints = int(detail.APoints.Int64)
		}
		if detail.BPoints.Valid {
			payload.OpponentPts = int(detail.BPoints.Int64)
		}
	} else if playerB != 0 && playerB == userID {
		payload.You = detail.PlayerBName.String
		payload.Opponent = detail.PlayerAName.String
		if detail.BGuess.Valid {
			payload.YourGuess = round2(detail.BGuess.Float64)
		}
		if detail.AGuess.Valid {
			payload.OpponentGuess = round2(detail.AGuess.Float64)
		}
		if detail.BPoints.Valid {
			payload.YourPoints = int(detail.BPoints.Int64)
		}
		if detail.APoints.Valid {
			payload.OpponentPts = int(detail.APoints.Int64)
		}
	} else {
		payload.You = detail.PlayerAName.String
		payload.Opponent = detail.PlayerBName.String
	}
	if detail.Actual.Valid {
		payload.Actual = round2(detail.Actual.Float64)
	}
	if detail.Winner.Valid {
		if detail.Winner.Int64 == detail.PlayerA {
			payload.Winner = detail.PlayerAName.String
		} else if detail.Winner.Int64 == playerB {
			payload.Winner = detail.PlayerBName.String
		}
	}
	payload.Bettable = detail.Status == "active" && detail.PlayerA != 0 && playerB != 0
	return payload
}

func (s *server) findWaitingMatch(ctx context.Context, userID int64) (int64, string) {
	row := s.db.QueryRowContext(ctx, `SELECT m.id, u.username FROM vs_matches m JOIN users u ON m.player_a = u.id WHERE m.status = 'waiting' AND m.player_a <> ? ORDER BY m.created_at LIMIT 1`, userID)
	var id int64
	var name string
	if err := row.Scan(&id, &name); err != nil {
		return 0, ""
	}
	return id, name
}

func (s *server) tryResolveMatch(ctx context.Context, matchID int64) {
	detail := s.matchDetails(ctx, matchID)
	if detail == nil || detail.Status != "active" {
		return
	}
	if !detail.AGuess.Valid || !detail.BGuess.Valid {
		return
	}

	actual := sampleProbability()
	aPoints := scoreAttempt(detail.AGuess.Float64, actual)
	bPoints := scoreAttempt(detail.BGuess.Float64, actual)

	winner := int64(0)
	switch {
	case aPoints > bPoints:
		winner = detail.PlayerA
	case bPoints > aPoints && detail.PlayerB.Valid:
		winner = detail.PlayerB.Int64
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE vs_matches SET status='resolved', resolved_at=CURRENT_TIMESTAMP, actual_value=?, a_points=?, b_points=?, winner_id=? WHERE id=?`, actual, aPoints, bPoints, winner, matchID)

	if winner != 0 {
		var loser int64
		if winner == detail.PlayerA && detail.PlayerB.Valid {
			loser = detail.PlayerB.Int64
		} else if detail.PlayerB.Valid && winner == detail.PlayerB.Int64 {
			loser = detail.PlayerA
		}
		_ = s.adjustWallet(ctx, winner, 150)
		if loser != 0 {
			_ = s.adjustWallet(ctx, loser, 60)
		}
	}

	s.settleBets(ctx, matchID, winner)
}

func (s *server) settleBets(ctx context.Context, matchID, winner int64) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, bettor_id, target_id, tier, wager FROM vs_bets WHERE match_id = ? AND state = 'pending'", matchID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var betID, bettor, target int64
		var tier, wager int
		if err := rows.Scan(&betID, &bettor, &target, &tier, &wager); err != nil {
			continue
		}
		info, ok := betTierTable[tier]
		if !ok {
			continue
		}
		payout := wager
		state := "lost"
		if winner != 0 && winner == target {
			payout += int(math.Round(float64(wager) * info.Win))
			state = "won"
		} else {
			payout -= int(math.Round(float64(wager) * info.Loss))
		}
		delta := payout - wager
		_ = s.adjustWallet(ctx, bettor, payout)
		_, _ = s.db.ExecContext(ctx, "UPDATE vs_bets SET state = ?, delta = ?, resolved_at = CURRENT_TIMESTAMP WHERE id = ?", state, delta, betID)
	}
}

func (s *server) betView(ctx context.Context, userID int64) []betPayload {
	rows, err := s.db.QueryContext(ctx, `SELECT b.match_id, b.tier, b.wager, u.username, b.state, b.delta
        FROM vs_bets b JOIN users u ON b.target_id = u.id
        WHERE b.bettor_id = ? ORDER BY b.id DESC LIMIT 8`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var bets []betPayload
	for rows.Next() {
		var payload betPayload
		var tier int
		if err := rows.Scan(&payload.MatchID, &tier, &payload.Wager, &payload.Target, &payload.State, &payload.Delta); err != nil {
			continue
		}
		if info, ok := betTierTable[tier]; ok {
			payload.Tier = info.Label
		} else {
			payload.Tier = fmt.Sprintf("%d%%", tier)
		}
		bets = append(bets, payload)
	}
	return bets
}

func (s *server) fetchLeaderboard(ctx context.Context) []leaderboardEntry {
	rows, err := s.db.QueryContext(ctx, `SELECT u.username, u.last_login, u.wallet_points,
        (SELECT COALESCE(SUM(delta),0) FROM refine_rounds WHERE user_id = u.id) as total
        FROM users u ORDER BY u.wallet_points DESC, total DESC LIMIT ?`, maxLeaderboardRecord)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []leaderboardEntry
	rank := 1
	for rows.Next() {
		var entry leaderboardEntry
		var total sql.NullInt64
		var last sql.NullTime
		if err := rows.Scan(&entry.Username, &last, &entry.Wallet, &total); err != nil {
			continue
		}
		entry.Rank = rank
		if total.Valid {
			entry.Total = int(total.Int64)
		}
		if last.Valid {
			entry.LastLogin = last.Time.Format(time.RFC3339)
		}
		entries = append(entries, entry)
		rank++
	}
	return entries
}

func sanitizeUsername(name string) string {
	name = strings.TrimSpace(name)
	return strings.ToLower(name)
}

func validateUsername(username string) error {
	if len(username) < minUsernameLength || len(username) > maxUsernameLength {
		return fmt.Errorf("사용자명은 %d~%d 글자여야 합니다.", minUsernameLength, maxUsernameLength)
	}
	for _, r := range username {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return errors.New("사용자명은 영문 소문자, 숫자, -, _ 만 사용할 수 있습니다.")
		}
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return fmt.Errorf("비밀번호는 %d~%d 글자여야 합니다.", minPasswordLength, maxPasswordLength)
	}
	return nil
}

func generateToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf[:]), nil
}

func normalizeOutcomeChoice(choice string) string {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "success", "성공", "s", "win", "pass":
		return "success"
	case "fail", "failure", "실패", "f", "lose":
		return "fail"
	default:
		return ""
	}
}

func sampleProbability() float64 {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		now := time.Now().UnixNano()
		return round2(60 + float64(now%4000)/100)
	}
	raw := binary.LittleEndian.Uint64(bytes[:])
	normalized := float64(raw) / float64(math.MaxUint64)
	curve := math.Pow(normalized, 2.2)
	value := 55 + curve*40
	if raw%997 == 0 {
		return 99.99
	}
	return round2(value)
}

func rollSuccess(prob float64) bool {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		now := time.Now().UnixNano()
		return float64(now%10000)/100 <= prob
	}
	raw := binary.LittleEndian.Uint64(bytes[:])
	normalized := float64(raw) / float64(math.MaxUint64)
	return normalized*100 <= prob
}

func calcBetReturn(stake int, prob float64, betSuccess bool) int {
	if stake <= 0 {
		return 0
	}
	if betSuccess {
		denom := math.Max(prob, 1)
		return int(math.Round(float64(stake) * (100.0 / denom)))
	}
	denom := math.Max(100.0-prob, 1)
	return int(math.Round(float64(stake) * (100.0 / denom)))
}

func displayResult(result string) string {
	switch strings.ToLower(result) {
	case "success":
		return "성공"
	case "fail":
		return "실패"
	default:
		return result
	}
}

func scoreAttempt(guess, actual float64) int {
	delta := math.Abs(actual - guess)
	raw := math.Max(0, 120-delta*3)
	if actual >= 99.90 {
		raw += 50
	}
	return int(math.Round(raw))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
