// The methods of BeltLayout, and the Run layout methods.
// the types are in model_fork.go.
//
// Naming: overloads become distinct Go names. `fork(_:)` is Fork,
// `fork(withRoute:)` is ForkWithRoute, `fork(after:)` is ForkAfter, `route(_:)`
// is Route, `take(_ route:)` is Take and `take(_ routes:)` is TakeRoutes,
// `resume(after:)` is Resume, `path(of:)` is PathOf and `path(_:longest:)` is
// Path. Private overloads keep lower-case names.

package core

// Fork returns the fork with id, or nil.
func (l BeltLayout) Fork(id string) *Fork {
	for i := range l.Forks {
		if l.Forks[i].ID == id {
			f := l.Forks[i].Clone()
			return &f
		}
	}
	return nil
}

// ForkWithRoute returns the fork a route belongs to, or nil.
func (l BeltLayout) ForkWithRoute(route string) *Fork {
	for i := range l.Forks {
		if forkHasRoute(l.Forks[i], route) {
			f := l.Forks[i].Clone()
			return &f
		}
	}
	return nil
}

// Route returns the route with id, or nil.
func (l BeltLayout) Route(id string) *Route {
	f := l.ForkWithRoute(id)
	if f == nil {
		return nil
	}
	return forkRoute(*f, id)
}

// Place returns the fork and route a stage is on, or nil when it is on none.
// The fork is the innermost. The optional tuple
// is a BeltLayoutWay.
func (l BeltLayout) Place(stageID string) *BeltLayoutWay {
	s := l.stageNamed(stageID)
	if s == nil || s.Route == nil {
		return nil
	}
	f := l.ForkWithRoute(*s.Route)
	if f == nil {
		return nil
	}
	r := forkRoute(*f, *s.Route)
	if r == nil {
		return nil
	}
	return &BeltLayoutWay{Fork: *f, Route: *r}
}

