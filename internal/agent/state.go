package agent

type State int

const (
	Watching State = iota
	Syncing
	Halted
	Snoozed
)

func (s State) String() string {
	switch s {
	case Watching:
		return "watching"
	case Syncing:
		return "syncing"
	case Halted:
		return "halted"
	case Snoozed:
		return "snoozed"
	default:
		return "unknown"
	}
}
