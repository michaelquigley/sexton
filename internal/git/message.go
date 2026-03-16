package git

import (
	"fmt"
	"strings"
)

func GenerateCommitMessage(status *Status) string {
	const prefix = "sexton: "

	if status == nil {
		return prefix + "update files"
	}

	adds := len(status.Added) + len(status.Untracked)
	mods := len(status.Modified)
	dels := len(status.Deleted)

	var parts []string
	if adds > 0 {
		if adds == 1 {
			parts = append(parts, "add 1 file")
		} else {
			parts = append(parts, fmt.Sprintf("add %d files", adds))
		}
	}
	if mods > 0 {
		if mods == 1 {
			parts = append(parts, "update 1 file")
		} else {
			parts = append(parts, fmt.Sprintf("update %d files", mods))
		}
	}
	if dels > 0 {
		if dels == 1 {
			parts = append(parts, "remove 1 file")
		} else {
			parts = append(parts, fmt.Sprintf("remove %d files", dels))
		}
	}

	if len(parts) == 0 {
		return prefix + "update files"
	}
	return prefix + strings.Join(parts, ", ")
}
