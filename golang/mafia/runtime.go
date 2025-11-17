package main

import (
	"github.com/gosuda/portal-toys/mafia/jobs"
)

var jobRegistry = map[string]jobs.Factory{
	"마피아": jobs.NewMafia,
	"의사":  jobs.NewDoctor,
	"경찰":  jobs.NewPolice,
	"군인":  jobs.NewSoldier,
	"정치인": jobs.NewPolitician,
	"시민":  jobs.NewCitizen,
}

func buildJob(spec JobSpec) jobs.Job {
	factory := jobRegistry[spec.Name]
	if factory == nil {
		factory = jobs.NewCitizen
	}
	return factory(jobs.Spec{Name: spec.Name, Team: spec.Team, Desc: spec.Desc})
}

// jobRoomAdapter bridges Room to jobs.RoomState.
type jobRoomAdapter struct {
	r *Room
}

func (r *Room) jobAdapter() jobs.RoomState {
	return &jobRoomAdapter{r: r}
}

func (a *jobRoomAdapter) Name() string { return a.r.name }

func (a *jobRoomAdapter) IsAlive(name string) bool {
	return a.r.state.Alive[name]
}

func (a *jobRoomAdapter) PushSystem(name, msg string) {
	if cl, ok := a.r.players[name]; ok {
		cl.pushSystem(msg)
	}
}

func (a *jobRoomAdapter) Broadcast(ev jobs.ServerEvent) {
	a.r.broadcast(ServerEvent{Type: ev.Type, Room: ev.Room, Body: ev.Body, Phase: ev.Phase, Author: ev.Author})
}

func (a *jobRoomAdapter) BroadcastTeam(team jobs.Team, ev jobs.ServerEvent) {
	a.r.broadcastTeam(team, ServerEvent{Type: ev.Type, Room: ev.Room, Body: ev.Body})
}

func (a *jobRoomAdapter) SetNightTarget(key, value string) {
	switch key {
	case "mafia":
		a.r.state.MafiaPick = value
	case "doctor":
		a.r.state.DoctorPick = value
	case "detective":
		a.r.state.DetectivePick = value
	}
}

func (a *jobRoomAdapter) LookupJob(name string) jobs.Job {
	return a.r.state.Runtime[name]
}

func (a *jobRoomAdapter) SetMeta(key, value string) {
	if a.r.state.Meta == nil {
		a.r.state.Meta = make(map[string]string)
	}
	a.r.state.Meta[key] = value
}

func (a *jobRoomAdapter) GetMeta(key string) string {
	if a.r.state.Meta == nil {
		return ""
	}
	return a.r.state.Meta[key]
}

func (a *jobRoomAdapter) AddVote(target string, delta int) {
	if a.r.state.Vote == nil {
		a.r.state.Vote = make(map[string]int)
	}
	a.r.state.Vote[target] += delta
}
