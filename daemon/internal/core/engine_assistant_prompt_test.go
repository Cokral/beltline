package core

import (
	"reflect"
	"testing"
)

// TestAssistantPromptLatestKeepsTheNewestVersion checks that each definition's
// newest version and every state file are current, and the rest are older.
func TestAssistantPromptLatestKeepsTheNewestVersion(t *testing.T) {
	t.Parallel()
	library := map[string][]byte{
		"belts/a/v1.json": {}, "belts/a/v10.json": {}, "belts/a/v9.json": {}, "belts/a/state.json": {},
		"agents/b/v1.md": {}, "agents/b/v2.md": {}, "README.md": {},
	}
	current, older := assistantPromptLatest(library)
	wantCurrent := []string{"README.md", "agents/b/v2.md", "belts/a/state.json", "belts/a/v10.json"}
	wantOlder := []string{"agents/b/v1.md", "belts/a/v1.json", "belts/a/v9.json"}
	if !reflect.DeepEqual(current, wantCurrent) {
		t.Fatalf("current = %v, want %v", current, wantCurrent)
	}
	if !reflect.DeepEqual(older, wantOlder) {
		t.Fatalf("older = %v, want %v", older, wantOlder)
	}
}

// TestAssistantPromptVersionReadsOnlyVersionNames checks which file names carry a
// version: a `v<n>` name whose number parses, and nothing else.
func TestAssistantPromptVersionReadsOnlyVersionNames(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]int{"belts/a/v3.json": 3, "agents/b/v12.md": 12, "belts/a/v1.2.json": 1} {
		if got := assistantPromptVersion(path); got == nil || *got != want {
			t.Errorf("%s = %v, want %d", path, got, want)
		}
	}
	for _, path := range []string{"belts/a/state.json", "agents/b/vx.md", "agents/b/v.md"} {
		if got := assistantPromptVersion(path); got != nil {
			t.Errorf("%s reads as version %d", path, *got)
		}
	}
}
