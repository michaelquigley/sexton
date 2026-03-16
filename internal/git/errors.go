package git

import "errors"

var (
	ErrNotARepo        = errors.New("not a git repository")
	ErrConflict        = errors.New("merge conflict detected")
	ErrDirtyWorkingTree = errors.New("uncommitted changes present")
	ErrNothingToCommit = errors.New("nothing to commit")
	ErrPushFailed      = errors.New("push failed")
	ErrPullFailed      = errors.New("pull failed")
	ErrNoRemote        = errors.New("no remote configured")
)
