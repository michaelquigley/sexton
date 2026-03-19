package git

import (
	"regexp"
	"strconv"
	"strings"
)

type Status struct {
	Branch    string
	Ahead     int
	Behind    int
	Modified  []string
	Added     []string
	Deleted   []string
	Untracked []string
}

func (s *Status) HasChanges() bool {
	return len(s.Modified) > 0 || len(s.Added) > 0 ||
		len(s.Deleted) > 0 || len(s.Untracked) > 0
}

func (s *Status) ChangeCount() int {
	return len(s.Modified) + len(s.Added) + len(s.Deleted) + len(s.Untracked)
}

var branchRegex = regexp.MustCompile(`^## ([^.\s]+)(?:\.\.\.(\S+))?(?: \[(.+)\])?$`)
var aheadBehindRegex = regexp.MustCompile(`(ahead|behind) (\d+)`)

func parseStatus(output string) *Status {
	s := &Status{
		Modified:  make([]string, 0),
		Added:     make([]string, 0),
		Deleted:   make([]string, 0),
		Untracked: make([]string, 0),
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "## ") {
			s.parseBranchLine(line)
			continue
		}

		if len(line) < 3 {
			continue
		}

		x := line[0]
		y := line[1]
		filename := strings.TrimSpace(line[3:])

		if strings.Contains(filename, " -> ") {
			parts := strings.Split(filename, " -> ")
			if len(parts) == 2 {
				filename = parts[1]
			}
		}

		switch {
		case x == '?' && y == '?':
			s.Untracked = append(s.Untracked, filename)
		case x == 'A' || y == 'A':
			s.Added = append(s.Added, filename)
		case x == 'D' || y == 'D':
			s.Deleted = append(s.Deleted, filename)
		case x == 'M' || y == 'M' || x == 'R' || y == 'R' || x == 'C' || y == 'C':
			s.Modified = append(s.Modified, filename)
		default:
			s.Modified = append(s.Modified, filename)
		}
	}

	return s
}

func (s *Status) parseBranchLine(line string) {
	matches := branchRegex.FindStringSubmatch(line)
	if len(matches) < 2 {
		s.Branch = strings.TrimPrefix(line, "## ")
		if idx := strings.Index(s.Branch, "..."); idx > 0 {
			s.Branch = s.Branch[:idx]
		}
		return
	}

	s.Branch = matches[1]

	if len(matches) >= 4 && matches[3] != "" {
		abMatches := aheadBehindRegex.FindAllStringSubmatch(matches[3], -1)
		for _, m := range abMatches {
			if len(m) >= 3 {
				count, _ := strconv.Atoi(m[2])
				switch m[1] {
				case "ahead":
					s.Ahead = count
				case "behind":
					s.Behind = count
				}
			}
		}
	}
}
