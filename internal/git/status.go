package git

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type StatusEntry struct {
	Path    string
	OldPath string
	X       byte
	Y       byte
}

func (e StatusEntry) IsTracked() bool {
	return e.X != '?' || e.Y != '?'
}

func (e StatusEntry) IsStaged() bool {
	return strings.ContainsRune("MADRCT", rune(e.X))
}

type Status struct {
	Branch    string
	Ahead     int
	Behind    int
	Modified  []string
	Added     []string
	Deleted   []string
	Untracked []string
	Entries   []StatusEntry
}

func NewStatus() *Status {
	return &Status{
		Modified:  make([]string, 0),
		Added:     make([]string, 0),
		Deleted:   make([]string, 0),
		Untracked: make([]string, 0),
		Entries:   make([]StatusEntry, 0),
	}
}

func (s *Status) HasChanges() bool {
	return len(s.Modified) > 0 || len(s.Added) > 0 ||
		len(s.Deleted) > 0 || len(s.Untracked) > 0
}

func (s *Status) ChangeCount() int {
	return len(s.Modified) + len(s.Added) + len(s.Deleted) + len(s.Untracked)
}

func (s *Status) TrackedPaths() []string {
	var paths []string
	for _, entry := range s.Entries {
		if !entry.IsTracked() {
			continue
		}
		if (entry.X == 'R' || entry.Y == 'R') && entry.OldPath != "" {
			paths = append(paths, entry.OldPath)
		}
		paths = append(paths, entry.Path)
	}
	return paths
}

var branchRegex = regexp.MustCompile(`^## ([^.\s]+)(?:\.\.\.(\S+))?(?: \[(.+)\])?$`)
var aheadBehindRegex = regexp.MustCompile(`(ahead|behind) (\d+)`)

func parseStatus(output string) *Status {
	s := NewStatus()
	fields := strings.Split(output, "\x00")

	for i := 0; i < len(fields); {
		field := fields[i]
		if field == "" {
			i++
			continue
		}
		if strings.HasPrefix(field, "## ") {
			s.parseBranchLine(field)
			i++
			continue
		}
		if len(field) < 3 {
			i++
			continue
		}

		entry := StatusEntry{
			X:    field[0],
			Y:    field[1],
			Path: field[3:],
		}
		i++
		if isTwoPathStatus(entry.X, entry.Y) && i < len(fields) {
			entry.OldPath = fields[i]
			i++
		}

		s.Entries = append(s.Entries, entry)
		s.classify(entry.X, entry.Y, entry.Path)
	}

	return s
}

func parseNameStatus(output string) (*Status, error) {
	s := NewStatus()
	fields := strings.Split(output, "\x00")

	for i := 0; i < len(fields); {
		code := strings.TrimLeft(fields[i], "\n")
		if code == "" {
			i++
			continue
		}

		entry := StatusEntry{X: code[0], Y: ' '}
		i++
		if entry.X == 'R' || entry.X == 'C' {
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("name-status entry %q is missing rename or copy paths", code)
			}
			entry.OldPath = fields[i]
			entry.Path = fields[i+1]
			i += 2
		} else {
			if i >= len(fields) {
				return nil, fmt.Errorf("name-status entry %q is missing a path", code)
			}
			entry.Path = fields[i]
			i++
		}

		s.Entries = append(s.Entries, entry)
		s.classify(entry.X, entry.Y, entry.Path)
	}

	return s, nil
}

func isTwoPathStatus(x, y byte) bool {
	return x == 'R' || x == 'C' || y == 'R' || y == 'C'
}

func (s *Status) classify(x, y byte, path string) {
	displayPath := escapeDisplayPath(path)
	switch {
	case x == '?' && y == '?':
		s.Untracked = append(s.Untracked, displayPath)
	case x == 'A' || y == 'A':
		s.Added = append(s.Added, displayPath)
	case x == 'D' || y == 'D':
		s.Deleted = append(s.Deleted, displayPath)
	default:
		s.Modified = append(s.Modified, displayPath)
	}
}

func escapeDisplayPath(path string) string {
	if utf8.ValidString(path) && strings.IndexFunc(path, unicode.IsControl) < 0 {
		return path
	}
	return strconv.Quote(path)
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
