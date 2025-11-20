package main

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosuda/portal-toys/mafia/jobs"
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
	Active       bool
	Phase        GamePhase
	DayCount     int
	Alive        map[string]bool
	Jobs         map[string]*JobSpec
	Assign       map[string]*AssignedJob
	Runtime      map[string]jobs.Job
	Prefix       map[string]map[string]string
	Vote         map[string]int
	VoteUsed     map[string]int
	NightTargets map[string]string
	Execution    *ExecutionState
	Meta         map[string]string
}

type AssignedJob struct {
	Name    string
	Team    jobs.Team
	Desc    string
	Passive string
}

type ExecutionState struct {
	Target string
	Agree  int
	Oppose int
	Voted  map[string]bool
}

type RosterState struct {
	Players []string `json:"players"`
	Host    string   `json:"host"`
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
	gs.Runtime = make(map[string]jobs.Job)
	gs.Meta = make(map[string]string)
	gs.Assign = make(map[string]*AssignedJob)
	gs.Prefix = make(map[string]map[string]string)
	gs.Vote = make(map[string]int)
	gs.VoteUsed = make(map[string]int)
	gs.NightTargets = make(map[string]string)
	gs.Execution = nil
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
	r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("[ %s ] 방에 %s 님이 입장했습니다. (인원 %d명)", r.name, c.name, len(r.players))})
	r.pushRoster()
	if r.state.Active && !r.state.Alive[c.name] {
		c.pushSystem("진행 중인 게임이 있어 관전자 상태입니다.")
	}
}

func (r *Room) removePlayer(c *Client) {
	delete(r.players, c.name)
	r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("%s 님이 퇴장했습니다.", c.name)})
	r.pushRoster()
	if len(r.players) == 0 {
		r.manager.removeRoom(r.name, r)
		r.close()
		return
	}
	if c.name == r.host {
		r.host = r.pickNextHost()
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("방장이 %s 님으로 변경되었습니다.", r.host)})
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
	case "decision":
		r.handleDecision(c, msg.Text)
	case "admin":
		r.handleAdmin(c, msg)
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
		r.broadcast(ServerEvent{Type: EventTypeChat, Room: r.name, Author: c.name, Body: text})
		return
	}
	phase := r.state.Phase
	switch phase {
	case PhaseDay, PhaseVote, PhaseDefense:
		r.broadcast(ServerEvent{Type: EventTypeChat, Room: r.name, Author: c.name, Body: text})
	case PhaseNight:
		job := r.state.Assign[c.name]
		if job == nil {
			c.pushSystem("밤에는 관전자입니다.")
			return
		}
		if job.Team == jobs.TeamMafia {
			r.broadcastTeam(jobs.TeamMafia, ServerEvent{Type: EventTypeChat, Room: r.name, Author: c.name, Body: fmt.Sprintf("[마피아] %s", text)})
		} else {
			c.pushSystem("밤에는 채팅이 제한됩니다.")
		}
	default:
		r.broadcast(ServerEvent{Type: EventTypeChat, Room: r.name, Author: c.name, Body: text})
	}
}

func (r *Room) handleNightAction(c *Client, target string) {
	if !r.state.Active || r.state.Phase != PhaseNight {
		c.pushSystem("지금은 능력을 사용할 수 없습니다.")
		return
	}
	job := r.state.Runtime[c.name]
	if !r.state.Alive[c.name] {
		c.pushSystem("사망자는 행동할 수 없습니다.")
		return
	}
	if _, ok := r.state.Alive[target]; !ok {
		c.pushSystem("대상을 찾을 수 없습니다.")
		return
	}
	if job == nil {
		c.pushSystem("능력이 없습니다.")
		return
	}
	ctx := &jobs.Context{
		Room:   r.jobAdapter(),
		Actor:  c.name,
		Target: target,
		Meta:   r.state.Meta,
	}

	if err := job.NightAction(ctx); err != nil {
		c.pushSystem(err.Error())
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
	dayIndex := r.state.DayCount
	if dayIndex == 0 {
		dayIndex = 1
	}
	if r.state.VoteUsed == nil {
		r.state.VoteUsed = make(map[string]int)
	}
	if r.state.VoteUsed[c.name] == dayIndex {
		c.pushSystem("이미 투표했습니다.")
		return
	}
	r.state.VoteUsed[c.name] = dayIndex
	r.state.Vote[target]++
	if job := r.state.Runtime[c.name]; job != nil {
		ctx := &jobs.VoteContext{Room: r.jobAdapter(), Actor: c.name, Target: target, Meta: r.state.Meta}
		job.OnVote(ctx)
	}

	r.broadcast(ServerEvent{
		Type: EventTypeLog,
		Room: r.name,
		Body: fmt.Sprintf("%s님 1표!", target),
	})
}

func (r *Room) startGame() {
	if r.state.Active {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "이미 게임이 진행 중입니다."})
		return
	}
	if len(r.players) < 4 {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "게임을 시작하려면 최소 4명이 필요합니다."})
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
	r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "게임이 시작되었습니다. 첫 번째 밤이 시작됩니다."})
	r.beginNight()
}

