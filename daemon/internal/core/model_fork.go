// Fork, Route, and the types and
// stored properties of BeltLayout; the layout methods are in
// model_fork_layout.go.

package core

import (
	"slices"
)

// Fork is where a belt splits into routes that meet again. A run takes one of
// them: the first, unless an outcome before the fork takes another. A fork at
// the start of a belt starts every route. The stages on a route name it as
// their `route`; they sit together, route after route, and the stage after
// them is where the routes meet. A route may hold another fork: Parent is that
// route, and a fork with no parent is on the line, as every fork was before
// forks could nest. A route may have no stages.
//
// The keys are id, routes, parent and at, with parent and at omitted when nil.
type Fork struct {
	ID     string  `json:"id"`
	Routes []Route `json:"routes"`
	// Parent is the route this fork sits on. Nil on the line, including forks
	// saved before nesting.
	Parent *string `json:"parent,omitzero"`
	// At is where the fork's stages start in the belt's list. Nil on older
	// forks, which sit where their stages are.
	At *int `json:"at,omitzero"`
}

// Route is one route of a fork: a sequence of stages that name it. feeds is
// written only when true.
//
// A route that feeds into the stage after its fork: nothing on the line splits
// into it, and a run only gets there by going to one of its stages, from an
// outcome or with Send to. It may never be reached.
type Route struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Feeds bool   `json:"feeds,omitzero"`
}

// BeltLayout is a belt's stages and forks, read as the ways a run can go
// through them.
type BeltLayout struct {
	Stages []Stage
	Forks  []Fork
}

// BeltLayoutPiece is one step of a containing sequence: a stage on that route,
// or a fork nested in it. Exactly one of Stage and Fork is set; Kind says which.
// The stage and fork are these fields, nil for the other case.
type BeltLayoutPiece struct {
	Kind  BeltLayoutPieceKind
	Stage *Stage
	Fork  *Fork
}

// BeltLayoutPieceKind is the case of a BeltLayoutPiece.
type BeltLayoutPieceKind string

const (
	// BeltLayoutPieceStage is a stage piece.
	BeltLayoutPieceStage BeltLayoutPieceKind = "stage"
	// BeltLayoutPieceFork is a fork piece.
	BeltLayoutPieceFork BeltLayoutPieceKind = "fork"
)

// BeltLayoutStep is where a run goes: a stage, or nil at the end of the belt,
// and the routes it takes on the way there, keyed by fork.
type BeltLayoutStep struct {
	// Stage is the stage ID, or nil at the end of the belt.
	Stage *string
	// Routes maps a fork ID to the route IDs taken through it.
	Routes map[string][]string
}

// BeltLayoutWay is a fork and the route taken through it.
type BeltLayoutWay struct {
	Fork  Fork
	Route Route
}

// BeltLayoutSegment is one piece of the belt as it is drawn: one stage, or a
// fork whose routes are themselves sequences of segments. Kind says which case;
// Stage is set for a stage, and Fork and Routes for a fork (Routes holds one
// sequence per route, in the fork's route order).
type BeltLayoutSegment struct {
	Kind   BeltLayoutSegmentKind
	Stage  *Stage
	Fork   *Fork
	Routes [][]BeltLayoutSegment
}

// BeltLayoutSegmentKind is the case of a BeltLayoutSegment.
type BeltLayoutSegmentKind string

const (
	// BeltLayoutSegmentStage is a stage segment.
	BeltLayoutSegmentStage BeltLayoutSegmentKind = "stage"
	// BeltLayoutSegmentFork is a fork segment.
	BeltLayoutSegmentFork BeltLayoutSegmentKind = "fork"
)

// Stages is the stages in the segment, in order: the stage itself, or every
// stage of every route of a fork.
func (s BeltLayoutSegment) Stages() []Stage {
	if s.Kind == BeltLayoutSegmentStage {
		return []Stage{*s.Stage}
	}
	var out []Stage
	for _, route := range s.Routes {
		for _, seg := range route {
			out = append(out, seg.Stages()...)
		}
	}
	return out
}

// IntRange is an inclusive range of positions.
type IntRange struct {
	Lower int
	Upper int
}

// Nested reports whether any fork sits inside a route.
func (l BeltLayout) Nested() bool {
	return slices.ContainsFunc(l.Forks, func(f Fork) bool { return f.Parent != nil })
}

// Layout is the layout of a belt's stages and forks.
func (b Belt) Layout() BeltLayout {
	return BeltLayout{Stages: b.Stages, Forks: b.Forks}
}

// Clone is a deep copy of the fork, with its routes and parent name.
func (f Fork) Clone() Fork {
	return cloneValue(f)
}

// Clone is a copy of the route. A route holds no slices or pointers, so the
// copy is the value itself; the method exists for the copy points that clone
// every value they store.
func (r Route) Clone() Route {
	return r
}
