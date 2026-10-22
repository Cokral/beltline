// The stage moves: where a stage
// goes on the belt, and removing one.

package core

// End is where a stage added at the end of route goes in the list (see
// BeltLayout.End).
func (b Belt) End(route string) int {
	return b.Layout().End(route)
}

// MoveTo moves a stage to the end of route, or, when route is nil, off its
// fork to just after that fork, on the route the fork sits in.
func (b *Belt) MoveTo(stageID string, route *string) {
	index := b.stageIndex(stageID)
	if index < 0 || SamePointee(b.Stages[index].Route, route) {
		return
	}
	layout := b.Layout()
	place := layout.Place(stageID)
	var span *IntRange
	if place != nil {
		span = layout.Span(place.Fork)
	}
	stage := b.Stages[index]
	b.Stages = append(append([]Stage(nil), b.Stages[:index]...), b.Stages[index+1:]...)
	if route != nil {
		stage.Route = CopyPointer(route)
		b.Stages = forkInsertStage(b.Stages, min(b.End(*route), len(b.Stages)), stage)
	} else {
		stage.Route = nil
		if place != nil {
			stage.Route = CopyPointer(place.Fork.Parent)
		}
		after := index
		if span != nil {
			after = span.Upper
		}
		b.Stages = forkInsertStage(b.Stages, min(after, len(b.Stages)), stage)
	}
	b.Tidy()
}

// MoveOnto moves a stage to where target is, onto target's route.
func (b *Belt) MoveOnto(stageID string, targetID string) {
	if stageID == targetID {
		return
	}
	from := b.stageIndex(stageID)
	to := b.stageIndex(targetID)
	if from < 0 || to < 0 {
		return
	}
	stage := b.Stages[from]
	b.Stages = append(append([]Stage(nil), b.Stages[:from]...), b.Stages[from+1:]...)
	stage.Route = CopyPointer(b.Stages[b.stageIndex(targetID)].Route)
	b.Stages = forkInsertStage(b.Stages, to, stage)
	b.Tidy()
}

// Shift moves a stage one step earlier (-1) or later (1): along its route, past
// a whole nested fork, or on the line, past a whole fork.
func (b *Belt) Shift(stageID string, step int) {
	index := b.stageIndex(stageID)
	if index < 0 {
		return
	}
	stage := b.Stages[index]
	layout := b.Layout()
	if stage.Route != nil {
		route := CopyPointer(stage.Route)
		items := layout.Pieces(route)
		at := layout.indexOfStagePiece(items, stageID)
		if at < 0 || at+step < 0 || at+step >= len(items) {
			return
		}
		items[at], items[at+step] = items[at+step], items[at]
		b.Stages = b.flatten(nil, &pieceOverride{Route: route, Items: items})
		b.Tidy()
		return
	}
	var pieces [][]Stage
	for _, seg := range layout.Segments() {
		pieces = append(pieces, seg.Stages())
	}
	at := -1
	for i, p := range pieces {
		if len(p) == 1 && p[0].ID == stageID {
			at = i
			break
		}
	}
	if at < 0 || at+step < 0 || at+step >= len(pieces) {
		return
	}
	pieces[at], pieces[at+step] = pieces[at+step], pieces[at]
	var flat []Stage
	for _, p := range pieces {
		flat = append(flat, p...)
	}
	b.Stages = flat
	b.Tidy()
}

// Remove removes a stage. A fork left with no stages, and nothing nested that
// has any, goes too.
func (b *Belt) Remove(stageID string) {
	var stages []Stage
	for _, s := range b.Stages {
		if s.ID != stageID {
			stages = append(stages, s.Clone())
		}
	}
	b.Stages = stages
	b.Tidy()
}
