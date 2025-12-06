package jobs

import (
	"errors"
)

type politicianJob struct{ spec Spec }

func NewPolitician(spec Spec) Job { return &politicianJob{spec: spec} }

func (j *politicianJob) Name() string                            { return j.spec.Name }
func (j *politicianJob) Team() Team                              { return j.spec.Team }
func (j *politicianJob) Description() string                     { return j.spec.Desc }
func (j *politicianJob) NightAction(ctx *Context) error          { return errors.New("능력이 없습니다.") }
func (j *politicianJob) OnNightResolved(ctx *NightResultContext) {}
func (j *politicianJob) OnDayStart(ctx *PhaseContext)            {}
func (j *politicianJob) OnVote(ctx *VoteContext) {
	ctx.Room.AddVote(ctx.Target, 1)
}
func (j *politicianJob) OnDeath(ctx *DeathContext) bool {
	if ctx.CauseType == "vote" {
		ctx.Room.Broadcast(ServerEvent{Type: EventTypeLog, Room: ctx.Room.Name(), Body: "정치인은 투표로 죽지 않습니다."})
		ctx.Room.PushSystem(ctx.Victim, "투표로 처형되지 않습니다.")
		return true
	}
	return false
}
