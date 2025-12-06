package jobs

import "errors"

type policeJob struct{ spec Spec }

func NewPolice(spec Spec) Job { return &policeJob{spec: spec} }

func (j *policeJob) Name() string        { return j.spec.Name }
func (j *policeJob) Team() Team          { return j.spec.Team }
func (j *policeJob) Description() string { return j.spec.Desc }
func (j *policeJob) NightAction(ctx *Context) error {
	night := ""
	if ctx.Meta != nil {
		night = ctx.Meta["night_counter"]
	}

	usedKey := "police_used_" + ctx.Actor
	if night != "" && ctx.Meta != nil && ctx.Meta[usedKey] == night {
		return errors.New("이번 밤에는 이미 조사를 진행했습니다.")
	}

	ctx.Room.SetNightTarget("police", ctx.Target)
	result := "마피아가 아닙니다."
	if job := ctx.Room.LookupJob(ctx.Target); job != nil && job.Name() == "마피아" {
		result = "마피아 입니다."
	}

	ctx.Room.PushSystem(ctx.Actor, result)
	if night != "" {
		ctx.Room.SetMeta(usedKey, night)
	}
	return nil
}
func (j *policeJob) OnNightResolved(ctx *NightResultContext) {}
func (j *policeJob) OnDayStart(ctx *PhaseContext)            {}
func (j *policeJob) OnVote(ctx *VoteContext)                 {}
func (j *policeJob) OnDeath(ctx *DeathContext) bool          { return false }
