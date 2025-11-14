package main

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

const (
	nightDuration   = 25 * time.Second
	dayDuration     = 40 * time.Second
	voteDuration    = 15 * time.Second
	defenseDuration = 10 * time.Second
)

type GamePhase string

const (
	PhaseLobby   GamePhase = "lobby"
	PhaseNight   GamePhase = "night"
	PhaseDay     GamePhase = "day"
	PhaseVote    GamePhase = "vote"
	PhaseDefense GamePhase = "defense"
)

type Room struct {
	name    string
	manager *RoomManager

	players map[string]*Client
	order   []string
	host    string

	commands chan func(*Room)
	closing  chan struct{}

	state GameState

	phaseTimer *time.Timer
}

type GameState struct {
	Active        bool
	Phase         GamePhase
	DayCount      int
	Alive         map[string]bool
	Jobs          map[string]*JobSpec
	Assign        map[string]*AssignedJob
	Prefix        map[string]map[string]string
	Vote          map[string]int
	Defense       string
	MafiaPick     string
	DoctorPick    string
	DetectivePick string
}

type AssignedJob struct {
	Name    string
	Team    string
	Desc    string
	Passive string
}

func NewRoom(name string, mgr *RoomManager) *Room {
	r := &Room{
		name:     name,
		manager:  mgr,
		players:  make(map[string]*Client),
		commands: make(chan func(*Room), 256),
		closing:  make(chan struct{}),
	}
	r.state.Reset()
	go r.loop()
	return r
}

func (gs *GameState) Reset() {
	gs.Active = false
	gs.Phase = PhaseLobby
	gs.DayCount = 0
	gs.Alive = make(map[string]bool)
	gs.Jobs = make(map[string]*JobSpec)
	gs.Assign = make(map[string]*AssignedJob)
	gs.Prefix = make(map[string]map[string]string)
	gs.Vote = make(map[string]int)
	gs.Defense = ""
	gs.MafiaPick = ""
	gs.DoctorPick = ""
	gs.DetectivePick = ""
}

func (r *Room) loop() {
	for {
		select {
		case fn := <-r.commands:
			fn(r)
		case <-r.closing:
			if r.phaseTimer != nil {
				r.phaseTimer.Stop()
			}
			return
		}
	}
}

func (r *Room) enqueue(fn func(*Room)) {
	select {
	case r.commands <- fn:
	default:
		// backpressure: drop oldest non-critical command
		select {
		case <-r.commands:
		default:
		}
		r.commands <- fn
	}
}

func (r *Room) close() {
	close(r.closing)
}

func (r *Room) addPlayer(c *Client) {
	c.room = r
	if len(r.players) == 0 {
		r.host = c.name
	}
	r.players[c.name] = c
	r.order = appendUnique(r.order, c.name)
	r.state.Prefix[c.name] = make(map[string]string)
	r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("[ %s ] 방에 %s 님이 입장했습니다. (인원 %d명)", r.name, c.name, len(r.players))})
	r.pushRoster()
	if r.state.Active && !r.state.Alive[c.name] {
		c.pushSystem("진행 중인 게임이 있어 관전자 상태입니다.")
	}
}

func (r *Room) removePlayer(c *Client) {
	delete(r.players, c.name)
	r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("%s 님이 퇴장했습니다.", c.name)})
	r.pushRoster()
	if len(r.players) == 0 {
		r.manager.removeRoom(r.name, r)
		r.close()
		return
	}
	if c.name == r.host {
		r.host = r.pickNextHost()
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("방장이 %s 님으로 변경되었습니다.", r.host)})
	}
	if r.state.Active && r.state.Alive[c.name] {
		delete(r.state.Alive, c.name)
		r.checkGameOver()
	}
	c.room = nil
}
func (r *Room) pickNextHost() string {
	for _, name := range r.order {
		if _, ok := r.players[name]; ok {
			return name
		}
	}
	for name := range r.players {
		return name
	}
	return ""
}

