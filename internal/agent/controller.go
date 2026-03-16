package agent

import "path/filepath"

// FindAgent returns the agent matching the given repo identifier, which can be
// a full path or a basename. returns nil if no match is found.
func (c *Container) FindAgent(repo string) *Agent {
	for _, a := range c.Agents {
		if a.cfg.Name == repo || a.cfg.Path == repo || filepath.Base(a.cfg.Path) == repo {
			return a
		}
	}
	return nil
}
