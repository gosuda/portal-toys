package jobs

import "fmt"

// Broadcast helpers from jobs runtime.

type BaseRoom interface {
	Name() string
	PushSystem(name, msg string)
	Broadcast(ev ServerEvent)
	BroadcastTeam(team string, ev ServerEvent)
}

// Utility to format selection DM.
func NotifySelection(room BaseRoom, actor, target string, action string) {
	room.PushSystem(actor, fmt.Sprintf("%s 님을 %s 대상으로 선택했습니다.", target, action))
}
