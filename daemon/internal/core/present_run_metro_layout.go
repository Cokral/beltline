//
// Lays one belt out as a metro line for the run page (RunMetro). The line
// runs left to right from the belt's name pill. At a fork the line splits:
// its routes spread evenly above and below it, as the machines' lanes do,
// each leaving at 45° where the fork starts and coming back at 45° where the
// routes meet. Outer routes leave first and rejoin last, so no two routes
// cross. A route that feeds in starts on its own row, open; one that does not
// move on past its fork ends open. Nested forks split the route they sit in.
// Positions depend only on the belt's shape; the run decides each stretch's
// travel and each station's state.

package core

import (
	"math"
	"slices"
	"unicode/utf8"
)

// Layout constants of the run metro.
const (
	// runMetroLayoutRowPitch is from one row to the next. A station's name reaches 36 points below its row, and
	// the next row's status chip starts 28 points above its own, so the two keep clear with room to spare.
	runMetroLayoutRowPitch = 76.0
	// runMetroLayoutStagger is the gap between neighbouring routes' 45° stretches.
	runMetroLayoutStagger = 12.0
	// runMetroLayoutMargin is the room a fork keeps at either end beyond its routes' own stretches.
	runMetroLayoutMargin = 10.0
	runMetroLayoutTop    = 40.0
	// runMetroLayoutCharWidth is the character width the run page measures labels with.
	runMetroLayoutCharWidth = 6.6
	runMetroLayoutLabelLine = 13.0
	// runMetroLayoutStatusGap is how far a status chip's foot sits above its station's centre: clear of the halo.
	runMetroLayoutStatusGap = 12.0
	// runMetroLayoutStatusHeight is a status chip's height.
	runMetroLayoutStatusHeight = 16.0
	// runMetroLayoutPillHeight is a route's name pill's height; its centre is on the route's track.
	runMetroLayoutPillHeight = 14.0
	// runMetroLayoutPillPad is the room a route's name pill takes beyond its text: its border and padding.
	runMetroLayoutPillPad = 12.0
	// runMetroLayoutPillInset is how far a route's name pill sits past the fork's split, along its track.
	runMetroLayoutPillInset = 4.0
	// runMetroLayoutPillGap is the track kept between a route's name pill and its first station.
	runMetroLayoutPillGap = 10.0
	// runMetroLayoutPillReserve is a status chip's width, reserved for every station so neighbouring chips
	// keep apart: the longest status the boxes show on one line, "✓ approved".
	runMetroLayoutPillReserve = 10*runMetroLayoutCharWidth + 16
	runMetroLayoutCorner      = 10.0
	// runMetroLayoutEndStub is how far a route that ends on its own runs past its last station: to its bar.
	runMetroLayoutEndStub = 30.0
	// runMetroLayoutBackStub is how far a route runs past its last station where it goes back, open.
	runMetroLayoutBackStub = 22.0
	// runMetroLayoutGlyphReach is how far a station's glyph reaches from its centre.
	runMetroLayoutGlyphReach = 9.0
	// runMetroLayoutHitReach is how far a station's click area reaches above its glyph (past the halo).
	runMetroLayoutHitReach = 14.0
	// runMetroLayoutHitSide is how far the click area reaches beside the station's name.
	runMetroLayoutHitSide = 5.0
	// runMetroLayoutHitFoot is how far the click area reaches below the name, to the status pill.
	runMetroLayoutHitFoot = 2.0
)

// RunMetroLayoutMap is the run's belt as a metro line. panels are the run page's stage panels, so each
// station's state, status and look match the machines map.
func RunMetroLayoutMap(run Run, panels []RunPresentationStagePanel) RunMetro {
	builder := newRunMetroLayoutBuilder(run, panels)
	return builder.build()
}

// runMetroLayoutWay is a route a stretch belongs to, by fork and route id.
type runMetroLayoutWay struct {
	Fork  string
	Route string
}

