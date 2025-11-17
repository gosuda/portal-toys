package main

import "github.com/gosuda/portal-toys/mafia/jobs"

// JobSpec describes a role pulled from reference data.
type JobSpec struct {
	Name string
	Team jobs.Team
	Desc string
}

var defaultJobs = map[string]JobSpec{
	"마피아": {
		Name: "마피아",
		Team: jobs.TeamMafia,
		Desc: "밤마다 한 명을 지목해 처형하려 시도합니다. 팀 간 비밀 채팅 가능.",
	},
	"의사": {
		Name: "의사",
		Team: jobs.TeamCitizen,
		Desc: "밤마다 한 명을 선택해 마피아의 공격을 1회 막습니다.",
	},
	"경찰": {
		Name: "경찰",
		Team: jobs.TeamCitizen,
		Desc: "밤마다 한 명을 조사해 그 사람의 직업을 확인합니다.",
	},
	"군인": {
		Name: "군인",
		Team: jobs.TeamCitizen,
		Desc: "마피아의 공격을 한 번 버텨낼 수 있습니다.",
	},
	"정치인": {
		Name: "정치인",
		Team: jobs.TeamCitizen,
		Desc: "투표로 처형당하지 않으며 투표권이 두 표로 인정됩니다.",
	},
	"시민": {
		Name: "시민",
		Team: jobs.TeamCitizen,
		Desc: "능력은 없지만 토론과 투표로 마피아를 색출합니다.",
	},
}
