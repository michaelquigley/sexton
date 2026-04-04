package agent

type State int

const (
	Watching State = iota
	Syncing
	Error
	Snoozed
	Holdout
)

func (s State) String() string {
	switch s {
	case Watching:
		return "watching"
	case Syncing:
		return "syncing"
	case Error:
		return "error"
	case Snoozed:
		return "snoozed"
	case Holdout:
		return "holdout"
	default:
		return "unknown"
	}
}
