package jobs

type policeJob struct{ spec Spec }

func NewPolice(spec Spec) Job { return &policeJob{spec: spec} }

func (j *policeJob) Name() string        { return j.spec.Name }
func (j *policeJob) Team() Team          { return j.spec.Team }
func (j *policeJob) Description() string { return j.spec.Desc }
func (j *policeJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("detective", ctx.Target)
	result := "마피아가 아닙니다."
	if job := ctx.Room.LookupJob(ctx.Target); job != nil && job.Name() == "마피아" {
		result = "마피아 입니다."
	}
	ctx.Room.PushSystem(ctx.Actor, result)
	return nil
}
func (j *policeJob) OnNightResolved(ctx *NightResultContext) {}
func (j *policeJob) OnDayStart(ctx *PhaseContext)            {}
func (j *policeJob) OnVote(ctx *VoteContext)                 {}
func (j *policeJob) OnDeath(ctx *DeathContext) bool          { return false }
