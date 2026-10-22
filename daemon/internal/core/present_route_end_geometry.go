// The run page's map with the route-end drawing rules the app drew it with: a fork whose routes feed into the
// line is as tall as its rows need, its rows sit around the line, and a route that never joins the line again
// stops short of the fork's end instead of bending back. These are extensions of MapPlan.

package core

import "slices"

// RouteEndHeight is the height of the map with route ends drawn.
func (p MapPlan) RouteEndHeight() float64 {
	height := p.Height()
	var onLine []float64
	for _, segment := range p.Segments {
		if segment.Kind != BeltLayoutSegmentFork || !mapPlanIsOnLine(*segment.Fork, segment.Routes) {
			continue
		}
		onLine = append(onLine, float64(2*p.Rows(len(segment.Routes)))-p.Metrics.Cell)
	}
	if len(onLine) == 0 {
		return height
	}
	return max(height, mapPlanMax(onLine))
}

// mapPlanMax is the largest of values, which must not be empty.
func mapPlanMax(values []float64) float64 {
	largest := values[0]
	for _, value := range values[1:] {
		if value > largest {
			largest = value
		}
	}
	return largest
}

// RouteEndGeometry is the geometry of the map at width with route ends drawn.
func (p MapPlan) RouteEndGeometry(width float64) MapGeometry {
	geometry := p.Geometry(width)
	routeEndHeight := p.RouteEndHeight()
	lineShift := (routeEndHeight - geometry.Height) / 2
	rowShifts := p.routeEndRowShifts(geometry.Height, routeEndHeight)

	geometry.Height = routeEndHeight
	geometry.MainY += lineShift
	p.shiftRouteEndCells(geometry.Cells, rowShifts, lineShift)
	for index := range geometry.Lanes {
		lane := &geometry.Lanes[index]
		if value, ok := rowShifts[lane.Route.ID]; ok {
			lane.Y += value
		} else {
			lane.Y += lineShift
		}

		span := mapGeometrySpanNamed(geometry.Forks, lane.Fork)
		if span == nil {
			continue
		}
		lane.Path = p.routeEndLanePath(geometry, *lane, *span)
	}
	geometry.Line = p.routeEndLine(geometry, lineShift)
	return geometry
}

// routeEndRowShifts is how far each route's row moves when its fork is drawn with route ends: a fork that feeds
// the line is centred again at the new height.
func (p MapPlan) routeEndRowShifts(height, routeEndHeight float64) map[string]float64 {
	rowShifts := map[string]float64{}
	for _, segment := range p.Segments {
		if segment.Kind != BeltLayoutSegmentFork || !mapPlanIsOnLine(*segment.Fork, segment.Routes) {
			continue
		}
		oldTop := (height - p.Rows(len(segment.Routes))) / 2
		newTop := (routeEndHeight - p.Metrics.Cell) / 2
		for _, route := range segment.Fork.Routes {
			rowShifts[route.ID] = newTop - oldTop
		}
	}
	return rowShifts
}

// shiftRouteEndCells moves each stage's cell with its route's row, or with the line when no route above it moved.
func (p MapPlan) shiftRouteEndCells(cells []MapGeometryPlaced, rowShifts map[string]float64, lineShift float64) {
	for index := range cells {
		placed := &cells[index]
		shift := lineShift
		for _, way := range p.Layout.Ancestry(placed.Stage.ID) {
			if value, ok := rowShifts[way.Route.ID]; ok {
				shift = value
				break
			}
		}
		placed.Top += shift
	}
}

// mapGeometrySpanNamed is the span of the fork with the given id, or nil when the map has none.
func mapGeometrySpanNamed(spans []MapGeometrySpan, fork string) *MapGeometrySpan {
	for index := range spans {
		if spans[index].ID == fork {
			return &spans[index]
		}
	}
	return nil
}