// runMetroLayoutNode is a point of a line. Owner is the routes this stretch belongs to, outermost first;
// Jump is set when the line jumps to this node without a stretch, past a fork.
type runMetroLayoutNode struct {
	At    MapPoint
	Owner []runMetroLayoutWay
	Jump  bool
}

// runMetroLayoutLine is one line of nodes, with its open ends.
type runMetroLayoutLine struct {
	Nodes     []runMetroLayoutNode
	OpenStart bool
	OpenEnd   bool
}

// runMetroLayoutPlaced is a stage placed on the line.
type runMetroLayoutPlaced struct {
	Stage Stage
	At    MapPoint
}

// runMetroLayoutRows is the number of rows a run of segments takes.
func runMetroLayoutRows(segments []BeltLayoutSegment) int {
	count := 1
	for _, segment := range segments {
		if segment.Kind != BeltLayoutSegmentFork {
			continue
		}
		total := 0
		for _, route := range segment.Routes {
			total += runMetroLayoutRows(route)
		}
		count = max(count, total)
	}
	return count
}

// runMetroLayoutWidth is the width a run of segments takes.
func runMetroLayoutWidth(segments []BeltLayoutSegment) float64 {
	total := 0.0
	for _, segment := range segments {
		switch segment.Kind {
		case BeltLayoutSegmentStage:
			total = total + runMetroLayoutStationWidth(*segment.Stage)
		case BeltLayoutSegmentFork:
			total = total + float64(2*runMetroLayoutLead(segment.Routes)) + runMetroLayoutForkWidest(*segment.Fork, segment.Routes)
		}
	}
	return total
}

// runMetroLayoutForkWidest is the width a fork's routes take beyond its lead: the room every route's name pill
// needs before its stages, then the widest route.
func runMetroLayoutForkWidest(fork Fork, routes [][]BeltLayoutSegment) float64 {
	widest := 0.0
	for i, route := range routes {
		if w := runMetroLayoutWidth(route); i == 0 || w > widest {
			widest = w
		}
	}
	return runMetroLayoutForkRoom(fork, routes) + widest
}

// runMetroLayoutForkRoom is the room a fork keeps before its routes' first stations for their name pills: the
// same for every route of the fork, so no pill reaches a neighbouring route's station. Zero when no route is
// tagged, and a route that feeds in with no stages is not.
func runMetroLayoutForkRoom(fork Fork, routes [][]BeltLayoutSegment) float64 {
	if len(fork.Routes) != len(routes) {
		return 0
	}
	widest := -1.0
	for i, route := range routes {
		if !runMetroLayoutTagged(fork.Routes[i], route) {
			continue
		}
		if w := runMetroLayoutPillWidth(fork.Routes[i].Name); w > widest {
			widest = w
		}
	}
	if widest < 0 {
		return 0
	}
	return runMetroLayoutPillInset + widest + runMetroLayoutPillGap
}

// runMetroLayoutTagged reports whether a route gets a name pill: every route but one that feeds in and has no stages.
func runMetroLayoutTagged(route Route, segments []BeltLayoutSegment) bool {
	return !route.Feeds || len(segments) != 0
}

// runMetroLayoutPillWidth is the width of a route's name pill: its text at the run page's character width, and its padding.
func runMetroLayoutPillWidth(name string) float64 {
	return runMetroLayoutTextWidth(name) + runMetroLayoutPillPad
}

// runMetroLayoutLead is the room a fork keeps at each end: far enough for its last 45° stretch to clear
// the routes.
func runMetroLayoutLead(routes [][]BeltLayoutSegment) float64 {
	offsets := runMetroLayoutOffsets(routes)
	drops := runMetroLayoutDrops(offsets)
	if len(offsets) == 0 {
		return runMetroLayoutMargin
	}
	most := 0.0
	for i := range offsets {
		v := math.Abs(offsets[i]) + drops[i]
		if i == 0 || v >= most {
			most = v
		}
	}
	return most + runMetroLayoutMargin
}

