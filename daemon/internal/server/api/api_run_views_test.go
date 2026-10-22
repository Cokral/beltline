// Golden checks of the run view wire types: RunViewResponse and every type it nests, the attempt
// output and artifacts, and the run sessions.

package api_test

import (
	"encoding/json"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// TestUnknownWireKindsAreErrors checks that a span, a block or a factory node of a kind the wire
// does not know is a decoding error, as the decoders throw.
func TestUnknownWireKindsAreErrors(t *testing.T) {
	t.Parallel()
	var span api.InlineSpanWire
	if err := json.Unmarshal([]byte(`{"text":"x","type":"link"}`), &span); err == nil {
		t.Error("an unknown inline span type decoded")
	}
	var block api.MarkdownBlockWire
	if err := json.Unmarshal([]byte(`{"text":"x","type":"table"}`), &block); err == nil {
		t.Error("an unknown Markdown block type decoded")
	}
	var node api.FactoryNodeWire
	if err := json.Unmarshal([]byte(`{"kind":"tunnel","value":"x"}`), &node); err == nil {
		t.Error("an unknown factory node kind decoded")
	}
}
