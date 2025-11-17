package jobs

import "fmt"

type mafiaJob struct{ spec Spec }

func NewMafia(spec Spec) Job { return &mafiaJob{spec: spec} }

func (j *mafiaJob) Name() string        { return j.spec.Name }
func (j *mafiaJob) Team() string        { return j.spec.Team }
func (j *mafiaJob) Description() string { return j.spec.Desc }
func (j *mafiaJob) NightAction(ctx *Context) error {
	ctx.Room.SetNightTarget("mafia", ctx.Target)
	ctx.Room.PushSystem(ctx.Actor, fmt.Sprintf("%s 님을 지목했습니다.", ctx.Target))
	ctx.Room.BroadcastTeam("mafia", ServerEvent{Type: "log", Room: ctx.Room.Name(), Body: fmt.Sprintf("마피아가 %s 님을 지목했습니다.", ctx.Target)})
	return nil
}
