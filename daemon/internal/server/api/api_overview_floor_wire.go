// The
// Floor's wire types, counts, belts, machines, boxes and drawn layout. Split
// out of api_overview_wire.go as a behaviour-free move.
package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// OverviewFloorWire is the Floor: entrances with their counts, and the look's
// drawing. The machines look sends its machines; the boxes look its boxes; each
// with the one drawn layout, in points. The metro sends neither, only its own
// drawing.
type OverviewFloorWire struct {
	Total OverviewFloorWireCounts `json:"total"`
	Belts []OverviewFloorWireBelt `json:"belts"`
	// Machines is the machines, for the machines look only.
	Machines []OverviewFloorWireMachine `json:"machines,omitzero"`
	// Boxes is the boxes, for the boxes look only.
	Boxes []OverviewFloorWireBox `json:"boxes,omitzero"`
	// Layout is the machines' or the boxes' drawing; nil for the metro.
	Layout *OverviewFloorWireLayout `json:"layout,omitzero"`
}

// NewOverviewFloorWire builds the overview floor wire from its arguments.
// Machines are sent only for the machines look and boxes only for the boxes look.
func NewOverviewFloorWire(floor core.OverviewFloor, look core.BeltLook, layout *core.FloorLayout) OverviewFloorWire {
	out := OverviewFloorWire{
		Total: NewOverviewFloorWireCounts(floor.Total),
		Belts: []OverviewFloorWireBelt{},
	}
	for _, belt := range floor.Belts {
		out.Belts = append(out.Belts, OverviewFloorWireBelt{
			ID:      belt.ID,
			Name:    belt.Name,
			Summary: belt.Summary,
			Paused:  belt.Paused,
			Counts:  NewOverviewFloorWireCounts(belt.Counts),
		})
	}
	if look == core.BeltLookMachines {
		out.Machines = []OverviewFloorWireMachine{}
		for _, machine := range floor.Machines {
			out.Machines = append(out.Machines, NewOverviewFloorWireMachine(machine))
		}
	}
	if look == core.BeltLookBoxes {
		out.Boxes = []OverviewFloorWireBox{}
		for _, box := range floor.Boxes {
			out.Boxes = append(out.Boxes, NewOverviewFloorWireBox(box))
		}
	}
	if layout != nil {
		wire := NewOverviewFloorWireLayout(*layout)
		out.Layout = &wire
	}
	return out
}

// OverviewFloorWireCounts are the counts an entrance or the total shows.
type OverviewFloorWireCounts struct {
	NeedYou      int `json:"needYou"`
	Running      int `json:"running"`
	InProgress   int `json:"inProgress"`
	ShippedToday int `json:"shippedToday"`
}

// NewOverviewFloorWireCounts builds the overview floor wire counts from its arguments.
func NewOverviewFloorWireCounts(counts core.FactorySummaryCounts) OverviewFloorWireCounts {
	return OverviewFloorWireCounts{
		NeedYou:      counts.NeedYou,
		Running:      counts.Running,
		InProgress:   counts.InProgress,
		ShippedToday: counts.ShippedToday,
	}
}

// OverviewFloorWireBelt is one entrance of the Floor.
type OverviewFloorWireBelt struct {
	ID      string                  `json:"id"`
	Name    string                  `json:"name"`
	Summary string                  `json:"summary"`
	Paused  bool                    `json:"paused"`
	Counts  OverviewFloorWireCounts `json:"counts"`
}

// OverviewFloorWireStatus is a machine status pill: its text, colour look and machine face.
type OverviewFloorWireStatus struct {
	Text string `json:"text"`
	Look string `json:"look"`
	Face string `json:"face"`
}

// OverviewFloorWireBoxStatus is a box's status: its text and colour look.
type OverviewFloorWireBoxStatus struct {
	Text string `json:"text"`
	Look string `json:"look"`
}

// OverviewFloorWireChip is a run tag on a machine or a box.
type OverviewFloorWireChip struct {
	RunID     string  `json:"runId"`
	Ref       string  `json:"ref"`
	Belt      *string `json:"belt,omitzero"`
	State     string  `json:"state"`
	Paused    bool    `json:"paused"`
	OpenStage *string `json:"openStage,omitzero"`
}

// NewOverviewFloorWireChip builds the overview floor wire chip from its arguments.
func NewOverviewFloorWireChip(chip core.OverviewFloorChip) OverviewFloorWireChip {
	return OverviewFloorWireChip{
		RunID:     chip.RunID,
		Ref:       chip.Ref,
		Belt:      chip.Belt,
		State:     string(chip.State),
		Paused:    chip.Paused,
		OpenStage: chip.OpenStage,
	}
}

// OverviewFloorWireMachine is one machine of the machines look.
type OverviewFloorWireMachine struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Number    int                     `json:"number"`
	Belts     []string                `json:"belts"`
	BeltNames []string                `json:"beltNames"`
	Mixed     bool                    `json:"mixed"`
	Workers   string                  `json:"workers"`
	Status    OverviewFloorWireStatus `json:"status"`
	Machine   string                  `json:"machine"`
	Tint      string                  `json:"tint"`
	RunCount  int                     `json:"runCount"`
	Chips     []OverviewFloorWireChip `json:"chips"`
	More      int                     `json:"more"`
	Dimmed    bool                    `json:"dimmed"`
}

