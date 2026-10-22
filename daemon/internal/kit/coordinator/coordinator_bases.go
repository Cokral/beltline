package coordinator

import (
	"context"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
)

// Bases works out the branch a run's work starts from and merges into, in each
// of its repositories. A `base:` label or a belt's base names a branch that may
// live in only some of them, such as an epic's feature branch opened where the
// epic changes code; the others use their default branch. A part of Coordinator
// whose methods run with c.mu held and suspend around GitHub calls.
type Bases struct {
	// c is the coordinator the bases belong to (the back pointer).
	c      *Coordinator
	github github.GitHub
	// defaults are each repository's default branch, once asked.
	defaults map[string]string
	// found holds the branches seen on a repository, as "<repo>|<branch>". A
	// missing one is asked again, since it may be pushed later.
	found map[string]struct{}
}

// newBases builds the base resolver over github.
// Lock: runs during New, before the coordinator is shared.
func newBases(c *Coordinator, github github.GitHub) Bases {
	return Bases{c: c, github: github, defaults: map[string]string{}, found: map[string]struct{}{}}
}

// base returns the branch run's work starts from in repo: the branch requested
// there when it exists, otherwise the repository's default.
// A requested branch that is missing goes straight to the default, not to a
// lower-priority choice. Lock: runs with c.mu held. Suspends: exists, defaultBranch.
func (b *Bases) base(ctx context.Context, run core.Run, repo string) (string, error) {
	if requested := run.RequestedBase(repo); requested != nil && b.exists(ctx, *requested, repo) {
		return *requested, nil
	}
	return b.defaultBranch(ctx, repo)
}

// exists reports whether the branch is in repo. One GitHub cannot answer for
// counts as there, so a failing lookup keeps the branch asked for.
// Lock: runs with c.mu held. Suspends: github.branchExists.
func (b *Bases) exists(ctx context.Context, branch, repo string) bool {
	key := repo + "|" + branch
	if _, ok := b.found[key]; ok {
		return true
	}
	var found *bool
	var err error
	b.c.Suspend(func() { found, err = b.github.BranchExists(ctx, repo, branch) })
	if err != nil || found == nil {
		return true
	}
	if !*found {
		return false
	}
	b.found[key] = struct{}{}
	return true
}

// defaultBranch returns repo's default branch, asking GitHub once.
// Lock: runs with c.mu held. Suspends: github.defaultBranch.
func (b *Bases) defaultBranch(ctx context.Context, repo string) (string, error) {
	if known, ok := b.defaults[repo]; ok {
		return known, nil
	}
	var branch string
	var err error
	b.c.Suspend(func() { branch, err = b.github.DefaultBranch(ctx, repo) })
	if err != nil {
		return "", err
	}
	b.defaults[repo] = branch
	return branch, nil
}
