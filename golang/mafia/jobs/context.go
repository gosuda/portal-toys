package jobs

// Context carries runtime information for job actions.
type Context struct {
	Room   RoomState
	Actor  string
	Target string
}

// RoomState is implemented by the Go Room adapter so jobs can interact with state.
type RoomState interface {
	Name() string
	IsAlive(name string) bool
	PushSystem(name, msg string)
	Broadcast(ev ServerEvent)
	BroadcastTeam(team string, ev ServerEvent)
	SetNightTarget(key, value string)
	LookupJob(name string) Job
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
	Team() string
	Description() string
	NightAction(ctx *Context) error
}

// Factory creates a job instance from spec metadata.
type Factory func(spec Spec) Job

// Spec defines the metadata pulled from reference data.
type Spec struct {
	Name string
	Team string
	Desc string
}
