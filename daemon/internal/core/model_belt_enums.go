// StageKind, Workspace, Fallback and
// AfkMode: the enums a belt's stages and runs use, with their labels.

package core

// StageKind is the kind a stage type belongs to.
type StageKind string

const (
	// StageKindAgent is an agent stage: an agent session writes its output.
	StageKindAgent StageKind = "agent"
	// StageKindGate is an approval: a person decides.
	StageKindGate StageKind = "gate"
	// StageKindWatch is a watch: it waits on something outside the run.
	StageKindWatch StageKind = "watch"
)

// Label is the name shown for the kind.
func (k StageKind) Label() string {
	switch k {
	case StageKindAgent:
		return "Agent"
	case StageKindGate:
		return "Approval"
	case StageKindWatch:
		return "Watcher"
	}
	return ""
}

// Workspace says where an agent stage works.
type Workspace string

const (
	// WorkspaceReadOnly is a read-only checkout of the base branch.
	WorkspaceReadOnly Workspace = "readOnly"
	// WorkspaceWorktree is a worktree on `beltline/issue-<n>`.
	WorkspaceWorktree Workspace = "worktree"
	// WorkspaceWorktreeReadOnly is the run's worktree, read-only.
	WorkspaceWorktreeReadOnly Workspace = "worktreeReadOnly"
)

// AllWorkspace lists every Workspace in declaration order (CaseIterable).
var AllWorkspace = []Workspace{WorkspaceReadOnly, WorkspaceWorktree, WorkspaceWorktreeReadOnly}

// Known reports whether x is one of the cases of Workspace.
func (x Workspace) Known() bool { return enumKnown(x, AllWorkspace) }

// Label describes the workspace.
func (w Workspace) Label() string {
	switch w {
	case WorkspaceReadOnly:
		return "Read-only checkout of the base branch"
	case WorkspaceWorktree:
		return "Worktree on beltline/issue-<n>"
	case WorkspaceWorktreeReadOnly:
		return "Same worktree, read-only"
	}
	return ""
}

// IsReadOnly reports whether the workspace is read-only.
func (w Workspace) IsReadOnly() bool { return w != WorkspaceWorktree }

// Fallback says what happens when a stage's session fails or ends without a
// known outcome. Approvals and watches have no fallback.
type Fallback string

const (
	// FallbackHold holds the run for a person.
	FallbackHold Fallback = "hold"
	// FallbackRetry runs the stage again, once.
	FallbackRetry Fallback = "retry"
	// FallbackDrop drops the run.
	FallbackDrop Fallback = "drop"
)

// AllFallback lists every Fallback in declaration order (CaseIterable).
var AllFallback = []Fallback{FallbackHold, FallbackRetry, FallbackDrop}

// Known reports whether x is one of the cases of Fallback.
func (x Fallback) Known() bool { return enumKnown(x, AllFallback) }

// Label is the name shown for the fallback.
func (f Fallback) Label() string {
	switch f {
	case FallbackHold:
		return "Hold for me"
	case FallbackRetry:
		return "Run this stage again, once"
	case FallbackDrop:
		return "Drop the run"
	}
	return ""
}

// AfkMode is whether a run is in AFK mode, or whether a belt's new runs start
// in it: always, never, or as the global switch says.
type AfkMode string

const (
	// AfkModeAlways is in AFK mode, whatever the global switch.
	AfkModeAlways AfkMode = "always"
	// AfkModeNever is never in AFK mode, whatever the global switch.
	AfkModeNever AfkMode = "never"
	// AfkModeGlobal is in AFK mode while the global switch is on.
	AfkModeGlobal AfkMode = "global"
)

// AllAfkMode lists every AfkMode in declaration order (CaseIterable).
var AllAfkMode = []AfkMode{AfkModeAlways, AfkModeNever, AfkModeGlobal}

// Known reports whether x is one of the cases of AfkMode.
func (x AfkMode) Known() bool { return enumKnown(x, AllAfkMode) }

// AfkModeFromChoice is a run's saved choice as a mode: true is always, false
// never, nil global.
func AfkModeFromChoice(choice *bool) AfkMode {
	switch {
	case choice == nil:
		return AfkModeGlobal
	case *choice:
		return AfkModeAlways
	}
	return AfkModeNever
}

// Label is the button text for the mode.
func (m AfkMode) Label() string {
	switch m {
	case AfkModeAlways:
		return "AFK: Always"
	case AfkModeNever:
		return "AFK: Never"
	case AfkModeGlobal:
		return "AFK: Global"
	}
	return ""
}

// Next is the mode a click on the button moves to.
func (m AfkMode) Next() AfkMode {
	switch m {
	case AfkModeGlobal:
		return AfkModeAlways
	case AfkModeAlways:
		return AfkModeNever
	case AfkModeNever:
		return AfkModeGlobal
	}
	return m
}

// RunChoice is the choice a run saves: nil follows the global switch.
func (m AfkMode) RunChoice() *bool {
	t, f := true, false
	switch m {
	case AfkModeAlways:
		return &t
	case AfkModeNever:
		return &f
	}
	return nil
}
