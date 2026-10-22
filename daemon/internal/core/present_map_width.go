package core

// MapWidthFloorMinimumColumn is the shared minimum column used by the Floor and run map.
const MapWidthFloorMinimumColumn = 172.0

// MapWidth is width arithmetic shared by the run map and the Floor's belt lanes.
// Forks reserve corner room on both sides, outside the equally sized columns.
type MapWidth struct {
	Columns       int
	Forks         int
	MinimumColumn float64
	Pad           float64
}

// MinimumWidth is the narrowest the map may be drawn.
func (w MapWidth) MinimumWidth() float64 {
	return float64(float64(w.Columns)*w.MinimumColumn) + float64(float64(w.Forks)*2*w.Pad)
}

// ContentWidth is the width the content takes in a viewport.
func (w MapWidth) ContentWidth(viewport float64) float64 {
	return max(viewport, w.MinimumWidth())
}

// Column is the width of one column in a viewport.
func (w MapWidth) Column(viewport float64) float64 {
	return max(w.MinimumColumn, (w.ContentWidth(viewport)-float64(float64(w.Forks)*2*w.Pad))/float64(max(w.Columns, 1)))
}
