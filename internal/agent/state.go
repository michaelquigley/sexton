package agent

type State int

const (
	Watching State = iota
	Syncing
	Halted
)

func (s State) String() string {
	switch s {
	case Watching:
		return "watching"
	case Syncing:
		return "syncing"
	case Halted:
		return "halted"
	default:
		return "unknown"
	}
}