func (r *Room) assignRoles(players []string) {
	rng.Shuffle(len(players), func(i, j int) { players[i], players[j] = players[j], players[i] })
	jobQueue := buildRoleQueue(len(players))
	rng.Shuffle(len(jobQueue), func(i, j int) { jobQueue[i], jobQueue[j] = jobQueue[j], jobQueue[i] })
	for idx, player := range players {
		role := jobQueue[idx]
		spec := defaultJobs[role]
		if r.state.Prefix[player] == nil {
			r.state.Prefix[player] = make(map[string]string)
		}
		r.state.Assign[player] = &AssignedJob{Name: spec.Name, Team: spec.Team, Desc: spec.Desc}
		r.state.Runtime[player] = buildJob(spec)
		if spec.Team == jobs.TeamMafia {
			r.state.Prefix[player][player] = spec.Name
		}
		r.players[player].push(ServerEvent{Type: EventTypeRole, Room: r.name, Body: fmt.Sprintf("당신의 직업은 %s 입니다. %s", spec.Name, spec.Desc)})
	}
}

func (r *Room) beginNight() {
	r.state.Phase = PhaseNight
	if r.state.NightTargets == nil {
		r.state.NightTargets = make(map[string]string)
	} else {
		for k := range r.state.NightTargets {
			delete(r.state.NightTargets, k)
		}
	}
	nightIndex := strconv.Itoa(r.state.DayCount + 1)
	if r.state.Meta == nil {
		r.state.Meta = make(map[string]string)
	}
	r.state.Meta["night_counter"] = nightIndex
	r.broadcast(ServerEvent{Type: EventTypePhase, Room: r.name, Phase: string(r.state.Phase), Body: fmt.Sprintf("%d번째 밤이 시작되었습니다.", r.state.DayCount+1)})
	r.setPhaseTimer(nightDuration, func(room *Room) {
		room.resolveNight()
	})
}

func (r *Room) resolveNight() {
	target := r.state.NightTargets["mafia"]
	saved := r.state.NightTargets["doctor"]

	if target != "" && target == saved {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("의사가 %s 님을 치료했습니다.", target)})
	} else if target != "" {
		r.eliminate(target, "마피아에게 살해당했습니다.", "mafia")
	} else {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "아무 일도 일어나지 않았습니다."})
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
	r.state.VoteUsed = make(map[string]int)
	r.broadcast(ServerEvent{Type: EventTypePhase, Room: r.name, Phase: string(r.state.Phase), Body: fmt.Sprintf("%d번째 낮이 시작되었습니다. 토론 후 투표가 진행됩니다.", r.state.DayCount)})
	r.setPhaseTimer(dayDuration, func(room *Room) {
		room.beginVote()
	})
}

func (r *Room) beginVote() {
	r.state.Phase = PhaseVote
	r.state.Vote = make(map[string]int)
	r.broadcast(ServerEvent{Type: EventTypePhase, Room: r.name, Phase: string(r.state.Phase), Body: "투표 시간이 시작되었습니다. /vote 명령으로 대상 입력"})
	r.setPhaseTimer(voteDuration, func(room *Room) {
		room.resolveVote()
	})
}

func (r *Room) resolveVote() {
	if len(r.state.Vote) == 0 {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "아무도 투표되지 않아 밤으로 넘어갑니다."})
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
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "표가 동률이라 밤으로 넘어갑니다."})
		r.beginNight()
		return
	}
	target := winners[0]
	r.beginDefense(target)
}

func (r *Room) beginDefense(target string) {
	r.state.Phase = PhaseDefense
	r.state.Execution = &ExecutionState{Target: target, Voted: make(map[string]bool)}
	r.broadcast(ServerEvent{Type: EventTypePhase, Room: r.name, Phase: string(r.state.Phase), Body: fmt.Sprintf("%s 님의 최후 변론 시간입니다.", target)})
	r.setPhaseTimer(defenseDuration, func(room *Room) {
		room.beginExecutionVote()
	})
}

func (r *Room) beginExecutionVote() {
	if r.state.Execution == nil {
		r.beginNight()
		return
	}
	r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("%s 님을 처형할지 agree/oppose 로 투표해 주세요.", r.state.Execution.Target)})
	r.setPhaseTimer(defenseDuration, func(room *Room) {
		room.resolveDefense()
	})
}

func (r *Room) eliminate(name, reason, cause string) {
	if _, ok := r.state.Alive[name]; !ok {
		return
	}
	if job := r.state.Runtime[name]; job != nil {
		ctx := &jobs.DeathContext{Room: r.jobAdapter(), Victim: name, Cause: reason, CauseType: cause, Meta: r.state.Meta}
		if job.OnDeath(ctx) {
			return
		}
	}
	delete(r.state.Alive, name)
	r.broadcast(ServerEvent{
		Type: EventTypeLog,
		Room: r.name,
		Body: fmt.Sprintf("%s %s", name, reason),
	})
}