// runMetroLayoutOffsets is each route's row centre from the fork's row, in points: the routes stacked evenly
// about it.
func runMetroLayoutOffsets(routes [][]BeltLayoutSegment) []float64 {
	bands := make([]int, len(routes))
	total := 0
	for i, route := range routes {
		bands[i] = runMetroLayoutRows(route)
		total += bands[i]
	}
	top := -float64(float64(total)*runMetroLayoutRowPitch) / 2
	out := make([]float64, len(bands))
	for i, band := range bands {
		step := float64(float64(band) * runMetroLayoutRowPitch)
		out[i] = top + step/2
		top += step
	}
	return out
}

// runMetroLayoutDrops is how far along each route waits before turning off: the outer ones on each side turn
// first.
func runMetroLayoutDrops(offsets []float64) []float64 {
	out := make([]float64, len(offsets))
	for i, offset := range offsets {
		if math.Abs(offset) <= 0.5 {
			out[i] = 0
			continue
		}
		outer := 0
		for _, other := range offsets {
			if (other > 0) == (offset > 0) && math.Abs(other) > math.Abs(offset)+0.5 {
				outer++
			}
		}
		out[i] = float64(float64(outer) * runMetroLayoutStagger)
	}
	return out
}

// runMetroLayoutStationWidth is the width a station's name and status take.
func runMetroLayoutStationWidth(stage Stage) float64 {
	widest := 0.0
	for i, name := range metroLayoutWrap(stage.Name, 15) {
		if w := runMetroLayoutTextWidth(name); i == 0 || w > widest {
			widest = w
		}
	}
	return metroMax(runMetroLayoutPillReserve, widest+14)
}

// runMetroLayoutTextWidth is the width of text at the run page's character width.
func runMetroLayoutTextWidth(text string) float64 {
	return float64(float64(utf8.RuneCountInString(text)) * runMetroLayoutCharWidth)
}

// runMetroLayoutGlyph is the station glyph for a stage kind.
func runMetroLayoutGlyph(kind StageKind) RunMetroGlyph {
	switch kind {
	case StageKindGate:
		return RunMetroGlyphDiamond
	case StageKindWatch:
		return RunMetroGlyphSquare
	default:
		return RunMetroGlyphCircle
	}
}

// runMetroLayoutBuilder accumulates the lines, placed stages and route tags of one belt.
type runMetroLayoutBuilder struct {
	Run    Run
	Layout BeltLayout
	Panels map[string]RunPresentationStagePanel
	Lines  []runMetroLayoutLine
	Placed []runMetroLayoutPlaced
	Tags   []RunMetroRouteTag
}

// newRunMetroLayoutBuilder is a builder for run with its stage panels (the first panel of an id wins).
func newRunMetroLayoutBuilder(run Run, panels []RunPresentationStagePanel) runMetroLayoutBuilder {
	byID := map[string]RunPresentationStagePanel{}
	for _, panel := range panels {
		if _, ok := byID[panel.ID]; !ok {
			byID[panel.ID] = panel
		}
	}
	return runMetroLayoutBuilder{Run: run, Layout: run.Layout(), Panels: byID}
}

// currentID is the stage the run is at, unless it has finished.
func (b runMetroLayoutBuilder) currentID() *string {
	if !b.Run.State.IsActive() || b.Run.StageID == "" {
		return nil
	}
	id := b.Run.StageID
	return &id
}

// taken reports whether the run took route at fork; nil while no choice has been made there.
func (b runMetroLayoutBuilder) taken(fork, route string) *bool {
	routes, ok := b.Run.Routes()[fork]
	if !ok || len(routes) == 0 {
		return nil
	}
	return new(slices.Contains(routes, route))
}

