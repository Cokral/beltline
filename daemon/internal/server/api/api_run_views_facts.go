// Part 2: a stage panel's
// facts and the inline spans its text carries.
package api

import (
	"errors"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// RunStageFactsWire is the facts a stage panel shows beside its status: head
// chips, attempt lines, watch cards, an approval's document and the adjustments.
type RunStageFactsWire struct {
	KindChip  string                          `json:"kindChip"`
	Working   string                          `json:"working"`
	State     RunStageFactsWireChip           `json:"state"`
	CanAdjust bool                            `json:"canAdjust"`
	Attempts  []RunStageFactsWireAttemptFacts `json:"attempts"`
	// Deciding is the AFK decision this stage is deciding now.
	Deciding     *RunStageFactsWireAfkNote     `json:"deciding,omitzero"`
	WatchCards   []RunStageFactsWireWatchCard  `json:"watchCards"`
	WatchLooking *string                       `json:"watchLooking,omitzero"`
	Approval     *RunStageFactsWireApproval    `json:"approval,omitzero"`
	Adjustments  []RunStageFactsWireAdjustment `json:"adjustments"`
	Send         RunStageFactsWireSend         `json:"send"`
}

// NewRunStageFactsWire builds the run stage facts wire from its arguments.
func NewRunStageFactsWire(facts coordinator.StagePanelFacts) RunStageFactsWire {
	out := RunStageFactsWire{
		KindChip:  facts.KindChip,
		Working:   facts.Working,
		State:     RunStageFactsWireChip{Text: facts.State.Text, Tone: facts.State.Tone},
		CanAdjust: facts.CanAdjust,
		Attempts:  []RunStageFactsWireAttemptFacts{},
		// WatchCards are the watch cards of a watch stage.
		WatchCards:   []RunStageFactsWireWatchCard{},
		WatchLooking: facts.WatchLooking,
		Adjustments:  []RunStageFactsWireAdjustment{},
		Send:         RunStageFactsWireSend{Earlier: facts.Send.Earlier, Note: facts.Send.Note, Again: facts.Send.Again},
	}
	if facts.State.ClockSince != nil {
		since := jsonx.Date{Time: *facts.State.ClockSince}
		out.State.ClockSince = &since
	}
	for _, attempt := range facts.Attempts {
		entry := RunStageFactsWireAttemptFacts{
			ID:           attempt.ID,
			RanOn:        attempt.RanOn,
			File:         attempt.File,
			Duration:     attempt.Duration,
			Decision:     attempt.Decision,
			DecidedBy:    attempt.DecidedBy,
			DecisionLine: attempt.DecisionLine,
		}
		if attempt.DecidedAt != nil {
			at := jsonx.Date{Time: *attempt.DecidedAt}
			entry.DecidedAt = &at
		}
		if attempt.Result != nil {
			result := NewRunStageFactsWireAttemptResult(*attempt.Result)
			entry.Result = &result
		}
		if attempt.Afk != nil {
			afk := NewRunStageFactsWireAfkNote(*attempt.Afk)
			entry.Afk = &afk
		}
		out.Attempts = append(out.Attempts, entry)
	}
	if facts.Deciding != nil {
		deciding := NewRunStageFactsWireAfkNote(*facts.Deciding)
		out.Deciding = &deciding
	}
	for _, card := range facts.WatchCards {
		out.WatchCards = append(out.WatchCards, NewRunStageFactsWireWatchCard(card))
	}
	if facts.Approval != nil {
		approval := NewRunStageFactsWireApproval(*facts.Approval)
		out.Approval = &approval
	}
	for _, adjustment := range facts.Adjustments {
		out.Adjustments = append(out.Adjustments, NewRunStageFactsWireAdjustment(adjustment))
	}
	return out
}

// RunStageFactsWireChip is a head chip: its text, tone and, when it counts up,
// the moment it counts from.
type RunStageFactsWireChip struct {
	Text       string      `json:"text"`
	Tone       string      `json:"tone"`
	ClockSince *jsonx.Date `json:"clockSince,omitzero"`
}

// RunStageFactsWireAfkNote is the AFK note on a stage panel.
type RunStageFactsWireAfkNote struct {
	Title        string  `json:"title"`
	OverruleLine *string `json:"overruleLine,omitzero"`
	Comments     *string `json:"comments,omitzero"`
	Why          *string `json:"why,omitzero"`
	Deciding     bool    `json:"deciding"`
}

// NewRunStageFactsWireAfkNote builds the run stage facts wire afk note from its arguments.
func NewRunStageFactsWireAfkNote(note coordinator.StagePanelFactsAfkNote) RunStageFactsWireAfkNote {
	return RunStageFactsWireAfkNote{
		Title:        note.Title,
		OverruleLine: note.OverruleLine,
		Comments:     note.Comments,
		Why:          note.Why,
		Deciding:     note.Deciding,
	}
}

// RunStageFactsWireAttemptResult is the result line of an attempt.
type RunStageFactsWireAttemptResult struct {
	Outcome *string `json:"outcome,omitzero"`
	Tone    *string `json:"tone,omitzero"`
	Route   *string `json:"route,omitzero"`
	Note    *string `json:"note,omitzero"`
	Detail  *string `json:"detail,omitzero"`
	Problem bool    `json:"problem"`
}

// NewRunStageFactsWireAttemptResult builds the run stage facts wire attempt result from its arguments.
func NewRunStageFactsWireAttemptResult(result coordinator.StagePanelFactsAttemptResult) RunStageFactsWireAttemptResult {
	return RunStageFactsWireAttemptResult{
		Outcome: result.Outcome,
		Tone:    result.Tone,
		Route:   result.Route,
		Note:    result.Note,
		Detail:  result.Detail,
		Problem: result.Problem,
	}
}

// RunStageFactsWireAttemptFacts is one attempt's facts line.
type RunStageFactsWireAttemptFacts struct {
	ID           string                          `json:"id"`
	RanOn        string                          `json:"ranOn"`
	File         *string                         `json:"file,omitzero"`
	Duration     *string                         `json:"duration,omitzero"`
	Decision     *string                         `json:"decision,omitzero"`
	DecidedBy    *string                         `json:"decidedBy,omitzero"`
	DecidedAt    *jsonx.Date                     `json:"decidedAt,omitzero"`
	DecisionLine *string                         `json:"decisionLine,omitzero"`
	Result       *RunStageFactsWireAttemptResult `json:"result,omitzero"`
	Afk          *RunStageFactsWireAfkNote       `json:"afk,omitzero"`
}

// RunStageFactsWireWatchCard is one watch card of a watch stage.
type RunStageFactsWireWatchCard struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Subtitle []InlineSpanWire `json:"subtitle"`
	State    string           `json:"state"`
	URL      string           `json:"url"`
}

