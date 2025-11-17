package jobs

import "fmt"

type detectiveJob struct{ spec Spec }

func NewDetective(spec Spec) Job { return &detectiveJob{spec: spec} }

func (j *detectiveJob) Name() string        { return j.spec.Name }
func (j *detectiveJob) Team() string        { return j.spec.Team }
func (j *detectiveJob) Description() string { return j.spec.Desc }
func (j *detectiveJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("detective", ctx.Target)
	result := "마피아가 아닙니다."
	if job := ctx.Room.LookupJob(ctx.Target); job != nil && job.Team() == "mafia" {
		result = "마피아 입니다."
	}
	ctx.Room.PushSystem(ctx.Actor, fmt.Sprintf("%s 님은 %s", ctx.Target, result))
	return nil
}