// runMetroLayoutWaysWith is a copy of chain with way added at its end, so no two stretches share a backing array.
func runMetroLayoutWaysWith(chain []runMetroLayoutWay, way runMetroLayoutWay) []runMetroLayoutWay {
	out := make([]runMetroLayoutWay, 0, len(chain)+1)
	out = append(out, chain...)
	return append(out, way)
}

// build lays the belt out and assembles the run metro.
func (b *runMetroLayoutBuilder) build() RunMetro {
	segments := b.Layout.Segments()
	pillText := b.Run.BeltName
	pill := RunMetroBox{
		X:      runMetroLayoutMargin,
		Y:      runMetroLayoutTop - MetroLayoutPillHeight/2,
		Width:  runMetroLayoutTextWidth(pillText) + 22,
		Height: MetroLayoutPillHeight,
	}
	start := pill.X + pill.Width
	b.Lines = []runMetroLayoutLine{{}}
	b.Lines[0].Nodes = append(b.Lines[0].Nodes, runMetroLayoutNode{At: MapPoint{X: start, Y: runMetroLayoutTop}})
	end := b.place(segments, nil, runMetroLayoutTop, 0, start)
	terminus := end + 24
	b.Lines[0].Nodes = append(b.Lines[0].Nodes, runMetroLayoutNode{At: MapPoint{X: terminus, Y: runMetroLayoutTop}})
	// Routes above the line pushed things up, and status chips sit above their stations; move everything down
	// so the highest sits at the top.
	lowestNode := 0.0
	firstNode := true
	for _, line := range b.Lines {
		for _, node := range line.Nodes {
			if firstNode || node.At.Y < lowestNode {
				lowestNode = node.At.Y
				firstNode = false
			}
		}
	}
	lowestTag := 0.0
	for i, tag := range b.Tags {
		if i == 0 || tag.Y < lowestTag {
			lowestTag = tag.Y
		}
	}
	highest := metroMin(metroMin(pill.Y, lowestNode), lowestTag)
	for _, placed := range b.Placed {
		highest = metroMin(highest, placed.At.Y-runMetroLayoutStatusGap-runMetroLayoutStatusHeight)
	}
	dy := runMetroLayoutTop - MetroLayoutPillHeight/2 - highest
	shifted := pill
	shifted.Y = shifted.Y + dy
	for line := range b.Lines {
		for node := range b.Lines[line].Nodes {
			b.Lines[line].Nodes[node].At.Y = b.Lines[line].Nodes[node].At.Y + dy
		}
	}
	for index := range b.Placed {
		b.Placed[index].At.Y = b.Placed[index].At.Y + dy
	}
	for index := range b.Tags {
		b.Tags[index].Y = b.Tags[index].Y + dy
	}
	return b.assemble(RunMetroPill{Text: pillText, Box: shifted}, terminus, runMetroLayoutTop+dy)
}

// place lays a sequence of stages and forks along the row at y, on line, from x. Returns where it ends.
func (b *runMetroLayoutBuilder) place(segments []BeltLayoutSegment, chain []runMetroLayoutWay, y float64, line int, x float64) float64 {
	for _, segment := range segments {
		switch segment.Kind {
		case BeltLayoutSegmentStage:
			width := runMetroLayoutStationWidth(*segment.Stage)
			at := MapPoint{X: x + width/2, Y: y}
			b.Placed = append(b.Placed, runMetroLayoutPlaced{Stage: *segment.Stage, At: at})
			b.Lines[line].Nodes = append(b.Lines[line].Nodes, runMetroLayoutNode{At: at, Owner: chain})
			x += width
		case BeltLayoutSegmentFork:
			x = b.placeFork(*segment.Fork, segment.Routes, chain, y, line, x)
		}
	}
	return x
}

