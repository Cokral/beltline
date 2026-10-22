//
// The belt editor's changes to forks and routes. Each leaves every fork's
// stages together, route after route, in the order of its routes, with a
// nested fork kept inside the route it belongs to. All are pointer-receiver
// methods. Overloads become distinct Go names:
// `move(_:to:)` is MoveTo and `move(_:onto:)` is MoveOnto, `flatten` with and
// without `seen` is flatten and flattenSeen.
//
// The methods change the belt's slices in place, so a caller that must keep the
// belt it started from (the draft requests) clones it first.

package core

import (
	"slices"
	"strconv"
)

// ForkAround puts a stage on the first route of a new fork, with an empty
// second route beside it that skips it. On a stage that is already on a route,
// the new fork sits in that route.
func (b *Belt) ForkAround(stageID string) {
	index := b.stageIndex(stageID)
	if index < 0 {
		return
	}
	parent := CopyPointer(b.Stages[index].Route)
	first := b.freshRouteID(nil)
	second := b.freshRouteID(map[string]struct{}{first: {}})
	at := index
	b.Forks = append(b.Forks, Fork{
		ID:     freshID("fork", forkIDSet(b.Forks)),
		Routes: []Route{{ID: first, Name: "Route 1"}, {ID: second, Name: "Route 2"}},
		Parent: parent,
		At:     &at,
	})
	b.Stages[index].Route = &first
	b.Tidy()
}

// FeedInto puts a fork in front of a stage whose first route is empty, the
// line itself, and whose second route feeds into the stage, holding stage. On a
// stage that is on a route, the new fork sits in that route.
func (b *Belt) FeedInto(stageID string, stage Stage) {
	index := b.stageIndex(stageID)
	if index < 0 {
		return
	}
	line := b.freshRouteID(nil)
	feed := b.freshRouteID(map[string]struct{}{line: {}})
	at := index
	b.Forks = append(b.Forks, Fork{
		ID:     freshID("fork", forkIDSet(b.Forks)),
		Routes: []Route{{ID: line, Name: "Line"}, {ID: feed, Name: "Feed in", Feeds: true}},
		Parent: CopyPointer(b.Stages[index].Route),
		At:     &at,
	})
	stage.Route = &feed
	b.Stages = forkInsertStage(b.Stages, index, stage)
	b.Tidy()
}

// AddRoute adds an empty route to a fork, one that feeds in when feeds is set,
// and returns its id, or nil when the fork is not on the belt.
func (b *Belt) AddRoute(forkID string, feeds bool) *string {
	index := -1
	for i := range b.Forks {
		if b.Forks[i].ID == forkID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	id := b.freshRouteID(nil)
	count := 0
	for _, r := range b.Forks[index].Routes {
		if r.Feeds == feeds {
			count++
		}
	}
	name := "Route " + strconv.Itoa(len(b.Forks[index].Routes)+1)
	if feeds {
		name = "Feed in " + strconv.Itoa(count+1)
	}
	b.Forks[index].Routes = append(slices.Clone(b.Forks[index].Routes), Route{ID: id, Name: name, Feeds: feeds})
	return &id
}

// RemoveRoute removes a route and everything nested in it. A fork left with one
// route is no fork: its contents join the route that held it, or the line.
func (b *Belt) RemoveRoute(routeID string) {
	index := -1
	for i := range b.Forks {
		if forkHasRoute(b.Forks[i], routeID) {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	gone := b.subtree(routeID)
	forkID := b.Forks[index].ID
	var stages []Stage
	for _, s := range b.Stages {
		if s.Route == nil || !forkHasKey(gone, *s.Route) {
			stages = append(stages, s.Clone())
		}
	}
	b.Stages = stages
	var forks []Fork
	for _, f := range b.Forks {
		if f.Parent == nil || !forkHasKey(gone, *f.Parent) {
			forks = append(forks, f.Clone())
		}
	}
	b.Forks = forks
	index = -1
	for i := range b.Forks {
		if b.Forks[i].ID == forkID {
			index = i
			break
		}
	}
	if index < 0 {
		b.forget(gone)
		b.Tidy()
		return
	}
	var routes []Route
	for _, r := range b.Forks[index].Routes {
		if r.ID != routeID {
			routes = append(routes, r)
		}
	}
	b.Forks[index].Routes = routes
	b.forget(gone)
	if len(routes) < 2 {
		b.Dissolve(b.Forks[index].ID)
	} else {
		b.Tidy()
	}
}

// Dissolve removes a fork but keeps what was on its routes, in route order, in
// the route that held the fork. A fork nested inside one of those routes stays.
func (b *Belt) Dissolve(forkID string) {
	fork := b.forkNamed(forkID)
	if fork == nil {
		return
	}
	ids := map[string]struct{}{}
	for _, r := range fork.Routes {
		ids[r.ID] = struct{}{}
	}
	for i := range b.Stages {
		if b.Stages[i].Route != nil && forkHasKey(ids, *b.Stages[i].Route) {
			b.Stages[i].Route = CopyPointer(fork.Parent)
		}
	}
	for i := range b.Forks {
		if b.Forks[i].Parent != nil && forkHasKey(ids, *b.Forks[i].Parent) {
			b.Forks[i].Parent = CopyPointer(fork.Parent)
		}
	}
	var forks []Fork
	for _, f := range b.Forks {
		if f.ID != forkID {
			forks = append(forks, f.Clone())
		}
	}
	b.Forks = forks
	b.forget(ids)
	b.Tidy()
}

// Insert adds a stage at index in the list (clamped); its Route says which
// route, if any.
func (b *Belt) Insert(stage Stage, index int) {
	b.Stages = forkInsertStage(b.Stages, min(max(index, 0), len(b.Stages)), stage)
	b.Tidy()
}

// Tidy puts each fork's stages together, route after route, where its first
// one is; takes stages off routes that are gone; and removes forks with no
// stages left anywhere under them.
func (b *Belt) Tidy() {
	routeIDs := forkRouteIDSet(b.Forks)
	for i := range b.Stages {
		if r := b.Stages[i].Route; r != nil && !forkHasKey(routeIDs, *r) {
			b.Stages[i].Route = nil
		}
	}
	for {
		layout := b.Layout()
		empty := map[string]struct{}{}
		emptyRoutes := map[string]struct{}{}
		for _, f := range b.Forks {
			if layout.Span(f) == nil {
				empty[f.ID] = struct{}{}
				for _, r := range f.Routes {
					emptyRoutes[r.ID] = struct{}{}
				}
			}
		}
		if len(empty) == 0 {
			break
		}
		var forks []Fork
		for _, f := range b.Forks {
			if !forkHasKey(empty, f.ID) {
				forks = append(forks, f.Clone())
			}
		}
		b.Forks = forks
		b.forget(emptyRoutes)
	}
	b.Stages = b.flatten(nil, nil)
	layout := b.Layout()
	for i := range b.Forks {
		b.Forks[i].At = nil
		if span := layout.Span(b.Forks[i]); span != nil {
			lo := span.Lower
			b.Forks[i].At = &lo
		}
	}
}