func (r *Room) handleMessage(c *Client, msg ClientMessage) {
	switch msg.Type {
	case "chat":
		r.handleChat(c, msg.Text)
	case "start":
		if c.name != r.host {
			c.pushSystem("방장만 시작할 수 있습니다.")
			return
		}
		r.startGame()
	case "vote":
		r.handleVote(c, strings.TrimSpace(msg.Target))
	case "action":
		r.handleNightAction(c, strings.TrimSpace(msg.Target))
	case "sync":
		r.sendState(c)
	default:
		c.pushSystem("알 수 없는 명령입니다.")
	}
}

func (r *Room) handleChat(c *Client, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if !r.state.Active {
		r.broadcast(ServerEvent{Type: "chat", Room: r.name, Author: c.name, Body: text})
		return
	}
	phase := r.state.Phase
	switch phase {
	case PhaseDay, PhaseVote, PhaseDefense:
		r.broadcast(ServerEvent{Type: "chat", Room: r.name, Author: c.name, Body: text})
	case PhaseNight:
		job := r.state.Assign[c.name]
		if job == nil {
			c.pushSystem("밤에는 관전자입니다.")
			return
		}
		if job.Team == "mafia" {
			r.broadcastTeam("mafia", ServerEvent{Type: "chat", Room: r.name, Author: c.name, Body: fmt.Sprintf("[마피아] %s", text)})
		} else {
			c.pushSystem("밤에는 채팅이 제한됩니다.")
		}
	default:
		r.broadcast(ServerEvent{Type: "chat", Room: r.name, Author: c.name, Body: text})
	}
}

func (r *Room) handleNightAction(c *Client, target string) {
	if !r.state.Active || r.state.Phase != PhaseNight {
		c.pushSystem("지금은 능력을 사용할 수 없습니다.")
		return
	}
	job := r.state.Assign[c.name]
	if job == nil {
		c.pushSystem("능력이 없습니다.")
		return
	}
	if !r.state.Alive[c.name] {
		c.pushSystem("사망자는 행동할 수 없습니다.")
		return
	}
	if _, ok := r.state.Alive[target]; !ok {
		c.pushSystem("대상을 찾을 수 없습니다.")
		return
	}
	switch job.Name {
	case "마피아":
		r.state.MafiaPick = target
		c.pushSystem(fmt.Sprintf("%s 님을 지목했습니다.", target))
		r.broadcastTeam("mafia", ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("마피아가 %s 님을 지목했습니다.", target)})
	case "의사":
		r.state.DoctorPick = target
		c.pushSystem(fmt.Sprintf("%s 님을 치료 대상으로 선택했습니다.", target))
	case "경찰":
		r.state.DetectivePick = target
		c.pushSystem(fmt.Sprintf("%s 님을 조사 대상으로 선택했습니다.", target))
	default:
		c.pushSystem("해당 능력은 아직 구현되지 않았습니다.")
	}
}

func (r *Room) handleVote(c *Client, target string) {
	if !r.state.Active || (r.state.Phase != PhaseDay && r.state.Phase != PhaseVote) {
		c.pushSystem("지금은 투표 시간이 아닙니다.")
		return
	}
	if _, ok := r.state.Alive[c.name]; !ok {
		c.pushSystem("사망자는 투표할 수 없습니다.")
		return
	}
	if _, ok := r.state.Alive[target]; !ok {
		c.pushSystem("대상은 생존 중인 플레이어가 아닙니다.")
		return
	}
	if r.state.Vote == nil {
		r.state.Vote = make(map[string]int)
	}
	r.state.Vote[target]++
	r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("%s 님이 %s 에게 표를 던졌습니다.", c.name, target)})
}

