package jobs

import "fmt"

type doctorJob struct{ spec Spec }

func NewDoctor(spec Spec) Job { return &doctorJob{spec: spec} }

func (j *doctorJob) Name() string        { return j.spec.Name }
func (j *doctorJob) Team() string        { return j.spec.Team }
func (j *doctorJob) Description() string { return j.spec.Desc }
func (j *doctorJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("doctor", ctx.Target)
	ctx.Room.PushSystem(ctx.Actor, fmt.Sprintf("%s 님을 치료 대상으로 선택했습니다.", ctx.Target))
	return nil
}
