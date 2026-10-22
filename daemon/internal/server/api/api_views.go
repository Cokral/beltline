package api

import (
	"fmt"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// Nested types are named by joining their names: a nested type `X.Y` is `XY` at package level.

// FactoryNodeKind is the kind of a factory track node on the wire.
type FactoryNodeKind string

// The FactoryNodeKind values, as their wire strings.
const (
	FactoryNodeKindEntrance FactoryNodeKind = "entrance"
	FactoryNodeKindMachine  FactoryNodeKind = "machine"
	FactoryNodeKindWaypoint FactoryNodeKind = "waypoint"
)

// FactoryNodeWire is a node of the factory track as the Floor's drawing names
// it: an entrance by belt id, a machine by machine id, or a waypoint by its
// index. The enum with associated values is one struct: Kind selects which
// payload field is set. On the wire it is {"kind": ..., "value": ...}.
type FactoryNodeWire struct {
	Kind FactoryNodeKind
	// ID is the belt id (entrance) or the machine id (machine).
	ID string
	// Index is the waypoint index (waypoint).
	Index int
}

// NewFactoryNodeWire builds the factory node wire from its arguments.
func NewFactoryNodeWire(node core.FactoryTrackNode) FactoryNodeWire {
	switch node.Kind {
	case core.FactoryTrackNodeMachine:
		return FactoryNodeWire{Kind: FactoryNodeKindMachine, ID: node.ID}
	case core.FactoryTrackNodeWaypoint:
		return FactoryNodeWire{Kind: FactoryNodeKindWaypoint, Index: node.Waypoint}
	default:
		return FactoryNodeWire{Kind: FactoryNodeKindEntrance, ID: node.ID}
	}
}

// MarshalJSON writes the value as JSON.
func (n FactoryNodeWire) MarshalJSON() ([]byte, error) {
	if n.Kind == FactoryNodeKindWaypoint {
		return jsonx.Marshal(struct {
			Kind  FactoryNodeKind `json:"kind"`
			Value int             `json:"value"`
		}{Kind: n.Kind, Value: n.Index})
	}
	id := n.ID
	return jsonx.Marshal(struct {
		Kind  FactoryNodeKind `json:"kind"`
		Value string          `json:"value"`
	}{Kind: n.Kind, Value: id})
}

// UnmarshalJSON reads the value from JSON; an unknown kind is an error.
func (n *FactoryNodeWire) UnmarshalJSON(data []byte) error {
	var tag struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch FactoryNodeKind(tag.Kind) {
	case FactoryNodeKindEntrance, FactoryNodeKindMachine:
		var wire struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*n = FactoryNodeWire{Kind: FactoryNodeKind(tag.Kind), ID: wire.Value}
		return nil
	case FactoryNodeKindWaypoint:
		var wire struct {
			Value int `json:"value"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*n = FactoryNodeWire{Kind: FactoryNodeKindWaypoint, Index: wire.Value}
		return nil
	default:
		return fmt.Errorf("Unknown factory node kind %s", tag.Kind) //nolint:staticcheck // ST1005: the message is part of the decode error text the API has always returned; kept as is.
	}
}

// WaitingResponse is `GET /api/v1/waiting`: the subjects intake last found waiting.
type WaitingResponse struct {
	Waiting []WaitingSubjectWire `json:"waiting"`
}

// NewWaitingResponse builds the waiting response from its arguments.
func NewWaitingResponse(waiting []core.WaitingSubject) WaitingResponse {
	out := WaitingResponse{Waiting: []WaitingSubjectWire{}}
	for _, subject := range waiting {
		out.Waiting = append(out.Waiting, NewWaitingSubjectWire(subject))
	}
	return out
}

// WaitingSubjectWire is one subject intake found waiting.
type WaitingSubjectWire struct {
	BeltID  string       `json:"beltId"`
	Subject core.Subject `json:"subject"`
	// Reference is the subject's short reference, such as "#9001" or "PAY-42".
	Reference string `json:"reference"`
	Reason    string `json:"reason"`
}

// NewWaitingSubjectWire builds the waiting subject wire from its arguments.
func NewWaitingSubjectWire(waiting core.WaitingSubject) WaitingSubjectWire {
	return WaitingSubjectWire{
		BeltID:    waiting.BeltID,
		Subject:   waiting.Subject,
		Reference: waiting.Subject.Ref(),
		Reason:    waiting.Reason,
	}
}
