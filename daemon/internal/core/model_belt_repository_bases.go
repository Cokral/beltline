// Repository base branches: a belt's per-repository override of its shared base
// branch. The overrides are keyed by "owner/repository" and are only kept for
// repositories some trigger entry lists.

package core

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// RepositoryBasesClean is the overrides with each branch trimmed and every empty
// one dropped, as they are stored and run. Nil when none remain, so an empty
// map is never written.
func RepositoryBasesClean(bases map[string]string) map[string]string {
	var out map[string]string
	for repo, branch := range bases {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[repo] = branch
	}
	return out
}

// ListedRepositories is the distinct repositories the belt's trigger entries
// list, in first-appearance order across the entries. Repeats are listed once.
func (b Belt) ListedRepositories() []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, entry := range b.Triggers {
		for _, repo := range entry.Repos {
			if _, ok := seen[repo]; ok {
				continue
			}
			seen[repo] = struct{}{}
			out = append(out, repo)
		}
	}
	return out
}

// RepositoryBase is the belt's override for repo, or "" when it has none.
func (b Belt) RepositoryBase(repo string) string {
	return RepositoryBasesClean(b.RepositoryBases)[repo]
}

// pruneRepositoryBases drops every override whose repository no trigger entry
// lists any more. An override survives while another entry still lists its
// repository, and is gone once its last listing is removed, so a repository
// added back starts without one.
func (b *Belt) pruneRepositoryBases() {
	if len(b.RepositoryBases) == 0 {
		return
	}
	listed := map[string]struct{}{}
	for _, repo := range b.ListedRepositories() {
		listed[repo] = struct{}{}
	}
	kept := map[string]string{}
	for repo, branch := range b.RepositoryBases {
		if _, ok := listed[repo]; ok {
			kept[repo] = branch
		}
	}
	b.RepositoryBases = RepositoryBasesClean(kept)
}

// RepositoryBaseProblems are the overrides a save refuses: each one for a
// repository no trigger entry lists. Each names its repository as the field.
// Empty overrides are not problems; they are dropped on save.
func RepositoryBaseProblems(belt Belt) []LocatedProblem {
	listed := map[string]struct{}{}
	for _, repo := range belt.ListedRepositories() {
		listed[repo] = struct{}{}
	}
	var problems []LocatedProblem
	for _, repo := range slices.Sorted(maps.Keys(RepositoryBasesClean(belt.RepositoryBases))) {
		if _, ok := listed[repo]; ok {
			continue
		}
		field := "repositoryBase:" + repo
		problems = append(problems, LocatedProblem{
			Message:  fmt.Sprintf("%s has a base branch, but no trigger lists it. Add the repository or remove its base branch.", repo),
			Location: new(ProblemLocation{Kind: "belt", Field: &field}),
		})
	}
	return problems
}
