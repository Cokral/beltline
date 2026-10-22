package core

// Input is something a stage needs from its run before it can work. A belt's
// trigger provides some, and earlier stages produce others.
type Input string

const (
	// InputBaseBranch is a base branch the run's own branch can start from.
	InputBaseBranch Input = "baseBranch"
	// InputBranch is code beyond the base to read: the run's worktree, or the
	// head of the pull request under review.
	InputBranch Input = "branch"
	// InputPullRequest is a pull request to watch: the one under review, or one
	// an agent opens from the run's own branch.
	InputPullRequest Input = "pullRequest"
	// InputTicket is an issue or ticket to watch: the one the run is about.
	InputTicket Input = "ticket"
)

// AllInput lists every Input in declaration order (CaseIterable).
var AllInput = []Input{InputBaseBranch, InputBranch, InputPullRequest, InputTicket}

// Label is what the app calls the input after "needs".
func (i Input) Label() string {
	switch i {
	case InputBaseBranch:
		return "a base branch"
	case InputBranch:
		return "code to read"
	case InputPullRequest:
		return "a pull request"
	case InputTicket:
		return "an issue or ticket"
	}
	return ""
}

// Has reports whether the run has input now, for the stage it is at.
func (r Run) Has(input Input) bool {
	switch input {
	case InputBaseBranch, InputBranch:
		return true
	case InputPullRequest:
		if about := r.Subject.About(); about != nil && about.Kind == ResourceRefKindPullRequest {
			return true
		}
		return r.Branch != nil
	case InputTicket:
		about := r.Subject.About()
		return about != nil && (about.Kind == ResourceRefKindIssue || about.Kind == ResourceRefKindTicket)
	}
	return false
}

// Provides is what this trigger kind's runs start with.
func (k TriggerKind) Provides() map[Input]struct{} {
	switch k {
	case TriggerKindIssues, TriggerKindJira:
		return inputSet(InputBaseBranch, InputTicket)
	case TriggerKindSchedule, TriggerKindManual:
		return inputSet(InputBaseBranch)
	case TriggerKindReviews:
		return inputSet(InputPullRequest, InputBranch)
	}
	return map[Input]struct{}{}
}

// Needs is what a stage of this type needs, apart from what its workspace does.
func (t StageType) Needs() map[Input]struct{} {
	switch t {
	case StageTypeAgent, StageTypeApprove:
		return map[Input]struct{}{}
	case StageTypeWatch:
		return inputSet(InputPullRequest)
	}
	return map[Input]struct{}{}
}

// Needs is what an agent working in this workspace needs.
func (w Workspace) Needs() map[Input]struct{} {
	switch w {
	case WorkspaceReadOnly:
		return map[Input]struct{}{}
	case WorkspaceWorktree:
		return inputSet(InputBaseBranch)
	case WorkspaceWorktreeReadOnly:
		return inputSet(InputBranch)
	}
	return map[Input]struct{}{}
}

// Needs is what this stage needs from the trigger or an earlier stage.
func (s Stage) Needs() map[Input]struct{} {
	if s.WatchesTicket() {
		return inputSet(InputTicket)
	}
	needs := s.Type.Needs()
	if s.Kind() == StageKindAgent {
		for in := range s.Workspace.Needs() {
			needs[in] = struct{}{}
		}
	}
	return needs
}

// Produces is what this stage gives the stages after it: a branch, and the
// pull request an agent opens from it.
func (s Stage) Produces() map[Input]struct{} {
	if s.ChangesCode() {
		return inputSet(InputBranch, InputPullRequest)
	}
	return map[Input]struct{}{}
}

// CanHave reports whether a stage of typ can be on this belt somewhere: its
// needs are available for every trigger, or what an agent in a worktree produces
// when every trigger can start a branch.
func (b Belt) CanHave(typ StageType) bool {
	return InputsSubset(typ.Needs(), b.MenuInputs())
}

// InputsSubset reports whether every input in a is in b. The stage menus and
// the belt draft checks use it to see whether a belt provides what a stage needs.
func InputsSubset(a, b map[Input]struct{}) bool {
	for input := range a {
		if _, ok := b[input]; !ok {
			return false
		}
	}
	return true
}

// inputSet builds a set from inputs.
func inputSet(inputs ...Input) map[Input]struct{} {
	set := make(map[Input]struct{}, len(inputs))
	for _, in := range inputs {
		set[in] = struct{}{}
	}
	return set
}
