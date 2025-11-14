package main

// JobSpec represents a simplified subset of the reference logic.
type JobSpec struct {
	Name string
	Team string // mafia or citizen
	Desc string
}

var defaultJobs = map[string]JobSpec{
	"마피아": {
		Name: "마피아",
		Team: "mafia",
		Desc: "밤마다 한 명을 지목해 처형하려 시도합니다. 팀 간 비밀 채팅 가능.",
	},
	"의사": {
		Name: "의사",
		Team: "citizen",
		Desc: "밤마다 한 명을 선택해 마피아의 공격을 1회 막습니다.",
	},
	"경찰": {
		Name: "경찰",
		Team: "citizen",
		Desc: "밤마다 한 명을 조사해 그 사람의 직업을 확인합니다.",
	},
	"시민": {
		Name: "시민",
		Team: "citizen",
		Desc: "능력은 없지만 토론과 투표로 마피아를 색출합니다.",
	},
}