func (r *Room) checkGameOver() {
	mafiaAlive := 0
	citizenAlive := 0
	for name := range r.state.Alive {
		job := r.state.Assign[name]
		if job == nil {
			continue
		}
		if job.Team == jobs.TeamMafia {
			mafiaAlive++
		} else {
			citizenAlive++
		}
	}
	if mafiaAlive == 0 {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "시민 팀이 승리했습니다!"})
		r.finishGame()
		return
	}
	if mafiaAlive >= citizenAlive {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "마피아 팀이 승리했습니다!"})
		r.finishGame()
	}
}

func (r *Room) finishGame() {
	r.state.Active = false
	r.state.Phase = PhaseLobby
	r.state.Vote = nil
	r.state.NightTargets = make(map[string]string)
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
	r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "직업 공개: " + strings.Join(arr, ", ")})
}

func (r *Room) handleDecision(c *Client, text string) {
	if r.state.Execution == nil {
		c.pushSystem("현재 처형 투표가 없습니다.")
		return
	}
	if !r.state.Active || r.state.Phase != PhaseDefense {
		c.pushSystem("지금은 처형 투표 시간이 아닙니다.")
		return
	}
	if !r.state.Alive[c.name] {
		c.pushSystem("사망자는 투표할 수 없습니다.")
		return
	}
	exec := r.state.Execution
	if exec.Voted[c.name] {
		c.pushSystem("이미 의견을 제출했습니다.")
		return
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "agree", "찬성":
		exec.Agree++
		exec.Voted[c.name] = true
		c.pushSystem("찬성하였습니다.")
	case "oppose", "반대":
		exec.Oppose++
		exec.Voted[c.name] = true
		c.pushSystem("반대하였습니다.")
	default:
		c.pushSystem("agree/oppose 로 입력해 주세요.")
	}
}

func (r *Room) resolveDefense() {
	exec := r.state.Execution
	if exec == nil {
		r.beginNight()
		return
	}
	if exec.Agree >= exec.Oppose {
		r.eliminate(exec.Target, fmt.Sprintf("찬성 %d : 반대 %d 로 처형되었습니다.", exec.Agree, exec.Oppose), "vote")
	} else {
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("%s 님은 처형을 모면했습니다.", exec.Target)})
	}
	r.state.Execution = nil
	r.checkGameOver()
	if !r.state.Active {
		return
	}
	r.beginNight()
}

func (r *Room) broadcast(ev ServerEvent) {
	for _, cl := range r.players {
		cl.push(ev)
	}
}

func (r *Room) broadcastTeam(team jobs.Team, ev ServerEvent) {
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
	state := RosterState{Players: order, Host: r.host}
	r.broadcast(ServerEvent{Type: EventTypeRoster, Room: r.name, State: state})
}

func (r *Room) sendState(c *Client) {
	snapshot := map[string]interface{}{
		"phase":  r.state.Phase,
		"active": r.state.Active,
		"day":    r.state.DayCount,
	}
	c.push(ServerEvent{Type: EventTypeState, Room: r.name, State: snapshot})
}

func (r *Room) handleAdmin(c *Client, msg ClientMessage) {
	if c.name != r.host {
		c.pushSystem("방장만 사용할 수 있습니다.")
		return
	}
	action := strings.ToLower(strings.TrimSpace(msg.Action))
	target := strings.TrimSpace(msg.Target)
	switch action {
	case "kick":
		if target == "" {
			c.pushSystem("추방할 대상 닉네임을 지정하세요.")
			return
		}
		if target == c.name {
			c.pushSystem("자기 자신은 추방할 수 없습니다.")
			return
		}
		if victim, ok := r.players[target]; ok {
			victim.pushSystem("방장에 의해 강퇴되었습니다.")
			victim.close()
			r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: fmt.Sprintf("%s 님이 강퇴되었습니다.", target)})
		} else {
			c.pushSystem("해당 플레이어가 존재하지 않습니다.")
		}
	case "end":
		r.broadcast(ServerEvent{Type: EventTypeLog, Room: r.name, Body: "방장이 게임을 종료했습니다."})
		r.finishGame()
	default:
		c.pushSystem("지원하지 않는 관리자 명령입니다.")
	}
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

func buildRoleQueue(count int) []string {
	mafiaCount := 1
	if count >= 7 {
		mafiaCount = 2
	}
	queue := make([]string, 0, count)
	for i := 0; i < mafiaCount && len(queue) < count; i++ {
		queue = append(queue, defaultMafiaRole)
	}
	for _, role := range defaultRolePriority {
		if role == defaultMafiaRole {
			continue
		}
		if len(queue) >= count {
			break
		}
		queue = append(queue, role)
	}
	for len(queue) < count {
		queue = append(queue, defaultCitizenRole)
	}
	return queue
}
