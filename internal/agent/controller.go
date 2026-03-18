package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

var ErrRepoNotFound = errors.New("repo not found")
var ErrAmbiguousRepo = errors.New("ambiguous repo")

type LookupError struct {
	Query   string
	Matches []string
	err     error
}

func (e *LookupError) Error() string {
	if errors.Is(e.err, ErrAmbiguousRepo) {
		return fmt.Sprintf("ambiguous repo %q; matches: %s; use configured name or full path", e.Query, strings.Join(e.Matches, ", "))
	}
	return fmt.Sprintf("repo not found: %q", e.Query)
}

func (e *LookupError) Unwrap() error {
	return e.err
}

// ResolveAgent returns the agent matching the given repo identifier. Explicit
// configured names and full paths are stable identifiers. A basename remains a
// convenience fallback only when it resolves to exactly one repo.
func (c *Container) ResolveAgent(repo string) (*Agent, error) {
	if ag, err := matchSingle(repo, c.Agents, func(a *Agent) bool {
		return a.cfg.Path == repo
	}); ag != nil || err != nil {
		return ag, err
	}

	if ag, err := matchSingle(repo, c.Agents, func(a *Agent) bool {
		return a.cfg.ExplicitName && a.cfg.Name == repo
	}); ag != nil || err != nil {
		return ag, err
	}

	if ag, err := matchSingle(repo, c.Agents, func(a *Agent) bool {
		return filepath.Base(a.cfg.Path) == repo
	}); ag != nil || err != nil {
		return ag, err
	}

	return nil, &LookupError{Query: repo, err: ErrRepoNotFound}
}

func matchSingle(query string, agents []*Agent, match func(*Agent) bool) (*Agent, error) {
	var matches []*Agent
	for _, ag := range agents {
		if match(ag) {
			matches = append(matches, ag)
		}
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		return nil, &LookupError{
			Query:   query,
			Matches: matchDescriptions(matches),
			err:     ErrAmbiguousRepo,
		}
	}
}

func matchDescriptions(matches []*Agent) []string {
	descriptions := make([]string, 0, len(matches))
	for _, ag := range matches {
		if ag.cfg.ExplicitName {
			descriptions = append(descriptions, fmt.Sprintf("%s (%s)", ag.cfg.Name, ag.cfg.Path))
			continue
		}
		descriptions = append(descriptions, ag.cfg.Path)
	}
	sort.Strings(descriptions)
	return descriptions
}
