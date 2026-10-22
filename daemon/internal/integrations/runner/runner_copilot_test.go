// Tests of the Copilot adapter and its events log, and of the usage it reports.

package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

func TestCompletionIgnoresIdlePartialOldAndForeignEvents(t *testing.T) {
	t.Parallel()
	idle := `{"type":"session.idle"}`
	wait := `{"type":"session.input_required"}`
	user := `{"type":"user.message","data":{"text":"hello"}}`
	old := `{"type":"session.task_complete","data":{"summary":"earlier"}}`
	broken := "{not json"
	partial := `{"type":"session.task_complete"`
	done := `{"type":"session.task_complete","data":{"summary":"now"}}`
	log := strings.Join([]string{old, idle, wait, user, broken, partial}, "\n") + "\n" + partial
	if got := copilotCompletionIn(log, 0); got == nil || got.Kind != LogEndingFinished {
		t.Fatalf("completion(0) = %v", got)
	}
	if got := copilotCompletionIn(log, 1); got != nil {
		t.Fatalf("completion(1) = %v", got)
	}
	if got := copilotCompletionIn(log+"\n"+done+"\n", 1); got == nil || got.Kind != LogEndingFinished {
		t.Fatalf("completion after a new task = %v", got)
	}
	if !copilotDidAbortIn(log+"\n"+`{"type":"abort"}`+"\n", 1) {
		t.Fatalf("abort not seen")
	}
	if copilotDidAbortIn(`{"type":"abort"}`+"\n"+idle+"\n", 1) {
		t.Fatalf("abort before the boundary seen")
	}

	root := t.TempDir()
	state := filepath.Join(root, "state")
	const name = "bl-copilot"
	testkit.WriteFile(t, filepath.Join(state, name, "events.jsonl"), old+"\n"+idle+"\n")
	adapter := CopilotAdapter{StateRoot: state}
	if got := adapter.completion(name, 1, nil); got != nil {
		t.Fatalf("own completion after the boundary = %v", got)
	}
	if got := adapter.completion("other-conversation", 0, nil); got != nil {
		t.Fatalf("missing session completion = %v", got)
	}
	if got := adapter.exactIdentity("other-conversation", nil); got != nil {
		t.Fatalf("missing session identity = %v", *got)
	}
	testkit.WriteFile(t, filepath.Join(state, "other-conversation", "events.jsonl"), done+"\n")
	if got := adapter.completion(name, 1, nil); got != nil {
		t.Fatalf("own completion after a foreign finish = %v", got)
	}
	if got := adapter.completion("other-conversation", 0, nil); got == nil || got.Kind != LogEndingFinished {
		t.Fatalf("foreign completion = %v", got)
	}
}

func TestAnAmbiguousSessionIsNotChosen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const name = "bl-shared"
	for _, id := range []string{"one", "two"} {
		folder := filepath.Join(root, id)
		testkit.WriteFile(t, filepath.Join(folder, "workspace.yaml"), "name: "+name+"\n")
		testkit.WriteFile(t, filepath.Join(folder, "events.jsonl"), `{"type":"session.task_complete"}`)
	}
	adapter := CopilotAdapter{StateRoot: root}
	if got := adapter.events(name, nil); got != nil {
		t.Fatalf("events of an ambiguous session = %q", *got)
	}
	if got := adapter.exactIdentity(name, nil); got != nil {
		t.Fatalf("identity of an ambiguous session = %q", *got)
	}
	got := adapter.completion(name, 0, nil)
	if got == nil || got.Kind != LogEndingFailed || got.Message != "more than one Copilot session matches "+name {
		t.Fatalf("completion = %+v", got)
	}
}

func TestCheckpointLinesAreTheCompletionBoundary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const name = "bl-boundary"
	events := filepath.Join(root, name, "events.jsonl")
	first := `{"type":"session.task_complete","data":{"total_nano_aiu":11000000000}}`
	testkit.WriteFile(t, events, first+"\n")
	adapter := CopilotAdapter{StateRoot: root}
	checkpoint := adapter.UsageCheckpoint(root, "sid", name, nil)
	if checkpoint == nil || checkpoint.Lines == nil || *checkpoint.Lines != 1 {
		t.Fatalf("checkpoint = %+v", checkpoint)
	}
	if checkpoint.SessionID == nil || *checkpoint.SessionID != name {
		t.Fatalf("checkpoint session = %+v", checkpoint)
	}
	if got := adapter.completion(name, *checkpoint.Lines, nil); got != nil {
		t.Fatalf("completion at the boundary = %v", got)
	}
	second := `{"type":"session.task_complete","data":{"total_nano_aiu":34000000000}}`
	testkit.WriteFile(t, events, first+"\n"+second+"\n")
	if got := adapter.completion(name, 1, nil); got == nil || got.Kind != LogEndingFinished {
		t.Fatalf("completion after the boundary = %v", got)
	}
	log, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if usage := CopilotUsageOf(string(log), 11_000_000_000); usage == nil || usage.CostNanos == nil || *usage.CostNanos != 230_000_000 {
		t.Fatalf("usage = %+v", usage)
	}
}
