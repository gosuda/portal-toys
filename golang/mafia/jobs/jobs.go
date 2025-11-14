package jobs

import (
	"errors"
	"fmt"
)

// Context carries state needed by job hooks.
type Context struct {
	Room   RoomState
	Actor  string
	Target string
}

type RoomState interface {
	Name() string
	IsAlive(name string) bool
	PushSystem(name, msg string)
	Broadcast(ev ServerEvent)
	BroadcastTeam(team string, ev ServerEvent)
	SetNightTarget(key, value string)
	LookupJob(name string) Job
}

type ServerEvent struct {
	Type   string
	Room   string
	Body   string
	Phase  string
	Author string
}

// Job defines runtime behavior for a role.
type Job interface {
	Name() string
	Team() string
	Description() string
	NightAction(ctx *Context) error
}

// Factory builds a job instance per player.
type Factory func(spec Spec) Job

type Spec struct {
	Name string
	Team string
	Desc string
}

func NewCitizen(spec Spec) Job { return &passiveJob{spec: spec} }

func NewMafia(spec Spec) Job     { return &mafiaJob{spec: spec} }
func NewDoctor(spec Spec) Job    { return &doctorJob{spec: spec} }
func NewDetective(spec Spec) Job { return &detectiveJob{spec: spec} }

// passive Job ------------------------------------------------

type passiveJob struct{ spec Spec }

func (j *passiveJob) Name() string        { return j.spec.Name }
func (j *passiveJob) Team() string        { return j.spec.Team }
func (j *passiveJob) Description() string { return j.spec.Desc }
func (j *passiveJob) NightAction(ctx *Context) error {
	return errors.New("능력이 없습니다.")
}

// mafia ------------------------------------------------------

type mafiaJob struct{ spec Spec }

func (j *mafiaJob) Name() string        { return j.spec.Name }
func (j *mafiaJob) Team() string        { return j.spec.Team }
func (j *mafiaJob) Description() string { return j.spec.Desc }
func (j *mafiaJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("mafia", ctx.Target)
	ctx.Room.PushSystem(ctx.Actor, fmt.Sprintf("%s 님을 지목했습니다.", ctx.Target))
	ctx.Room.BroadcastTeam("mafia", ServerEvent{Type: "log", Room: ctx.Room.Name(), Body: fmt.Sprintf("마피아가 %s 님을 지목했습니다.", ctx.Target)})
	return nil
}

// doctor -----------------------------------------------------

type doctorJob struct{ spec Spec }

func (j *doctorJob) Name() string        { return j.spec.Name }
func (j *doctorJob) Team() string        { return j.spec.Team }
func (j *doctorJob) Description() string { return j.spec.Desc }
func (j *doctorJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("doctor", ctx.Target)
	ctx.Room.PushSystem(ctx.Actor, fmt.Sprintf("%s 님을 치료 대상으로 선택했습니다.", ctx.Target))
	return nil
}

// detective --------------------------------------------------

type detectiveJob struct{ spec Spec }

func (j *detectiveJob) Name() string        { return j.spec.Name }
func (j *detectiveJob) Team() string        { return j.spec.Team }
func (j *detectiveJob) Description() string { return j.spec.Desc }
func (j *detectiveJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("detective", ctx.Target)
	txt := "마피아가 아닙니다."
	if job := ctx.Room.LookupJob(ctx.Target); job != nil && job.Team() == "mafia" {
		txt = "마피아 입니다."
	}
	ctx.Room.PushSystem(ctx.Actor, fmt.Sprintf("%s 님은 %s", ctx.Target, txt))
	return nil
}