// NewOverviewFloorWireMachine builds the overview floor wire machine from its arguments.
func NewOverviewFloorWireMachine(machine core.OverviewFloorMachine) OverviewFloorWireMachine {
	out := OverviewFloorWireMachine{
		ID:        machine.ID,
		Name:      machine.Name,
		Number:    machine.Number,
		Belts:     append([]string{}, machine.Belts...),
		BeltNames: append([]string{}, machine.BeltNames...),
		Mixed:     machine.Mixed,
		Workers:   machine.Workers,
		Status: OverviewFloorWireStatus{
			Text: machine.Status.Text,
			Look: string(machine.Status.Look),
			Face: string(machine.Status.Face),
		},
		Machine:  string(machine.Look.Machine),
		Tint:     string(machine.Look.Tint),
		RunCount: machine.RunCount,
		Chips:    []OverviewFloorWireChip{},
		More:     machine.More,
		Dimmed:   machine.Dimmed,
	}
	for _, chip := range machine.Chips {
		out.Chips = append(out.Chips, NewOverviewFloorWireChip(chip))
	}
	return out
}

// OverviewFloorWireBox is a merged stage as a box: its icon and tint, caption,
// status and run tags, and the cell it sits in (the box, its status above and
// its tags below).
type OverviewFloorWireBox struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Caption  string                     `json:"caption"`
	Icon     string                     `json:"icon"`
	Tint     string                     `json:"tint"`
	Belts    []string                   `json:"belts"`
	Status   OverviewFloorWireBoxStatus `json:"status"`
	RunCount int                        `json:"runCount"`
	Chips    []OverviewFloorWireChip    `json:"chips"`
	More     int                        `json:"more"`
	X        float64                    `json:"x"`
	Y        float64                    `json:"y"`
	Width    float64                    `json:"width"`
	Height   float64                    `json:"height"`
}

// NewOverviewFloorWireBox builds the overview floor wire box from its arguments.
func NewOverviewFloorWireBox(box core.OverviewFloorBox) OverviewFloorWireBox {
	out := OverviewFloorWireBox{
		ID:       box.ID,
		Name:     box.Name,
		Caption:  box.Caption,
		Icon:     string(box.Look.Icon),
		Tint:     string(box.Look.Tint),
		Belts:    append([]string{}, box.Belts...),
		Status:   OverviewFloorWireBoxStatus{Text: box.StatusText, Look: string(box.StatusLook)},
		RunCount: box.RunCount,
		Chips:    []OverviewFloorWireChip{},
		More:     box.More,
		X:        box.Frame.X,
		Y:        box.Frame.Y,
		Width:    box.Frame.Width,
		Height:   box.Frame.Height,
	}
	for _, chip := range box.Chips {
		out.Chips = append(out.Chips, NewOverviewFloorWireChip(chip))
	}
	return out
}

// OverviewFloorWirePlaced is a node placed on the floor layout.
type OverviewFloorWirePlaced struct {
	Node   FactoryNodeWire `json:"node"`
	X      float64         `json:"x"`
	Top    float64         `json:"top"`
	Height float64         `json:"height"`
	Anchor float64         `json:"anchor"`
}

// OverviewFloorWireStroke is one track of the floor layout.
type OverviewFloorWireStroke struct {
	From  FactoryNodeWire `json:"from"`
	To    FactoryNodeWire `json:"to"`
	Belts []string        `json:"belts"`
	Path  string          `json:"path"`
	Width float64         `json:"width"`
}

// OverviewFloorWireLane is one lane of the floor layout, with its arrow and label.
type OverviewFloorWireLane struct {
	From   FactoryNodeWire `json:"from"`
	To     FactoryNodeWire `json:"to"`
	Belts  []string        `json:"belts"`
	Path   string          `json:"path"`
	Arrow  string          `json:"arrow"`
	Label  string          `json:"label"`
	LabelX float64         `json:"labelX"`
	LabelY float64         `json:"labelY"`
}

// OverviewFloorWireLayout is the machines' or the boxes' drawing of the Floor.
type OverviewFloorWireLayout struct {
	Width  float64                   `json:"width"`
	Height float64                   `json:"height"`
	Column float64                   `json:"column"`
	Placed []OverviewFloorWirePlaced `json:"placed"`
	Tracks []OverviewFloorWireStroke `json:"tracks"`
	Lanes  []OverviewFloorWireLane   `json:"lanes"`
}

// NewOverviewFloorWireLayout builds the overview floor wire layout from its arguments.
func NewOverviewFloorWireLayout(layout core.FloorLayout) OverviewFloorWireLayout {
	out := OverviewFloorWireLayout{
		Width:  layout.Width,
		Height: layout.Height,
		Column: layout.Column,
		Placed: []OverviewFloorWirePlaced{},
		Tracks: []OverviewFloorWireStroke{},
		Lanes:  []OverviewFloorWireLane{},
	}
	for _, placed := range layout.Placed {
		out.Placed = append(out.Placed, OverviewFloorWirePlaced{
			Node:   NewFactoryNodeWire(placed.Node),
			X:      placed.X,
			Top:    placed.Top,
			Height: placed.Height,
			Anchor: placed.Anchor,
		})
	}
	for _, stroke := range layout.Tracks {
		out.Tracks = append(out.Tracks, OverviewFloorWireStroke{
			From:  NewFactoryNodeWire(stroke.From),
			To:    NewFactoryNodeWire(stroke.To),
			Belts: append([]string{}, stroke.Belts...),
			Path:  stroke.Path,
			Width: stroke.Width,
		})
	}
	for _, lane := range layout.Lanes {
		out.Lanes = append(out.Lanes, OverviewFloorWireLane{
			From:   NewFactoryNodeWire(lane.From),
			To:     NewFactoryNodeWire(lane.To),
			Belts:  append([]string{}, lane.Belts...),
			Path:   lane.Path,
			Arrow:  lane.Arrow,
			Label:  lane.Label,
			LabelX: lane.LabelX,
			LabelY: lane.LabelY,
		})
	}
	return out
}
