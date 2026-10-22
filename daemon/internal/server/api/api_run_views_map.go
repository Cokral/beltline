// The Markdown blocks of an attempt's output, and its artifacts.
package api

import (
	"errors"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// MarkdownBlockWireKind is the type tag of a Markdown block on the wire.
type MarkdownBlockWireKind string

// The MarkdownBlockWireKind values, as their wire strings.
const (
	MarkdownBlockWireKindHeading   MarkdownBlockWireKind = "heading"
	MarkdownBlockWireKindItem      MarkdownBlockWireKind = "item"
	MarkdownBlockWireKindCode      MarkdownBlockWireKind = "code"
	MarkdownBlockWireKindParagraph MarkdownBlockWireKind = "paragraph"
)

// MarkdownBlockWire is one ordered Markdown block with an explicit stable tag
// and named payload fields. The blocks are one struct:
// Kind selects the case, and only that case's fields are used. On the
// wire each case writes its own keys: heading {type, level, text}, item {type,
// marker, text}, code {type, text}, paragraph {type, text}.
type MarkdownBlockWire struct {
	Kind   MarkdownBlockWireKind
	Level  int
	Marker string
	Text   string
}

// NewMarkdownBlockWire ports `MarkdownBlockWire.init(_ block: MarkdownBlock)`.
func NewMarkdownBlockWire(block core.MarkdownBlock) MarkdownBlockWire {
	out := MarkdownBlockWire{Kind: MarkdownBlockWireKind(block.Kind)}
	switch block.Kind {
	case core.MarkdownBlockKindHeading:
		out.Level = block.Level
		out.Text = block.Text
	case core.MarkdownBlockKindItem:
		out.Marker = block.Marker
		out.Text = block.Text
	default:
		out.Text = block.Text
	}
	return out
}

// MarshalJSON ports `MarkdownBlockWire.encode(to:)`.
func (b MarkdownBlockWire) MarshalJSON() ([]byte, error) {
	switch b.Kind {
	case MarkdownBlockWireKindHeading:
		return jsonx.Marshal(struct {
			Type  string `json:"type"`
			Level int    `json:"level"`
			Text  string `json:"text"`
		}{Type: string(b.Kind), Level: b.Level, Text: b.Text})
	case MarkdownBlockWireKindItem:
		return jsonx.Marshal(struct {
			Type   string `json:"type"`
			Marker string `json:"marker"`
			Text   string `json:"text"`
		}{Type: string(b.Kind), Marker: b.Marker, Text: b.Text})
	default:
		return jsonx.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: string(b.Kind), Text: b.Text})
	}
}

// UnmarshalJSON ports `MarkdownBlockWire.init(from:)`; an unknown type is an error.
func (b *MarkdownBlockWire) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch MarkdownBlockWireKind(tag.Type) {
	case MarkdownBlockWireKindHeading:
		var wire struct {
			Level int    `json:"level"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*b = MarkdownBlockWire{Kind: MarkdownBlockWireKindHeading, Level: wire.Level, Text: wire.Text}
	case MarkdownBlockWireKindItem:
		var wire struct {
			Marker string `json:"marker"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*b = MarkdownBlockWire{Kind: MarkdownBlockWireKindItem, Marker: wire.Marker, Text: wire.Text}
	case MarkdownBlockWireKindCode, MarkdownBlockWireKindParagraph:
		var wire struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*b = MarkdownBlockWire{Kind: MarkdownBlockWireKind(tag.Type), Text: wire.Text}
	default:
		return errors.New("Unknown Markdown block type.") //nolint:staticcheck // ST1005: the message is part of the decode error text the API has always returned; kept as is.
	}
	return nil
}

// AttemptOutputResponse is an attempt's output as Markdown blocks.
// Availability is `available`, `missing`, or `notApplicable` for a non-agent
// attempt. Message is omitted when nil.
type AttemptOutputResponse struct {
	RunID        string              `json:"runId"`
	AttemptID    string              `json:"attemptId"`
	Availability string              `json:"availability"`
	Blocks       []MarkdownBlockWire `json:"blocks"`
	Message      *string             `json:"message,omitzero"`
}

// NewAttemptOutputResponse ports `AttemptOutputResponse.init(_ snapshot: CoordinatorAttemptOutputSnapshot)`.
// core.AttemptOutputPresent decides the availability, blocks and message; this maps them.
func NewAttemptOutputResponse(snapshot coordinator.CoordinatorAttemptOutputSnapshot) AttemptOutputResponse {
	presented := core.AttemptOutputPresent(snapshot.Stage, snapshot.Attempt, snapshot.Text)
	out := AttemptOutputResponse{
		RunID:        snapshot.RunID,
		AttemptID:    snapshot.Attempt.ID,
		Availability: string(presented.Availability),
		Blocks:       []MarkdownBlockWire{},
		Message:      presented.Message,
	}
	for _, block := range presented.Blocks {
		out.Blocks = append(out.Blocks, NewMarkdownBlockWire(block))
	}
	return out
}

// AttemptArtifactWire is one file an attempt left in its artifact folder. The
// The `id` is the name and is not encoded.
type AttemptArtifactWire struct {
	Name      string     `json:"name"`
	SizeBytes int64      `json:"sizeBytes"`
	URL       string     `json:"url"`
	ExpiresAt jsonx.Date `json:"expiresAt"`
}

// AttemptArtifactsResponse is an attempt's artifact list. Warning is the
// sentence shown above the list: what opening an agent's HTML may do.
type AttemptArtifactsResponse struct {
	RunID     string                `json:"runId"`
	AttemptID string                `json:"attemptId"`
	Artifacts []AttemptArtifactWire `json:"artifacts"`
	Warning   string                `json:"warning"`
}

// NewAttemptArtifactsResponse ports `AttemptArtifactsResponse.init(runId:attemptId:artifacts:)`;
// Warning is `StageCopy.artifactWarning`.
func NewAttemptArtifactsResponse(runID, attemptID string, artifacts []AttemptArtifactWire) AttemptArtifactsResponse {
	return AttemptArtifactsResponse{
		RunID:     runID,
		AttemptID: attemptID,
		Artifacts: artifacts,
		Warning:   core.StageCopyArtifactWarning,
	}
}
