package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"maps"
)

// MetroLayoutWire is the metro layout's geometry, in points: dimensions, lines
// and branches, returns, termini, stations, stops, glyphs, name boxes, run dots
// and labels.
type MetroLayoutWire struct {
	Width    float64                  `json:"width"`
	Height   float64                  `json:"height"`
	Lines    []MetroLayoutWireLine    `json:"lines"`
	Stations []MetroLayoutWireStation `json:"stations"`
	Dots     []MetroLayoutWireDot     `json:"dots"`
	Labels   []MetroLayoutWireLabel   `json:"labels"`
}

// MetroLayoutWirePoint is a point in points.
type MetroLayoutWirePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// MetroLayoutWireBox is a rectangle in points.
type MetroLayoutWireBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// MetroLayoutWireRange is a closed range of points along one axis.
type MetroLayoutWireRange struct {
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
}

// MetroLayoutWireBranch is one branch of a line.
type MetroLayoutWireBranch struct {
	Points []MetroLayoutWirePoint `json:"points"`
	From   *string                `json:"from,omitzero"`
	To     *string                `json:"to,omitzero"`
	Stops  []string               `json:"stops"`
	End    *MetroLayoutWirePoint  `json:"end,omitzero"`
	Back   bool                   `json:"back"`
}

// MetroLayoutWireLine is one belt's line on the map.
type MetroLayoutWireLine struct {
	ID       string                   `json:"id"`
	BeltID   string                   `json:"beltId"`
	Name     string                   `json:"name"`
	Colour   int                      `json:"colour"`
	NameBox  MetroLayoutWireBox       `json:"nameBox"`
	Points   []MetroLayoutWirePoint   `json:"points"`
	Branches []MetroLayoutWireBranch  `json:"branches"`
	Returns  [][]MetroLayoutWirePoint `json:"returns"`
	Terminus MetroLayoutWirePoint     `json:"terminus"`
	// Paths holds each solid stretch (the main line, then the branches) as SVG
	// path data, corners rounded.
	Paths []string `json:"paths"`
	// ReturnPaths holds each return lane as SVG path data, corners rounded.
	ReturnPaths []string `json:"returnPaths"`
	// EndBars are the bars across the line's ends.
	EndBars []MetroLayoutWireBar `json:"endBars"`
	// Routes are the names of the routes the belt takes, each in a pill on its track before its first stop.
	Routes []MetroLayoutWireRoute `json:"routes"`
}

// MetroLayoutWireRoute is a route's name pill on a belt's line.
type MetroLayoutWireRoute struct {
	ID    string `json:"id"`
	Fork  string `json:"fork"`
	Route string `json:"route"`
	Text  string `json:"text"`
	// Box is the pill in points.
	Box MetroLayoutWireBox `json:"box"`
}

// MetroLayoutWireStatus is a station's aggregate status chip: its text and look, and its box in points.
type MetroLayoutWireStatus struct {
	Text string             `json:"text"`
	Look string             `json:"look"`
	Box  MetroLayoutWireBox `json:"box"`
}

// MetroLayoutWireBar is a vertical bar across a line's end.
type MetroLayoutWireBar struct {
	X      float64 `json:"x"`
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
}

// MetroLayoutWireCard is the card a run dot opens.
type MetroLayoutWireCard struct {
	Headline string  `json:"headline"`
	Detail   *string `json:"detail,omitzero"`
	Hint     string  `json:"hint"`
	// Left, Top and Width are the card's left edge, top and width, in points
	// beside its dot.
	Left  float64 `json:"left"`
	Top   float64 `json:"top"`
	Width float64 `json:"width"`
}

// MetroLayoutWireStation is one station of the map.
type MetroLayoutWireStation struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Lines   []string               `json:"lines"`
	Glyph   string                 `json:"glyph"`
	Kind    string                 `json:"kind"`
	X       float64                `json:"x"`
	Stops   map[string]float64     `json:"stops"`
	NameBox MetroLayoutWireBox     `json:"nameBox"`
	Pieces  []MetroLayoutWireRange `json:"pieces"`
	Top     float64                `json:"top"`
	Bottom  float64                `json:"bottom"`
	Belts   []string               `json:"belts"`
	Box     MetroLayoutWireBox     `json:"box"`
	// Status is the station's aggregate status chip, above its glyph.
	Status MetroLayoutWireStatus `json:"status"`
}

