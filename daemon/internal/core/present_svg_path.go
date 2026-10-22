//
// Path data in SVG's own syntax, so the browser draws it as given.

package core

import (
	"math"
	"strconv"
)

// Syntax of SVG path data as the layouts write it.
const (
	svgMoveTo = "M "
	svgLineTo = " L "
)

// svgPoint is a point in path data.
type svgPoint struct {
	X float64
	Y float64
}

// svgNumber is a coordinate as SVG writes it: whole numbers bare, others to two places.
func svgNumber(value float64) string {
	rounded := math.Round(value*100) / 100
	if rounded == math.Trunc(rounded) {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

// svgLine is a polyline through points, with no rounding.
func svgLine(points []svgPoint) string {
	if len(points) == 0 {
		return ""
	}
	out := svgMoveTo + svgNumber(points[0].X) + " " + svgNumber(points[0].Y)
	for _, point := range points[1:] {
		out += svgSegment(point)
	}
	return out
}

// svgSegment is a straight segment to point.
func svgSegment(point svgPoint) string {
	return svgLineTo + svgNumber(point.X) + " " + svgNumber(point.Y)
}

// svgRounded is a polyline whose interior corners are rounded to radius: each corner an arc
// tangent to both stretches, shortened on both
// sides so it never runs past its neighbour. A point the line runs straight on through has no
// corner.
func svgRounded(points []svgPoint, radius float64) string {
	if len(points) <= 1 {
		return ""
	}
	first := points[0]
	out := svgMoveTo + svgNumber(first.X) + " " + svgNumber(first.Y)
	for i := 1; i < len(points); i++ {
		corner := points[i]
		if i >= len(points)-1 {
			out += svgSegment(corner)
			break
		}
		prev, next := points[i-1], points[i+1]
		cross := float64((corner.X-prev.X)*(next.Y-corner.Y)) - float64((corner.Y-prev.Y)*(next.X-corner.X))
		dot := float64((corner.X-prev.X)*(next.X-corner.X)) + float64((corner.Y-prev.Y)*(next.Y-corner.Y))
		// The angle the line turns through: an arc of radius r meets both stretches r·tan(turn/2) from the corner.
		turn := math.Atan2(math.Abs(cross), dot)
		if !(turn > 0.001 && turn < math.Pi-0.001) {
			out += svgSegment(corner)
			continue
		}
		tangent := math.Tan(turn / 2)
		// A corner shares a stretch with the next corner, but not with an end.
		firstHalf := 2.0
		if i == 1 {
			firstHalf = 1
		}
		secondHalf := 2.0
		if i == len(points)-2 {
			secondHalf = 1
		}
		reach := min(radius*tangent, svgDistance(prev, corner)/firstHalf, svgDistance(corner, next)/secondHalf)
		if !(reach > 0) {
			out += svgSegment(corner)
			continue
		}
		entry := svgTowards(corner, prev, reach)
		exit := svgTowards(corner, next, reach)
		r := reach / tangent
		out += svgSegment(entry)
		sweep := "0"
		if cross > 0 {
			sweep = "1"
		}
		out += " A " + svgNumber(r) + " " + svgNumber(r) + " 0 0 " + sweep + " " + svgNumber(exit.X) + " " + svgNumber(exit.Y)
	}
	return out
}

// svgDistance is the straight-line distance between two points.
func svgDistance(a, b svgPoint) float64 {
	return math.Sqrt(float64((a.X-b.X)*(a.X-b.X)) + float64((a.Y-b.Y)*(a.Y-b.Y)))
}

// svgTowards is the point distance along the way from corner back towards origin.
func svgTowards(corner, origin svgPoint, distance float64) svgPoint {
	length := svgDistance(origin, corner)
	if !(length > 0) {
		return corner
	}
	return svgPoint{
		X: corner.X + float64((origin.X-corner.X)*distance)/length,
		Y: corner.Y + float64((origin.Y-corner.Y)*distance)/length,
	}
}
