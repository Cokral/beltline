// Covers the rules a task you start must pass (Intake.startTask): the manual
// entry it runs on, the title and repository checks, and its number.

package core

import "testing"

// mtBelt is a belt with the given trigger entries.
func mtBelt(entries ...TriggerEntry) Belt { return Belt{ID: "tasks", Triggers: entries} }

// mtFailure is the refusal message of err, or "" when err is nil.
func mtFailure(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	failure, ok := err.(Failure)
	if !ok {
		t.Fatalf("error is %T, want Failure", err)
	}
	return failure.Message
}

// TestABeltWithAManualTriggerHasOne: only a manual entry counts.
func TestABeltWithAManualTriggerHasOne(t *testing.T) {
	t.Parallel()
	if mtBelt(vtEntry("1", btIssuesOf(IssueFilter{}), "a/b")).HasManualTrigger() {
		t.Fatal("an issues-only belt reports a manual trigger")
	}
	if !mtBelt(vtEntry("1", btIssuesOf(IssueFilter{}), "a/b"), vtEntry("2", btManual(), "a/b")).HasManualTrigger() {
		t.Fatal("a belt with a manual entry reports none")
	}
}

// TestAManualTaskRunsOnTheOnlyManualEntry: with one manual entry, no id is needed.
func TestAManualTaskRunsOnTheOnlyManualEntry(t *testing.T) {
	t.Parallel()
	belt := mtBelt(vtEntry("1", btIssuesOf(IssueFilter{}), "a/b"), vtEntry("2", btManual(), "a/b"))
	entry, err := belt.ManualTrigger(nil)
	if err != nil || entry.ID != "2" {
		t.Fatalf("ManualTrigger(nil) = %q, %v; want entry 2", entry.ID, err)
	}
}

// TestAManualTaskNamesItsEntryWhenThereAreSeveral: an entry id picks one; none asks.
func TestAManualTaskNamesItsEntryWhenThereAreSeveral(t *testing.T) {
	t.Parallel()
	belt := mtBelt(vtEntry("2", btManual(), "a/b"), vtEntry("5", btManual(), "c/d"))
	if _, err := belt.ManualTrigger(nil); mtFailure(t, err) != "Pick a trigger." {
		t.Fatalf("no id: %q, want 'Pick a trigger.'", mtFailure(t, err))
	}
	id := "5"
	entry, err := belt.ManualTrigger(&id)
	if err != nil || entry.ID != "5" {
		t.Fatalf("ManualTrigger(5) = %q, %v; want entry 5", entry.ID, err)
	}
}

// TestAManualTaskIsRefusedWithoutAManualEntry: no manual entry, or an id that is
// not a manual entry, gets the belt-takes-no-tasks refusal.
func TestAManualTaskIsRefusedWithoutAManualEntry(t *testing.T) {
	t.Parallel()
	issues := mtBelt(vtEntry("1", btIssuesOf(IssueFilter{}), "a/b"))
	if _, err := issues.ManualTrigger(nil); mtFailure(t, err) != TaskNotAcceptedMessage {
		t.Fatalf("no manual entry: %q", mtFailure(t, err))
	}
	belt := mtBelt(vtEntry("1", btIssuesOf(IssueFilter{}), "a/b"), vtEntry("2", btManual(), "a/b"))
	id := "1"
	if _, err := belt.ManualTrigger(&id); mtFailure(t, err) != TaskNotAcceptedMessage {
		t.Fatalf("id of an issues entry: %q", mtFailure(t, err))
	}
}

// TestATaskNeedsATitleAndOneOfTheEntryRepositories: the title is trimmed, and the
// repository must be one the entry names.
func TestATaskNeedsATitleAndOneOfTheEntryRepositories(t *testing.T) {
	t.Parallel()
	entry := vtEntry("2", btManual(), "a/b")
	title, err := entry.CheckTask("a/b", "  Write docs  ")
	if err != nil || title != "Write docs" {
		t.Fatalf("CheckTask = %q, %v; want the trimmed title", title, err)
	}
	if _, err := entry.CheckTask("a/b", "   "); mtFailure(t, err) != "Give the task a title." {
		t.Fatalf("blank title: %q", mtFailure(t, err))
	}
	if _, err := entry.CheckTask("x/y", "Docs"); mtFailure(t, err) != "Pick one of the belt's repositories." {
		t.Fatalf("other repository: %q", mtFailure(t, err))
	}
}

// TestTheNextTaskNumberFollowsTheHighestOfThisBelt: one more than the highest
// manual key on the belt, counting negative keys, and 1 when there are none.
func TestTheNextTaskNumberFollowsTheHighestOfThisBelt(t *testing.T) {
	t.Parallel()
	task := func(belt, key string, kind SubjectKind) Run {
		return Run{BeltID: belt, Subject: Subject{Kind: kind, Key: key}}
	}
	if got := NextManualTaskNumber(nil, "tasks"); got != 1 {
		t.Fatalf("no runs: %d, want 1", got)
	}
	runs := []Run{
		task("tasks", "3", SubjectKindManual),
		task("tasks", "7", SubjectKindManual),
		task("tasks", "9", SubjectKindIssue),
		task("other", "20", SubjectKindManual),
		task("tasks", "x", SubjectKindManual),
	}
	if got := NextManualTaskNumber(runs, "tasks"); got != 8 {
		t.Fatalf("highest manual key 7: %d, want 8", got)
	}
	if got := NextManualTaskNumber([]Run{task("tasks", "-5", SubjectKindManual)}, "tasks"); got != -4 {
		t.Fatalf("only key -5: %d, want -4", got)
	}
}
