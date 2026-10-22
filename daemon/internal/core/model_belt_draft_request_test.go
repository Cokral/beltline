// BeltDraftRequest's
// decoding and the unknown-operation cases of apply and located.

package core

import (
	"strings"
	"testing"
)

// TestUnknownBeltDraftOperationIsAnError checks that a hand-built request with an
// unknown operation is a problem and an error, never a silent no-op.
func TestUnknownBeltDraftOperationIsAnError(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperation("fork-move")}
	problems := BeltDraftEditingLocated(request)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "fork-move") {
		t.Errorf("located problems = %+v", problems)
	}
	if _, _, err := BeltDraftEditingApplyResult(request); err == nil {
		t.Error("an unknown operation applied without an error")
	}
	if _, err := BeltDraftEditingApply(request); err == nil {
		t.Error("an unknown operation applied without an error")
	}
}