// MetroLayoutWireDot is one run dot on the map.
type MetroLayoutWireDot struct {
	ID        string               `json:"id"`
	RunIDs    []string             `json:"runIds"`
	BeltID    string               `json:"beltId"`
	StationID string               `json:"stationId"`
	Look      string               `json:"look"`
	Halo      bool                 `json:"halo"`
	Place     int                  `json:"place"`
	Overflow  int                  `json:"overflow"`
	At        MetroLayoutWirePoint `json:"at"`
	Card      MetroLayoutWireCard  `json:"card"`
}

// MetroLayoutWireLabel is one run label on the map.
type MetroLayoutWireLabel struct {
	Text     string               `json:"text"`
	Box      MetroLayoutWireBox   `json:"box"`
	NeedsYou bool                 `json:"needsYou"`
	Urgent   *string              `json:"urgent,omitzero"`
	RunIDs   []string             `json:"runIds"`
	BeltID   string               `json:"beltId"`
	Above    bool                 `json:"above"`
	Dot      MetroLayoutWirePoint `json:"dot"`
	// OpenRunID is the run a click opens, when the label names one.
	OpenRunID *string `json:"openRunId,omitzero"`
}

// NewMetroLayoutWire builds the metro layout wire from its arguments.
func NewMetroLayoutWire(layout core.MetroLayout) MetroLayoutWire {
	out := MetroLayoutWire{
		Width:    layout.Width,
		Height:   layout.Height,
		Lines:    make([]MetroLayoutWireLine, 0, len(layout.Lines)),
		Stations: make([]MetroLayoutWireStation, 0, len(layout.Stations)),
		Dots:     make([]MetroLayoutWireDot, 0, len(layout.Dots)),
		Labels:   make([]MetroLayoutWireLabel, 0, len(layout.Labels)),
	}
	for _, line := range layout.Lines {
		out.Lines = append(out.Lines, metroLayoutWireLine(line))
	}
	for _, station := range layout.Stations {
		out.Stations = append(out.Stations, metroLayoutWireStation(station))
	}
	for _, dot := range layout.Dots {
		out.Dots = append(out.Dots, metroLayoutWireDot(dot))
	}
	for _, label := range layout.Labels {
		out.Labels = append(out.Labels, metroLayoutWireLabel(label))
	}
	return out
}

// metroLayoutWireLine maps one belt's line with its returns, branches, end bars and route pills.
func metroLayoutWireLine(line core.MetroLayoutLine) MetroLayoutWireLine {
	returns := make([][]MetroLayoutWirePoint, 0, len(line.Returns))
	for _, lane := range line.Returns {
		returns = append(returns, metroLayoutWirePoints(lane))
	}
	branches := make([]MetroLayoutWireBranch, 0, len(line.Branches))
	for _, branch := range line.Branches {
		branches = append(branches, metroLayoutWireBranch(branch))
	}
	endBars := make([]MetroLayoutWireBar, 0)
	for _, bar := range line.EndBars() {
		endBars = append(endBars, MetroLayoutWireBar{X: bar.X, Top: bar.Top, Bottom: bar.Bottom})
	}
	routes := make([]MetroLayoutWireRoute, 0, len(line.Pills))
	for _, pill := range line.Pills {
		routes = append(routes, MetroLayoutWireRoute{ID: pill.ID, Fork: pill.Fork, Route: pill.Route, Text: pill.Text,
			Box: metroLayoutWireBox(pill.Box)})
	}
	return MetroLayoutWireLine{
		ID:          line.ID(),
		BeltID:      line.BeltID,
		Name:        line.Name,
		Colour:      line.Colour,
		NameBox:     metroLayoutWireBox(line.NameBox),
		Points:      metroLayoutWirePoints(line.Points),
		Branches:    branches,
		Returns:     returns,
		Terminus:    metroLayoutWirePoint(line.Terminus),
		Paths:       line.Paths(),
		ReturnPaths: line.ReturnPaths(),
		EndBars:     endBars,
		Routes:      routes,
	}
}

// metroLayoutWireBranch maps one branch of a line; a missing end stays nil.
func metroLayoutWireBranch(branch core.MetroLayoutBranch) MetroLayoutWireBranch {
	wire := MetroLayoutWireBranch{
		Points: metroLayoutWirePoints(branch.Points),
		From:   branch.From,
		To:     branch.To,
		Stops:  append([]string{}, branch.Stops...),
		Back:   branch.Back,
	}
	if branch.End != nil {
		end := metroLayoutWirePoint(*branch.End)
		wire.End = &end
	}
	return wire
}