func (r *Room) startGame() {
	if r.state.Active {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "이미 게임이 진행 중입니다."})
		return
	}
	if len(r.players) < 4 {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "게임을 시작하려면 최소 4명이 필요합니다."})
		return
	}
	r.state.Reset()
	r.state.Active = true
	r.state.DayCount = 0

	names := make([]string, 0, len(r.players))
	for name := range r.players {
		names = append(names, name)
		r.state.Alive[name] = true
	}
	r.assignRoles(names)
	r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "게임이 시작되었습니다. 첫 번째 밤이 시작됩니다."})
	r.beginNight()
}

func (r *Room) assignRoles(players []string) {
	rng.Shuffle(len(players), func(i, j int) { players[i], players[j] = players[j], players[i] })
	mafiaCount := 1
	if len(players) >= 7 {
		mafiaCount = 2
	}
	jobQueue := make([]string, 0, len(players))
	for i := 0; i < mafiaCount; i++ {
		jobQueue = append(jobQueue, "마피아")
	}
	jobQueue = append(jobQueue, "의사", "경찰")
	for len(jobQueue) < len(players) {
		jobQueue = append(jobQueue, "시민")
	}
	rng.Shuffle(len(jobQueue), func(i, j int) { jobQueue[i], jobQueue[j] = jobQueue[j], jobQueue[i] })
	for idx, player := range players {
		role := jobQueue[idx]
		spec := defaultJobs[role]
		r.state.Assign[player] = &AssignedJob{Name: spec.Name, Team: spec.Team, Desc: spec.Desc}
		if spec.Team == "mafia" {
			r.state.Prefix[player][player] = spec.Name
		}
		r.players[player].push(ServerEvent{Type: "role", Room: r.name, Body: fmt.Sprintf("당신의 직업은 %s 입니다. %s", spec.Name, spec.Desc)})
	}
}

func (r *Room) beginNight() {
	r.state.Phase = PhaseNight
	r.state.MafiaPick = ""
	r.state.DoctorPick = ""
	r.state.DetectivePick = ""
	r.broadcast(ServerEvent{Type: "phase", Room: r.name, Phase: string(r.state.Phase), Body: fmt.Sprintf("%d번째 밤이 시작되었습니다.", r.state.DayCount+1)})
	r.setPhaseTimer(nightDuration, func(room *Room) {
		room.resolveNight()
	})
}

func (r *Room) resolveNight() {
	target := r.state.MafiaPick
	saved := r.state.DoctorPick
	detectiveTarget := r.state.DetectivePick

	if target != "" && target == saved {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("의사가 %s 님을 치료했습니다.", target)})
	} else if target != "" {
		r.eliminate(target, "마피아에게 살해당했습니다.")
	} else {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "마피아가 행동하지 않았습니다."})
	}

	if detectiveTarget != "" {
		if job := r.state.Assign[detectiveTarget]; job != nil {
			if detName := r.findDetective(); detName != "" {
				if detClient, ok := r.players[detName]; ok {
					detClient.push(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("%s 님의 직업은 %s 입니다.", detectiveTarget, job.Name)})
				}
			}
		}
	}

	r.checkGameOver()
	if !r.state.Active {
		return
	}
	r.beginDay()
}
func (r *Room) beginDay() {
	r.state.Phase = PhaseDay
	r.state.DayCount++
	r.state.Vote = make(map[string]int)
	r.broadcast(ServerEvent{Type: "phase", Room: r.name, Phase: string(r.state.Phase), Body: fmt.Sprintf("%d번째 낮이 시작되었습니다. 토론 후 투표가 진행됩니다.", r.state.DayCount)})
	r.setPhaseTimer(dayDuration, func(room *Room) {
		room.beginVote()
	})
}

func (r *Room) beginVote() {
	r.state.Phase = PhaseVote
	r.state.Vote = make(map[string]int)
	r.broadcast(ServerEvent{Type: "phase", Room: r.name, Phase: string(r.state.Phase), Body: "투표 시간이 시작되었습니다. /vote 명령으로 대상 입력"})
	r.setPhaseTimer(voteDuration, func(room *Room) {
		room.resolveVote()
	})
}

