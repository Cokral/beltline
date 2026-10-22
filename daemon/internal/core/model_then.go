// Then and its Action. Outcomes are in model_outcome.go.

package core

import (
	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// ThenAction is what an outcome does next.
type ThenAction string

const (
	ThenActionNext  ThenAction = "next"
	ThenActionGoto  ThenAction = "goto"
	ThenActionRoute ThenAction = "route"
	ThenActionRetry ThenAction = "retry"
	ThenActionHold  ThenAction = "hold"
	ThenActionPark  ThenAction = "park"
	ThenActionDone  ThenAction = "done"
	ThenActionShip  ThenAction = "ship"
	ThenActionDrop  ThenAction = "drop"
)

// AllThenAction lists every ThenAction in declaration order (CaseIterable).
var AllThenAction = []ThenAction{
	ThenActionNext, ThenActionGoto, ThenActionRoute, ThenActionRetry, ThenActionHold, ThenActionPark, ThenActionDone, ThenActionShip, ThenActionDrop,
}

// Known reports whether x is one of the cases of ThenAction.
func (x ThenAction) Known() bool { return enumKnown(x, AllThenAction) }

// Label is the name shown for the action.
func (a ThenAction) Label() string {
	switch a {
	case ThenActionNext:
		return "Next stage"
	case ThenActionGoto:
		return "Go to a stage"
	case ThenActionRoute:
		return "Take a route"
	case ThenActionRetry:
		return "Run again"
	case ThenActionHold:
		return "Hold for me"
	case ThenActionPark:
		return "Park"
	case ThenActionDone:
		return "Done"
	case ThenActionShip:
		return "Ship"
	case ThenActionDrop:
		return "Drop"
	}
	return ""
}

// MovesOn reports whether the run moves on rather than stopping. Done and Ship end
// the run's route, so they move on as Next does past the end of the belt.
func (a ThenAction) MovesOn() bool {
	return a == ThenActionNext || a == ThenActionGoto || a == ThenActionRoute || a == ThenActionDone || a == ThenActionShip
}

// Finishes reports whether the action ends the leg it is taken on as a successful
// completion: Done, or Ship, which also records that the work shipped.
func (a ThenAction) Finishes() bool {
	return a == ThenActionDone || a == ThenActionShip
}

// Then is what an outcome does next: the action and its targets.
//
// The keys are do (the Action), to, route, withOutput, max and comment. To and
// Routes are each written as a scalar when they hold one element and as an
// array otherwise; see MarshalJSON.
type Then struct {
	// Action is the key "do". A request body must give it (required:"true").
	Action ThenAction `json:"do" required:"true"`
	// To are the stages a goto jumps to: one, or one per route when it goes back
	// into a fork, and the run goes to the one on the route it took.
	To []string `json:"to"`
	// Routes are the explicitly named routes a route takes. Adding a route to a
	// fork never expands this selection. Encoded under the key "route".
	Routes []string `json:"route"`
	// WithOutput passes this stage's output to the next stage as a note.
	WithOutput bool `json:"withOutput"`
	// Max, after this many uses on one run, holds instead.
	Max *int `json:"max,omitzero"`
	// Comment posts the output's questions on the issue when holding.
	Comment bool `json:"comment"`
}

// Route is the single-route compatibility accessor: the first of Routes, or nil.
func (t Then) Route() *string {
	if len(t.Routes) == 0 {
		return nil
	}
	first := t.Routes[0]
	return &first
}

// SetRoute replaces the selection with one route, or none when nil.
func (t *Then) SetRoute(route *string) {
	if route == nil {
		t.Routes = []string{}
		return
	}
	t.Routes = []string{*route}
}

// ThenArgs are the parameters of NewThen. It covers the three ways to make a Then:
// To holds the one stage of `to: String?` or the list of
// `to: [String]`, and Routes holds `routes:`, which Route fills with one route
// when given.
type ThenArgs struct {
	Action     ThenAction
	To         []string
	Route      *string
	Routes     []string
	WithOutput bool
	Max        *int
	Comment    bool
}

// NewThen creates a Then. A Route given is the one route the selection holds;
// Routes, when set, replaces it.
func NewThen(a ThenArgs) Then {
	t := Then{
		Action:     a.Action,
		To:         append([]string{}, a.To...),
		Routes:     []string{},
		WithOutput: a.WithOutput,
		Max:        a.Max,
		Comment:    a.Comment,
	}
	if a.Route != nil {
		t.Routes = []string{*a.Route}
	}
	if a.Routes != nil {
		t.Routes = append([]string{}, a.Routes...)
	}
	return t
}

// UnmarshalJSON reads a Then. To and route each hold one string or a list of
// them, so single-route files stay readable; absent or null is an empty list.
func (t *Then) UnmarshalJSON(data []byte) error {
	var w struct {
		Action     ThenAction      `json:"do"`
		To         json.RawMessage `json:"to"`
		Route      json.RawMessage `json:"route"`
		WithOutput bool            `json:"withOutput"`
		Max        *int            `json:"max"`
		Comment    bool            `json:"comment"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*t = Then{Action: w.Action, WithOutput: w.WithOutput, Max: w.Max, Comment: w.Comment}
	var err error
	if t.To, err = thenOneOrList(w.To); err != nil {
		return err
	}
	t.Routes, err = thenOneOrList(w.Route)
	return err
}

// thenOneOrList reads one string or a list of them. Absent or null is an empty list.
func thenOneOrList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || codecIsNull(raw) {
		return []string{}, nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return append([]string{}, list...), nil
}

// MarshalJSON writes a Then with to and route as a scalar for one element, a
// list for several, and nothing when empty.
func (t Then) MarshalJSON() ([]byte, error) {
	w := thenWire{Action: t.Action, To: thenScalarOrList(t.To), Route: thenScalarOrList(t.Routes), Max: t.Max}
	if t.WithOutput {
		yes := true
		w.WithOutput = &yes
	}
	if t.Comment {
		yes := true
		w.Comment = &yes
	}
	return jsonx.Marshal(w)
}

// thenWire is the encoded form of a Then. To and Route hold a string for one
// element, a list for several, and nothing when empty; withOutput and comment
// are written only when true.
type thenWire struct {
	Action     ThenAction `json:"do"`
	To         any        `json:"to,omitzero"`
	Route      any        `json:"route,omitzero"`
	WithOutput *bool      `json:"withOutput,omitzero"`
	Max        *int       `json:"max,omitzero"`
	Comment    *bool      `json:"comment,omitzero"`
}

// thenScalarOrList is the one element of list as a string, the list itself
// when it has several, and nil when it is empty.
func thenScalarOrList(list []string) any {
	switch len(list) {
	case 0:
		return nil
	case 1:
		return list[0]
	}
	return list
}

// Clone is a deep copy of the then: its goto targets and routes, and its max.
func (t Then) Clone() Then {
	return cloneValue(t)
}