// metroLayoutWireStation maps one station with its stops, name box, pieces and status chip.
func metroLayoutWireStation(station core.MetroLayoutStation) MetroLayoutWireStation {
	pieces := make([]MetroLayoutWireRange, 0, len(station.Pieces))
	for _, piece := range station.Pieces {
		pieces = append(pieces, MetroLayoutWireRange{Lower: piece.Lower, Upper: piece.Upper})
	}
	stops := make(map[string]float64, len(station.Stops))
	maps.Copy(stops, station.Stops)
	return MetroLayoutWireStation{
		ID:      station.ID,
		Name:    station.Name,
		Lines:   append([]string{}, station.Lines...),
		Glyph:   metroLayoutWireGlyph(station.Glyph),
		Kind:    metroLayoutWireGlyph(station.Kind),
		X:       station.X,
		Stops:   stops,
		NameBox: metroLayoutWireBox(station.NameBox),
		Pieces:  pieces,
		Top:     station.Top(),
		Bottom:  station.Bottom(),
		Belts:   station.Belts(),
		Box:     metroLayoutWireBox(station.Box()),
		Status: MetroLayoutWireStatus{Text: station.Status.Text, Look: string(station.Status.Look),
			Box: metroLayoutWireBox(station.Status.Box)},
	}
}

// metroLayoutWireDot maps one run dot with the card it opens.
func metroLayoutWireDot(dot core.MetroLayoutDot) MetroLayoutWireDot {
	return MetroLayoutWireDot{
		ID:        dot.ID,
		RunIDs:    append([]string{}, dot.RunIDs...),
		BeltID:    dot.BeltID,
		StationID: dot.StationID,
		Look:      metroLayoutWireLook(dot.Look),
		Halo:      dot.Halo,
		Place:     dot.Place,
		Overflow:  dot.Overflow,
		At:        metroLayoutWirePoint(dot.At),
		Card: MetroLayoutWireCard{
			Headline: dot.Card.Headline,
			Detail:   dot.Card.Detail,
			Hint:     dot.Card.Hint,
			Left:     dot.Card.Left,
			Top:      dot.Card.Top,
			Width:    dot.Card.Width,
		},
	}
}

// metroLayoutWireLabel maps one run label; an urgent look is set only when the label has one.
func metroLayoutWireLabel(label core.MetroLayoutLabel) MetroLayoutWireLabel {
	wire := MetroLayoutWireLabel{
		Text:      label.Text,
		Box:       metroLayoutWireBox(label.Box),
		NeedsYou:  label.NeedsYou,
		RunIDs:    append([]string{}, label.RunIDs...),
		BeltID:    label.BeltID,
		Above:     label.Above,
		Dot:       metroLayoutWirePoint(label.Dot),
		OpenRunID: label.OpenRunID,
	}
	if label.Urgent != nil {
		urgent := metroLayoutWireLook(*label.Urgent)
		wire.Urgent = &urgent
	}
	return wire
}

// metroLayoutWirePoints maps points to their wire form; a nil list is an empty array.
func metroLayoutWirePoints(points []core.MetroLayoutPoint) []MetroLayoutWirePoint {
	out := make([]MetroLayoutWirePoint, 0, len(points))
	for _, point := range points {
		out = append(out, metroLayoutWirePoint(point))
	}
	return out
}

// metroLayoutWirePoint is the metro layout wire point.
func metroLayoutWirePoint(point core.MetroLayoutPoint) MetroLayoutWirePoint {
	return MetroLayoutWirePoint{X: point.X, Y: point.Y}
}

// metroLayoutWireBox is the metro layout wire box.
func metroLayoutWireBox(box core.MetroLayoutBox) MetroLayoutWireBox {
	return MetroLayoutWireBox{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height}
}

// metroLayoutWireGlyph is the glyph's wire name.
func metroLayoutWireGlyph(glyph core.MetroLayoutGlyph) string {
	switch glyph {
	case core.MetroLayoutGlyphCircle:
		return "circle"
	case core.MetroLayoutGlyphDiamond:
		return "diamond"
	case core.MetroLayoutGlyphSquare:
		return "square"
	case core.MetroLayoutGlyphInterchange:
		return "interchange"
	default:
		return string(glyph)
	}
}

// metroLayoutWireLook is the look's wire name.
func metroLayoutWireLook(look core.MetroLayoutLook) string {
	switch look {
	case core.MetroLayoutLookRunning:
		return "running"
	case core.MetroLayoutLookWaiting:
		return "waiting"
	case core.MetroLayoutLookHeld:
		return "held"
	case core.MetroLayoutLookQueued:
		return "queued"
	case core.MetroLayoutLookParked:
		return "parked"
	default:
		return string(look)
	}
}