// routeEndLanePath is the path of one route's lane across its fork's span: it leaves the line where the route
// opens and rejoins it where it joins.
func (p MapPlan) routeEndLanePath(geometry MapGeometry, lane MapGeometryLane, span MapGeometrySpan) MapPath {
	stop := p.routeEndStop(geometry, lane.Route.ID, span)
	opens := p.Layout.Opens(lane.Route.ID)
	mapLane := NewMapLane(MapLaneArgs{Start: span.Start, End: span.End, MainY: geometry.MainY, Y: lane.Y,
		Thickness: p.Metrics.Thickness, Opens: &opens, Stop: stop})
	points := make([]MapPoint, 0, len(mapLane.Points)+2)
	for _, point := range mapLane.Points {
		points = append(points, MapPoint(point))
	}
	if lane.Y != geometry.MainY {
		if mapLane.Opens {
			points = append([]MapPoint{{X: span.Start - p.Metrics.Corner, Y: geometry.MainY}}, points...)
		}
		if mapLane.Joins {
			points = append(points, MapPoint{X: span.End + p.Metrics.Corner, Y: geometry.MainY})
		}
	}
	return NewMapPathPoints(points, p.Metrics.Corner)
}

// routeEndStop is where a route that never joins the line again stops, short of its fork's end: just before the
// last stage that runs down its branch, and never before the fork's inside edge. Nil when the route joins the line.
func (p MapPlan) routeEndStop(geometry MapGeometry, route string, span MapGeometrySpan) *float64 {
	if p.Layout.Joins(route) {
		return nil
	}
	inside := span.Start + p.Metrics.Pad
	last := inside
	found := false
	for _, placed := range geometry.Cells {
		if !draftAncestryHasRoute(p.Layout.Ancestry(placed.Stage.ID), route) {
			continue
		}
		edge := placed.X + geometry.Column/2
		if !found || edge > last {
			last = edge
			found = true
		}
	}
	value := max(last-float64(geometry.Column*0.15), inside)
	return &value
}

// routeEndLine is the line moved to the route-end height, without its last stretch when the fork at its end never
// joins it.
func (p MapPlan) routeEndLine(geometry MapGeometry, lineShift float64) MapPath {
	line := make([][]MapPoint, 0, len(geometry.Line.Subpaths))
	for _, path := range geometry.Line.Subpaths {
		shifted := make([]MapPoint, 0, len(path))
		for _, point := range path {
			shifted = append(shifted, MapPoint{X: point.X, Y: point.Y + lineShift})
		}
		line = append(line, shifted)
	}
	if n := len(p.Segments); n > 0 && p.Segments[n-1].Kind == BeltLayoutSegmentFork {
		fork := p.Segments[n-1].Fork
		joins := slices.ContainsFunc(fork.Routes, func(route Route) bool { return p.Layout.Joins(route.ID) })
		if !joins && len(line) > 0 {
			line = line[:len(line)-1]
		}
	}
	return MapPath{Subpaths: line, CornerRadius: geometry.Line.CornerRadius}
}

// IsLine reports whether a lane is the line itself: a feeding fork's first, empty route,
// which draws no route tag.
func (p MapPlan) IsLine(lane MapGeometryLane, geometry MapGeometry) bool {
	if lane.Y != geometry.MainY {
		return false
	}
	fork := p.Layout.ForkWithRoute(lane.Route.ID)
	if fork == nil || len(fork.Routes) == 0 || fork.Routes[0].ID != lane.Route.ID {
		return false
	}
	return slices.ContainsFunc(fork.Routes, func(route Route) bool { return route.Feeds })
}

// mapPlanIsOnLine reports whether a fork's routes feed into the line.
func mapPlanIsOnLine(fork Fork, routes [][]BeltLayoutSegment) bool {
	feeds := slices.ContainsFunc(fork.Routes, func(route Route) bool { return route.Feeds })
	if !feeds || len(routes) == 0 || len(routes[0]) != 0 {
		return false
	}
	for _, route := range routes {
		for _, segment := range route {
			if segment.Kind != BeltLayoutSegmentStage {
				return false
			}
		}
	}
	return true
}