// placeFork lays a fork from xs: the line splits into its routes, spread evenly above and below y, and carries on
// past the fork where they meet again.
func (b *runMetroLayoutBuilder) placeFork(fork Fork, routes [][]BeltLayoutSegment, chain []runMetroLayoutWay, y float64, line int, xs float64) float64 {
	// A fork whose routes are missing (a cycle the model ignores) draws nothing.
	if len(routes) != len(fork.Routes) || len(routes) == 0 {
		return xs
	}
	lead := runMetroLayoutLead(routes)
	inner := xs + lead
	room := runMetroLayoutForkRoom(fork, routes)
	xe := inner + runMetroLayoutForkWidest(fork, routes) + lead
	offsets := runMetroLayoutOffsets(routes)
	drops := runMetroLayoutDrops(offsets)

	b.Lines[line].Nodes = append(b.Lines[line].Nodes, runMetroLayoutNode{At: MapPoint{X: xs, Y: y}, Owner: chain})
	for index, route := range fork.Routes {
		offset, drop := offsets[index], drops[index]
		reach := math.Abs(offset)
		routeY := y + offset
		way := runMetroLayoutWaysWith(chain, runMetroLayoutWay{Fork: fork.ID, Route: route.ID})
		branch := len(b.Lines)
		b.Lines = append(b.Lines, runMetroLayoutLine{})
		if route.Feeds {
			b.Lines[branch].OpenStart = true
			b.Lines[branch].Nodes = append(b.Lines[branch].Nodes, runMetroLayoutNode{At: MapPoint{X: inner, Y: routeY}, Owner: way})
		} else {
			b.Lines[branch].Nodes = append(b.Lines[branch].Nodes, runMetroLayoutNode{At: MapPoint{X: xs + drop, Y: y}, Owner: chain})
			if reach > 0.5 {
				b.Lines[branch].Nodes = append(b.Lines[branch].Nodes, runMetroLayoutNode{At: MapPoint{X: xs + drop + reach, Y: routeY}, Owner: way})
			}
			b.Lines[branch].Nodes = append(b.Lines[branch].Nodes, runMetroLayoutNode{At: MapPoint{X: inner, Y: routeY}, Owner: way})
		}
		// The route's stations start after its name pill, which sits on the track just past the split.
		b.place(routes[index], way, routeY, branch, inner+room)
		if b.Layout.Joins(route.ID) {
			b.Lines[branch].Nodes = append(b.Lines[branch].Nodes,
				runMetroLayoutNode{At: MapPoint{X: xe - drop - reach, Y: routeY}, Owner: way},
				runMetroLayoutNode{At: MapPoint{X: xe - drop, Y: y}, Owner: way})
		} else if n := len(b.Lines[branch].Nodes); n > 0 && !b.Lines[branch].Nodes[n-1].Jump {
			// It runs on a little past its last station, as the Factory floor's branches do: to a bar where
			// the run ends there, and open where it goes back to an earlier stage.
			last := b.Lines[branch].Nodes[n-1]
			back := metroRouteGoesBack(routes[index])
			stub := runMetroLayoutEndStub
			if back {
				stub = runMetroLayoutBackStub
			}
			b.Lines[branch].Nodes = append(b.Lines[branch].Nodes, runMetroLayoutNode{At: MapPoint{X: last.At.X + stub, Y: routeY}, Owner: way})
			b.Lines[branch].OpenEnd = !back
		}
		if runMetroLayoutTagged(route, routes[index]) {
			b.Tags = append(b.Tags, RunMetroRouteTag{ID: fork.ID + ":" + route.ID, Fork: fork.ID, Route: route.ID, Text: route.Name,
				X: inner + runMetroLayoutPillInset, Y: routeY - runMetroLayoutPillHeight/2, Width: runMetroLayoutPillWidth(route.Name),
				Height: runMetroLayoutPillHeight, Taken: b.taken(fork.ID, route.ID), Feeds: route.Feeds})
		}
	}
	b.Lines[line].Nodes = append(b.Lines[line].Nodes, runMetroLayoutNode{At: MapPoint{X: xe, Y: y}, Owner: chain, Jump: true})
	return xe
}
