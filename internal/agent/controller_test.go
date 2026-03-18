package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/michaelquigley/sexton/internal/config"
)

func TestResolveAgentMatchesExplicitName(t *testing.T) {
	c := &Container{
		Agents: []*Agent{
			newResolvedAgent("/repos/alpha/grimoire", "research", true),
			newResolvedAgent("/repos/beta/grimoire", "grimoire", false),
		},
	}

	ag, err := c.ResolveAgent("research")
	if err != nil {
		t.Fatalf("ResolveAgent() error = %v", err)
	}
	if got := ag.Path(); got != "/repos/alpha/grimoire" {
		t.Fatalf("ResolveAgent() path = %q, want %q", got, "/repos/alpha/grimoire")
	}
}

func TestResolveAgentMatchesFullPath(t *testing.T) {
	c := &Container{
		Agents: []*Agent{
			newResolvedAgent("/repos/alpha/grimoire", "research", true),
			newResolvedAgent("/repos/beta/grimoire", "grimoire", false),
		},
	}

	ag, err := c.ResolveAgent("/repos/beta/grimoire")
	if err != nil {
		t.Fatalf("ResolveAgent() error = %v", err)
	}
	if got := ag.Path(); got != "/repos/beta/grimoire" {
		t.Fatalf("ResolveAgent() path = %q, want %q", got, "/repos/beta/grimoire")
	}
}

func TestResolveAgentUsesUniqueBasenameFallback(t *testing.T) {
	c := &Container{
		Agents: []*Agent{
			newResolvedAgent("/repos/alpha/grimoire", "research", true),
			newResolvedAgent("/repos/beta/archive", "archive", false),
		},
	}

	ag, err := c.ResolveAgent("archive")
	if err != nil {
		t.Fatalf("ResolveAgent() error = %v", err)
	}
	if got := ag.Path(); got != "/repos/beta/archive" {
		t.Fatalf("ResolveAgent() path = %q, want %q", got, "/repos/beta/archive")
	}
}

func TestResolveAgentPrefersExplicitNameOverBasename(t *testing.T) {
	c := &Container{
		Agents: []*Agent{
			newResolvedAgent("/repos/alpha/research", "archive", true),
			newResolvedAgent("/repos/beta/archive", "archive", false),
		},
	}

	ag, err := c.ResolveAgent("archive")
	if err != nil {
		t.Fatalf("ResolveAgent() error = %v", err)
	}
	if got := ag.Path(); got != "/repos/alpha/research" {
		t.Fatalf("ResolveAgent() path = %q, want %q", got, "/repos/alpha/research")
	}
}

func TestResolveAgentFailsOnAmbiguousBasename(t *testing.T) {
	c := &Container{
		Agents: []*Agent{
			newResolvedAgent("/repos/alpha/grimoire", "grimoire", false),
			newResolvedAgent("/repos/beta/grimoire", "grimoire", false),
		},
	}

	_, err := c.ResolveAgent("grimoire")
	if err == nil {
		t.Fatal("ResolveAgent() error = nil, want ambiguous repo error")
	}
	if !errors.Is(err, ErrAmbiguousRepo) {
		t.Fatalf("ResolveAgent() error = %v, want ErrAmbiguousRepo", err)
	}

	var lookupErr *LookupError
	if !errors.As(err, &lookupErr) {
		t.Fatalf("ResolveAgent() error = %v, want LookupError", err)
	}
	if len(lookupErr.Matches) != 2 {
		t.Fatalf("ResolveAgent() matches = %v, want 2 matches", lookupErr.Matches)
	}
}

func TestResolveAgentFailsWhenRepoNotFound(t *testing.T) {
	c := &Container{Agents: []*Agent{newResolvedAgent("/repos/alpha/grimoire", "research", true)}}

	_, err := c.ResolveAgent("missing")
	if err == nil {
		t.Fatal("ResolveAgent() error = nil, want not found")
	}
	if !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("ResolveAgent() error = %v, want ErrRepoNotFound", err)
	}
}

func TestValidateRepoIdentifiersRejectsDuplicateConfiguredNames(t *testing.T) {
	agents := []*Agent{
		newResolvedAgent("/repos/alpha", "research", true),
		newResolvedAgent("/repos/beta", "research", true),
	}

	err := validateRepoIdentifiers(agents)
	if err == nil {
		t.Fatal("validateRepoIdentifiers() error = nil, want duplicate configured name error")
	}
	if !strings.Contains(err.Error(), `duplicate configured repo name "research"`) {
		t.Fatalf("validateRepoIdentifiers() error = %q", err)
	}
}

func TestValidateRepoIdentifiersAllowsDuplicateDefaultBasenames(t *testing.T) {
	agents := []*Agent{
		newResolvedAgent("/repos/alpha/grimoire", "grimoire", false),
		newResolvedAgent("/repos/beta/grimoire", "grimoire", false),
	}

	if err := validateRepoIdentifiers(agents); err != nil {
		t.Fatalf("validateRepoIdentifiers() error = %v, want nil", err)
	}
}

func newResolvedAgent(path, name string, explicitName bool) *Agent {
	return &Agent{
		cfg: &config.ResolvedRepo{
			Path:         path,
			Name:         name,
			ExplicitName: explicitName,
		},
	}
}