// Ancestry returns the forks around a stage, outermost first.
func (l BeltLayout) Ancestry(stageID string) []BeltLayoutWay {
	var chain []BeltLayoutWay
	var routeID *string
	if s := l.stageNamed(stageID); s != nil {
		routeID = s.Route
	}
	seen := map[string]struct{}{}
	for routeID != nil {
		f := l.ForkWithRoute(*routeID)
		if f == nil {
			break
		}
		if _, dup := seen[f.ID]; dup {
			break
		}
		seen[f.ID] = struct{}{}
		r := forkRoute(*f, *routeID)
		if r == nil {
			break
		}
		chain = append(chain, BeltLayoutWay{Fork: *f, Route: *r})
		routeID = f.Parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// PathOf returns the route names from the outside in, so two forks can reuse a
// name.
func (l BeltLayout) PathOf(stageID string) []string {
	var names []string
	for _, w := range l.Ancestry(stageID) {
		names = append(names, w.Route.Name)
	}
	return names
}

// StagesOn returns a route's own stages, in order, not the stages of a fork
// nested in it.
func (l BeltLayout) StagesOn(route string) []Stage {
	var out []Stage
	for _, s := range l.Stages {
		if s.Route != nil && *s.Route == route {
			out = append(out, s)
		}
	}
	return out
}

// StagesWithin returns the stages on a route and inside the forks nested in
// it, in order. A cycle is ignored, so a belt that contains itself still
// validates.
func (l BeltLayout) StagesWithin(route string) []Stage {
	return l.stagesWithin(route, map[string]struct{}{})
}

// stagesWithin is StagesWithin with the forks already seen on the way down.
func (l BeltLayout) stagesWithin(route string, seen map[string]struct{}) []Stage {
	var out []Stage
	for _, piece := range l.piecesIn(route) {
		if piece.Kind == BeltLayoutPieceStage {
			out = append(out, *piece.Stage)
			continue
		}
		if forkHasKey(seen, piece.Fork.ID) {
			continue
		}
		for _, r := range piece.Fork.Routes {
			out = append(out, l.stagesWithin(r.ID, forkSeen(seen, piece.Fork.ID))...)
		}
	}
	return out
}

// End is where a stage added at the end of route goes in the list: after the
// route's last stage, or, for a route with none, where its fork's stages begin
// (after the previous route's end for a later route).
func (l BeltLayout) End(route string) int {
	if last := l.StagesWithin(route); len(last) > 0 {
		if index := l.indexOfStage(last[len(last)-1].ID); index >= 0 {
			return index + 1
		}
	}
	fork := l.ForkWithRoute(route)
	if fork == nil {
		return len(l.Stages)
	}
	at := forkRouteIndex(*fork, route)
	if at < 0 {
		return len(l.Stages)
	}
	if at == 0 {
		if span := l.Span(*fork); span != nil {
			return span.Lower
		}
		if fork.At != nil {
			return *fork.At
		}
		return len(l.Stages)
	}
	previous := fork.Routes[at-1].ID
	if last := l.StagesWithin(previous); len(last) > 0 {
		if index := l.indexOfStage(last[len(last)-1].ID); index >= 0 {
			return index + 1
		}
	}
	return l.End(previous)
}

// Span is the positions a fork's stages, including nested forks, take in the
// list, or nil when it has none.
func (l BeltLayout) Span(fork Fork) *IntRange {
	indices := l.descendantIndices(fork)
	if len(indices) == 0 {
		return nil
	}
	lo, hi := indices[0], indices[0]
	for _, i := range indices[1:] {
		lo = min(lo, i)
		hi = max(hi, i)
	}
	return &IntRange{Lower: lo, Upper: hi}
}

// First is where a new run starts.
func (l BeltLayout) First() BeltLayoutStep {
	pieces := l.Pieces(nil)
	if len(pieces) == 0 {
		return forkStep(nil)
	}
	return l.opening(pieces[0])
}

// Next is where a run goes after stageID: the next stage on its route, or, at
// the end of a route, where the routes meet. Nil for a stage that is not on the
// belt.
func (l BeltLayout) Next(stageID string) *BeltLayoutStep {
	if l.stageNamed(stageID) == nil {
		return nil
	}
	return l.successor(stageID)
}

// ForkAfter is the fork a run enters when it leaves stageID, when the stage
// comes directly before one; nil when the next thing is a plain stage.
func (l BeltLayout) ForkAfter(stageID string) *Fork {
	var parent *string
	if s := l.stageNamed(stageID); s != nil {
		parent = s.Route
	}
	items := l.Pieces(parent)
	at := l.indexOfStagePiece(items, stageID)
	if at < 0 {
		return nil
	}
	if at+1 < len(items) {
		return items[at+1].Fork
	}
	if parent == nil {
		return nil
	}
	if f := l.ForkWithRoute(*parent); f != nil {
		return l.forkFollowing(*f, map[string]struct{}{})
	}
	return nil
}

// RoutesAfter are the routes an outcome of stageID can take: those of the fork
// right after it, but not one that feeds in.
func (l BeltLayout) RoutesAfter(stageID string) []Route {
	f := l.ForkAfter(stageID)
	if f == nil {
		return nil
	}
	var out []Route
	for _, r := range f.Routes {
		if !r.Feeds {
			out = append(out, r)
		}
	}
	return out
}

// UnavailableRoutes are the saved selections the route editor must show for
// removal, not offer as new choices.
func (l BeltLayout) UnavailableRoutes(selected []string, stageID string) []string {
	available := map[string]struct{}{}
	for _, r := range l.RoutesAfter(stageID) {
		available[r.ID] = struct{}{}
	}
	var out []string
	for _, id := range selected {
		if _, ok := available[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// forkFollowing is the fork after fork in the same route or, at a route's end,
// after the fork it is in.
func (l BeltLayout) forkFollowing(fork Fork, seen map[string]struct{}) *Fork {
	if forkHasKey(seen, fork.ID) {
		return nil
	}
	items := l.Pieces(fork.Parent)
	at := indexOfForkPiece(items, fork.ID)
	if at < 0 {
		return nil
	}
	if at+1 < len(items) {
		return items[at+1].Fork
	}
	if fork.Parent == nil {
		return nil
	}
	outer := l.ForkWithRoute(*fork.Parent)
	if outer == nil {
		return nil
	}
	return l.forkFollowing(*outer, forkSeen(seen, fork.ID))
}

// Take is where a run goes when it takes route: its first stage, or where the
// routes meet when it has none. Nil when the route is not on the belt.
func (l BeltLayout) Take(route string) *BeltLayoutStep {
	step := l.TakeRoutes([]string{route})
	if step == nil {
		return nil
	}
	f := l.ForkWithRoute(route)
	if f == nil {
		return nil
	}
	step.Routes[f.ID] = []string{route}
	return step
}

// TakeRoutes is where a run goes when it takes routes at once: the first
// route's first stage, and every named route recorded. The engine gives each
// named route its own leg.
func (l BeltLayout) TakeRoutes(routes []string) *BeltLayoutStep {
	if len(routes) == 0 {
		return nil
	}
	first := routes[0]
	f := l.ForkWithRoute(first)
	if f == nil {
		return nil
	}
	items := l.piecesIn(first)
	var step BeltLayoutStep
	if len(items) > 0 {
		step = l.opening(items[0])
	} else {
		step = l.resumeFork(*f, true)
	}
	if step.Routes == nil {
		step.Routes = map[string][]string{}
	}
	step.Routes[f.ID] = append([]string(nil), routes...)
	return &step
}

// Resume is the next thing after a fork in its own route, without leaving the
// route when the fork is the last thing in it.
func (l BeltLayout) Resume(fork Fork) BeltLayoutStep {
	return l.resumeFork(fork, false)
}

// resumeFork is Resume, leaving the route for the outer one when bubble is set.
func (l BeltLayout) resumeFork(fork Fork, bubble bool) BeltLayoutStep {
	items := l.Pieces(fork.Parent)
	at := indexOfForkPiece(items, fork.ID)
	if at < 0 {
		return forkStep(nil)
	}
	if at+1 < len(items) {
		return l.opening(items[at+1])
	}
	if !bubble || fork.Parent == nil {
		return forkStep(nil)
	}
	outer := l.ForkWithRoute(*fork.Parent)
	if outer == nil {
		return forkStep(nil)
	}
	return l.resumeFork(*outer, true)
}
