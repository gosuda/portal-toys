package jobs

import "errors"

// passiveJob is used for roles without active skills (e.g., citizens).
type passiveJob struct{ spec Spec }

func NewCitizen(spec Spec) Job { return &passiveJob{spec: spec} }

func (j *passiveJob) Name() string        { return j.spec.Name }
func (j *passiveJob) Team() Team          { return j.spec.Team }
func (j *passiveJob) Description() string { return j.spec.Desc }
func (j *passiveJob) NightAction(ctx *Context) error {
	return errors.New("능력이 없습니다.")
}

func (j *passiveJob) OnNightResolved(ctx *NightResultContext) {}
func (j *passiveJob) OnDayStart(ctx *PhaseContext)            {}
func (j *passiveJob) OnVote(ctx *VoteContext)                 {}
func (j *passiveJob) OnDeath(ctx *DeathContext) bool          { return false }
