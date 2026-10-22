package core

import (
	"strings"
	"testing"
)

// docHoist is Document.hoisting with its default section.
func docHoist(text string) string {
	return DocumentHoisting(text, DocumentHoistingDefaultSection)
}

// docQuestions and docSections are the fixtures of DocumentTests.
const (
	docQuestions = "## Open questions\n\n1. Which page?\n2. Does it happen for new items only?"
	docSections  = "## Goal\n\nX\n\n## Behaviour\n\nY\n\n## Non-goals\n\nZ"
)

// TestQuestionsLastBecomeFirstAndKeepTheOtherSectionsInOrder
func TestQuestionsLastBecomeFirstAndKeepTheOtherSectionsInOrder(t *testing.T) {
	t.Parallel()
	input := "# Title\n\n" + docSections + "\n\n" + docQuestions
	expected := "# Title\n\n" + docQuestions + "\n\n" + docSections
	if got := docHoist(input); got != expected {
		t.Fatalf("hoist = %q, want %q", got, expected)
	}
	if got := docHoist(OutcomeParserStrip(input + "\n\nOutcome: complete")); got != expected {
		t.Fatalf("hoist of stripped = %q, want %q", got, expected)
	}
	if got := docHoist(expected); got != expected {
		t.Fatalf("hoist of hoisted = %q", got)
	}
}

// TestWithoutQuestionsNothingChanges
func TestWithoutQuestionsNothingChanges(t *testing.T) {
	t.Parallel()
	input := "# Title\n\n## Goal\n\nIt works."
	if got := docHoist(input); got != input {
		t.Fatalf("hoist = %q, want unchanged", got)
	}
	if got := docHoist(""); got != "" {
		t.Fatalf("hoist of empty = %q", got)
	}
}

// TestOldSimulatedTriageAndCustomDocumentsAreHoisted
func TestOldSimulatedTriageAndCustomDocumentsAreHoisted(t *testing.T) {
	t.Parallel()
	// The needs-info body from SimulatedRunner, deliberately kept in its old order.
	old := "# Triage\n\nThe issue doesn't say where it happens.\n\n## Open questions\n1. Which page shows the problem?\n2. Does it happen for new items only?"
	first := "# Triage\n\n## Open questions\n1. Which page shows the problem?\n2. Does it happen for new items only?\n\nThe issue doesn't say where it happens."
	if docHoist(old) != docHoist(first) {
		t.Fatalf("old %q and first %q hoist differently", docHoist(old), docHoist(first))
	}
	if got := docHoist("Intro\n\n" + docQuestions); got != docQuestions+"\n\nIntro" {
		t.Fatalf("intro hoist = %q", got)
	}
	if got := docHoist("# Title\n\n## Goal\nX\n\n## OPEN QUESTIONS\n1. Why?"); !strings.HasPrefix(got, "# Title\n\n## OPEN QUESTIONS\n1. Why?") {
		t.Fatalf("upper-case section hoist = %q", got)
	}
}