// NewRunStageFactsWireWatchCard builds the watch card from its id, title, subtitle, state and url.
func NewRunStageFactsWireWatchCard(card coordinator.StagePanelFactsWatchCard) RunStageFactsWireWatchCard {
	out := RunStageFactsWireWatchCard{
		ID:       card.ID,
		Title:    card.Title,
		Subtitle: []InlineSpanWire{},
		State:    card.State,
		URL:      card.URL,
	}
	for _, span := range card.Subtitle {
		out.Subtitle = append(out.Subtitle, NewInlineSpanWire(span))
	}
	return out
}

// RunStageFactsWireApproval is an approval stage's document and what it does.
type RunStageFactsWireApproval struct {
	// Blocks is the parsed approval document; omitted when there is none.
	Blocks        []MarkdownBlockWire `json:"blocks,omitzero"`
	EmptyDocument string              `json:"emptyDocument"`
	WillHeading   *string             `json:"willHeading,omitzero"`
	Will          [][]InlineSpanWire  `json:"will"`
	Primary       *string             `json:"primary,omitzero"`
	OutcomesHelp  string              `json:"outcomesHelp"`
	// Outcomes are the outcome buttons in the stage's order.
	Outcomes []RunStageFactsWireOutcome `json:"outcomes"`
}

// RunStageFactsWireOutcome is one outcome button of an approval: the outcome, the words it shows and its tone.
type RunStageFactsWireOutcome struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

