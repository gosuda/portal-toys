package jobs

import (
	"errors"
	"fmt"
)

type soldierJob struct{ spec Spec }

func NewSoldier(spec Spec) Job { return &soldierJob{spec: spec} }

func (j *soldierJob) Name() string                            { return j.spec.Name }
func (j *soldierJob) Team() Team                              { return j.spec.Team }
func (j *soldierJob) Description() string                     { return j.spec.Desc }
func (j *soldierJob) NightAction(ctx *Context) error          { return errors.New("능력이 없습니다.") }
func (j *soldierJob) OnNightResolved(ctx *NightResultContext) {}
func (j *soldierJob) OnDayStart(ctx *PhaseContext)            {}
func (j *soldierJob) OnVote(ctx *VoteContext)                 {}
func (j *soldierJob) OnDeath(ctx *DeathContext) bool {
	key := "soldier_respawn_" + ctx.Victim
	if ctx.Room.GetMeta(key) == "used" {
		return false
	}
	ctx.Room.SetMeta(key, "used")
	ctx.Room.PushSystem(ctx.Victim, "마피아의 공격을 버텨냈습니다.")
	ctx.Room.Broadcast(ServerEvent{Type: "log", Room: ctx.Room.Name(), Body: fmt.Sprintf("[ %s ] 님이 마피아의 공격을 버텨 냈습니다.", ctx.Victim)})
	return true
}
