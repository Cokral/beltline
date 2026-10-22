package coordinator

import (
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// sessionsBranchBase is the ref a nested leg's branch is created from: the
// enclosing route's branch, or nil for a top-level leg, which starts from the base.
// Lock: none (pure).
func sessionsBranchBase(run core.Run, leg *core.Leg) *string {
	if leg == nil || leg.Join == nil {
		return nil
	}
	var join *core.Join
	for index := range run.Joins {
		if run.Joins[index].ID == *leg.Join {
			join = &run.Joins[index]
			break
		}
	}
	if join == nil {
		return nil
	}
	fork := run.Layout().Fork(join.ForkID)
	if fork == nil || fork.Parent == nil || join.Into == nil || *join.Into == "" {
		return nil
	}
	into := *join.Into
	return &into
}

// sessionsBranch is the branch a leg works on: the one it already has, or the
// run's own branch with the route's name after it once a run works more than one
// route at a time.
// Lock: none (pure).
func sessionsBranch(run core.Run, leg *core.Leg) string {
	if leg != nil && leg.Branch != nil && *leg.Branch != "" {
		return *leg.Branch
	}
	var legID *string
	if leg != nil {
		legID = &leg.ID
	}
	id := core.LegFromID(legID)
	if id == nil {
		if run.Branch != nil {
			return *run.Branch
		}
		return core.RunBranch(run)
	}
	slug := branchSlug(strings.ReplaceAll(*id, "r:", ""))
	return core.RunBranch(run) + "-" + slug
}

// branchSlug replaces each run of characters that are not ASCII letters or digits with one hyphen.
// Lock: none (pure).
func branchSlug(text string) string {
	var b strings.Builder
	inRun := false
	for _, r := range text {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if alnum {
			b.WriteRune(r)
			inRun = false
			continue
		}
		if !inRun {
			b.WriteByte('-')
		}
		inRun = true
	}
	return b.String()
}
