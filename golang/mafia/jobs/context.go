package jobs

// Team represents the alignment of a role.
type Team string

const (
	TeamCitizen Team = "citizen"
	TeamMafia   Team = "mafia"
	TeamSect    Team = "sect"
	TeamNeutral Team = "neutral"
)

const (
	EventTypeLog = "log"
)

// Context carries runtime information for job actions.
type Context struct {
	Room   RoomState
	Actor  string
	Target string
	Meta   map[string]string
}

// RoomState is implemented by the Go Room adapter so jobs can interact with state.
type RoomState interface {
	Name() string
	IsAlive(name string) bool
	PushSystem(name, msg string)
	Broadcast(ev ServerEvent)
	BroadcastTeam(team Team, ev ServerEvent)
	SetNightTarget(key, value string)
	LookupJob(name string) Job
	SetMeta(key, value string)
	GetMeta(key string) string
	AddVote(target string, delta int)
}

// ServerEvent mirrors the Room broadcast payload (subset used by jobs).
type ServerEvent struct {
	Type   string
	Room   string
	Body   string
	Phase  string
	Author string
}

// Job represents a playable role.
type Job interface {
	Name() string
	Team() Team
	Description() string
	NightAction(ctx *Context) error
	OnNightResolved(ctx *NightResultContext)
	OnDayStart(ctx *PhaseContext)
	OnVote(ctx *VoteContext)
	OnDeath(ctx *DeathContext) bool
}

// Factory creates a job instance from spec metadata.
type Factory func(spec Spec) Job

// Spec defines the metadata pulled from reference data.
type Spec struct {
	Name string
	Team Team
	Desc string
}

// Additional lifecycle contexts for future hooks.
type NightResultContext struct {
	Room RoomState
	Meta map[string]string
}

type PhaseContext struct {
	Room RoomState
	Meta map[string]string
}

type VoteContext struct {
	Room   RoomState
	Actor  string
	Target string
	Meta   map[string]string
}

type DeathContext struct {
	Room      RoomState
	Victim    string
	Cause     string
	CauseType string
	Meta      map[string]string
}
