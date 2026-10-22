// Tests of the task draft.

package core

import (
	"reflect"
	"testing"
)

// taskDraftTestBelt is a belt named Tasks with the given entries and no stages.
func taskDraftTestBelt(entries []TriggerEntry) Belt {
	return DefaultBelt("tasks", "Tasks", entries, []Stage{})
}

func TestDefaultsToTheFirstManualEntry(t *testing.T) {
	t.Parallel()
	belt := taskDraftTestBelt([]TriggerEntry{
		{ID: "1", Trigger: TriggerKindIssues.Fresh(), Repos: []string{"acme/one"}},
		{ID: "2", Trigger: TriggerKindManual.Fresh(), Repos: []string{"acme/two"}},
		{ID: "3", Trigger: TriggerKindManual.Fresh(), Repos: []string{"acme/three"}},
	})
	if got := TaskDraftEntryID(nil, belt); got == nil || *got != "2" {
		t.Errorf("entry with none selected = %v, want 2", got)
	}
	if got := TaskDraftEntryID(new("3"), belt); got == nil || *got != "3" {
		t.Errorf("entry 3 selected = %v, want 3", got)
	}
	if got := TaskDraftEntryID(new("gone"), belt); got == nil || *got != "2" {
		t.Errorf("entry for a missing id = %v, want 2", got)
	}
}

func TestRetainsARepositoryOnlyWhileTheEntryHasIt(t *testing.T) {
	t.Parallel()
	belt := taskDraftTestBelt([]TriggerEntry{
		{ID: "2", Trigger: TriggerKindManual.Fresh(), Repos: []string{"acme/two", "acme/three"}},
	})
	if got := TaskDraftRepo("acme/three", new("2"), belt); got != "acme/three" {
		t.Errorf("kept repo = %q, want acme/three", got)
	}
	if got := TaskDraftRepo("acme/other", new("2"), belt); got != "acme/two" {
		t.Errorf("replacement repo = %q, want acme/two", got)
	}
	if got := TaskDraftRepo("acme/two", new("none"), belt); got != "" {
		t.Errorf("repo for a missing entry = %q, want empty", got)
	}
}

func TestStartNeedsANonBlankTitleAndAValidRepository(t *testing.T) {
	t.Parallel()
	belt := taskDraftTestBelt([]TriggerEntry{{ID: "2", Trigger: TriggerKindManual.Fresh(), Repos: []string{"acme/two"}}})
	selection := TaskDraftSelectionOf(belt, new("2"), "acme/two", "Do it")
	if !selection.CanStart {
		t.Errorf("canStart with a title and repository = false, want true")
	}
	selection = TaskDraftSelectionOf(belt, new("2"), "acme/two", "   ")
	if selection.CanStart {
		t.Errorf("canStart with a blank title = true, want false")
	}
	if selection.Problem == nil {
		t.Errorf("problem with a blank title = nil, want a message")
	}
	selection = TaskDraftSelectionOf(belt, new("2"), "acme/other", "Do it")
	if !selection.CanStart {
		t.Errorf("canStart with a repository the entry lacks = false, want true (it falls back)")
	}
	if selection.Repo != "acme/two" {
		t.Errorf("repo = %q, want acme/two", selection.Repo)
	}
	if TaskDraftCanStart("Do it", "acme/other", []string{"acme/two"}) {
		t.Errorf("canStart for a repository outside the entry = true, want false")
	}
}

func TestNoManualEntriesLeavesNothingToStart(t *testing.T) {
	t.Parallel()
	belt := taskDraftTestBelt([]TriggerEntry{{ID: "1", Trigger: TriggerKindIssues.Fresh(), Repos: []string{"acme/one"}}})
	selection := TaskDraftSelectionOf(belt, nil, "acme/one", "Do it")
	if selection.CanStart {
		t.Errorf("canStart with no manual entry = true, want false")
	}
	if selection.Entry != nil {
		t.Errorf("entry = %+v, want nil", *selection.Entry)
	}
}

// TestManualsKeepTheirPositionInTheBelt: a manual entry's position is its index among all the
// belt's entries, not among the manual ones.
func TestManualsKeepTheirPositionInTheBelt(t *testing.T) {
	t.Parallel()
	belt := taskDraftTestBelt([]TriggerEntry{
		{ID: "1", Trigger: TriggerKindIssues.Fresh(), Repos: []string{"acme/one"}},
		{ID: "2", Trigger: TriggerKindManual.Fresh(), Repos: []string{"acme/two"}},
		{ID: "3", Trigger: TriggerKindManual.Fresh(), Repos: []string{"acme/three"}},
	})
	want := []TaskDraftEntry{
		{ID: "2", Position: 1, Repos: []string{"acme/two"}},
		{ID: "3", Position: 2, Repos: []string{"acme/three"}},
	}
	if got := TaskDraftManuals(belt); !reflect.DeepEqual(got, want) {
		t.Fatalf("TaskDraftManuals = %+v, want %+v", got, want)
	}
}