// NewRunStageFactsWireApproval builds the approval fact from its arguments:
// the document is parsed with the Markdown blocks after hoisting and stripping outcome lines.
func NewRunStageFactsWireApproval(approval coordinator.StagePanelFactsApproval) RunStageFactsWireApproval {
	out := RunStageFactsWireApproval{
		EmptyDocument: approval.EmptyDocument,
		WillHeading:   approval.WillHeading,
		Will:          [][]InlineSpanWire{},
		Primary:       approval.Primary,
		OutcomesHelp:  approval.OutcomesHelp,
		Outcomes:      []RunStageFactsWireOutcome{},
	}
	for _, outcome := range approval.Outcomes {
		out.Outcomes = append(out.Outcomes, RunStageFactsWireOutcome{ID: outcome.ID, Name: outcome.Name, Label: outcome.Label, Tone: string(outcome.Tone)})
	}
	if approval.Document != nil {
		// The document is parsed as the output route parses it: stripped, then hoisted.
		text := core.DocumentHoisting(core.OutcomeParserStrip(*approval.Document), core.DocumentHoistingDefaultSection)
		out.Blocks = []MarkdownBlockWire{}
		for _, block := range core.MarkdownBlockParse(text) {
			out.Blocks = append(out.Blocks, NewMarkdownBlockWire(block))
		}
	}
	for _, line := range approval.Will {
		spans := []InlineSpanWire{}
		for _, span := range line {
			spans = append(spans, NewInlineSpanWire(span))
		}
		out.Will = append(out.Will, spans)
	}
	return out
}

// RunStageFactsWireAdjustment is one dated adjustment line.
type RunStageFactsWireAdjustment struct {
	Date jsonx.Date `json:"date"`
	Text string     `json:"text"`
}

// NewRunStageFactsWireAdjustment from `StagePanelFacts.adjustments`.
func NewRunStageFactsWireAdjustment(adjustment coordinator.StagePanelFactsAdjustment) RunStageFactsWireAdjustment {
	return RunStageFactsWireAdjustment{Date: jsonx.Date{Time: adjustment.Date}, Text: adjustment.Text}
}

// RunStageFactsWireSend is the Send copy of a stage panel: the earlier-attempt
// and note wording, and the again line. Earlier and Note are omitted when nil.
type RunStageFactsWireSend struct {
	Earlier *string `json:"earlier,omitzero"`
	Note    *string `json:"note,omitzero"`
	Again   string  `json:"again"`
}

// InlineSpanWireKind is the type tag of an inline span on the wire.
type InlineSpanWireKind string

// The InlineSpanWireKind values, as their wire strings.
const (
	InlineSpanWireKindText   InlineSpanWireKind = "text"
	InlineSpanWireKindStrong InlineSpanWireKind = "strong"
	InlineSpanWireKindCode   InlineSpanWireKind = "code"
)

// InlineSpanWire is one parsed inline span: plain text, **strong** or `code`.
// The enum with associated values is one struct; Kind selects which
// Text is its payload. On the wire it is {"type": ..., "text": ...}.
type InlineSpanWire struct {
	Kind InlineSpanWireKind
	Text string
}

// NewInlineSpanWire builds the inline span wire from its arguments.
func NewInlineSpanWire(span core.InlineSpan) InlineSpanWire {
	return InlineSpanWire{Kind: InlineSpanWireKind(span.Kind), Text: span.Text}
}

// MarshalJSON writes the value as JSON.
func (s InlineSpanWire) MarshalJSON() ([]byte, error) {
	return jsonx.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: string(s.Kind), Text: s.Text})
}

// UnmarshalJSON reads the value from JSON; an unknown type is an error.
func (s *InlineSpanWire) UnmarshalJSON(data []byte) error {
	var wire struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	switch InlineSpanWireKind(wire.Type) {
	case InlineSpanWireKindText, InlineSpanWireKindStrong, InlineSpanWireKindCode:
		s.Kind = InlineSpanWireKind(wire.Type)
		s.Text = wire.Text
		return nil
	default:
		return errors.New("Unknown inline span type.") //nolint:staticcheck // ST1005: the message is part of the decode error text the API has always returned; kept as is.
	}
}