func (r *Room) resolveVote() {
	if len(r.state.Vote) == 0 {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "아무도 투표되지 않아 밤으로 넘어갑니다."})
		r.beginNight()
		return
	}
	maxVotes := 0
	winners := make([]string, 0)
	for target, count := range r.state.Vote {
		if count > maxVotes {
			maxVotes = count
			winners = []string{target}
		} else if count == maxVotes {
			winners = append(winners, target)
		}
	}
	sort.Strings(winners)
	if len(winners) != 1 {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "표가 동률이라 밤으로 넘어갑니다."})
		r.beginNight()
		return
	}
	target := winners[0]
	r.eliminate(target, fmt.Sprintf("투표로 인해 %s 님이 처형되었습니다.", target))
	r.checkGameOver()
	if !r.state.Active {
		return
	}
	r.beginNight()
}

func (r *Room) eliminate(name, reason string) {
	if _, ok := r.state.Alive[name]; !ok {
		return
	}
	delete(r.state.Alive, name)
	r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: fmt.Sprintf("%s (%s) %s", name, r.state.Assign[name].Name, reason)})
}

func (r *Room) checkGameOver() {
	mafiaAlive := 0
	citizenAlive := 0
	for name := range r.state.Alive {
		job := r.state.Assign[name]
		if job == nil {
			continue
		}
		if job.Team == "mafia" {
			mafiaAlive++
		} else {
			citizenAlive++
		}
	}
	if mafiaAlive == 0 {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "시민 팀이 승리했습니다!"})
		r.finishGame()
		return
	}
	if mafiaAlive >= citizenAlive {
		r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "마피아 팀이 승리했습니다!"})
		r.finishGame()
	}
}

func (r *Room) finishGame() {
	r.state.Active = false
	r.state.Phase = PhaseLobby
	r.state.Vote = nil
	r.state.MafiaPick = ""
	r.state.DoctorPick = ""
	r.state.DetectivePick = ""
	if r.phaseTimer != nil {
		r.phaseTimer.Stop()
	}
	r.broadcastRoles()
}

func (r *Room) broadcastRoles() {
	arr := make([]string, 0, len(r.state.Assign))
	for name, job := range r.state.Assign {
		arr = append(arr, fmt.Sprintf("%s => %s", name, job.Name))
	}
	sort.Strings(arr)
	r.broadcast(ServerEvent{Type: "log", Room: r.name, Body: "직업 공개: " + strings.Join(arr, ", ")})
}

func (r *Room) broadcast(ev ServerEvent) {
	for _, cl := range r.players {
		cl.push(ev)
	}
}

func (r *Room) broadcastTeam(team string, ev ServerEvent) {
	for name := range r.state.Assign {
		job := r.state.Assign[name]
		if job != nil && job.Team == team {
			if cl, ok := r.players[name]; ok {
				cl.push(ev)
			}
		}
	}
}

func (r *Room) pushRoster() {
	order := make([]string, 0, len(r.players))
	for _, name := range r.order {
		if _, ok := r.players[name]; ok {
			order = append(order, name)
		}
	}
	r.broadcast(ServerEvent{Type: "roster", Room: r.name, State: order})
}

func (r *Room) sendState(c *Client) {
	snapshot := map[string]interface{}{
		"phase":  r.state.Phase,
		"active": r.state.Active,
		"day":    r.state.DayCount,
	}
	c.push(ServerEvent{Type: "state", Room: r.name, State: snapshot})
}

func (r *Room) setPhaseTimer(d time.Duration, fn func(*Room)) {
	if r.phaseTimer != nil {
		r.phaseTimer.Stop()
	}
	r.phaseTimer = time.AfterFunc(d, func() {
		r.enqueue(fn)
	})
}

func (r *Room) findDetective() string {
	for name, job := range r.state.Assign {
		if job != nil && job.Name == "경찰" {
			return name
		}
	}
	return ""
}

func appendUnique(slice []string, v string) []string {
	for _, existing := range slice {
		if existing == v {
			return slice
		}
	}
	return append(slice, v)
}
