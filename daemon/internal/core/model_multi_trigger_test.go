// Tests of multi-trigger belts: the model, and the update that keeps matched repositories.

package core

import (
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// btApproval is an approval stage with one outcome.
func btApproval() Stage {
	return NewStage(StageArgs{ID: "approve", Type: StageTypeApprove, Outcomes: []Outcome{btOutcome("approved")}})
}

// btEntry is a trigger entry with the given id, trigger and repositories.
func btEntry(id string, trigger Trigger, repos ...string) TriggerEntry {
	return TriggerEntry{ID: id, Trigger: trigger, Repos: append([]string{}, repos...)}
}

// btMixed is a belt with three trigger entries, a base, one
// policy off, and AFK always.
func btMixed() Belt {
	belt := DefaultBelt("shared", "Shared", []TriggerEntry{
		btEntry("1", btIssuesOf(IssueFilter{Label: "agent", Assignee: TriggerAssigneeAnyone, Poll: 30}), "acme/billing"),
		btEntry("2", btIssuesOf(IssueFilter{Label: "bug", Assignee: TriggerAssigneeMe, Poll: 300}), "acme/web", "acme/platform"),
		btEntry("3", btJiraOf(JiraQuery{Jql: "project = PAY", Poll: 60}), "acme/billing", "acme/infra"),
	}, []Stage{btApproval()})
	belt.Base = "develop"
	belt.Policies = Policies{RetriageOnEdit: false, StopOnClose: true, CommentOnHold: true, WaitForBlockers: true}
	belt.Afk = AfkModeAlways
	belt.Forks = []Fork{}
	return belt
}

// TestEntriesRoundTripIndependentlyOfOrder reorders and edits entries, then saves and reads them back.
func TestEntriesRoundTripIndependentlyOfOrder(t *testing.T) {
	t.Parallel()
	belt := btMixed()
	belt.MoveTrigger("3", -1)
	belt.UpdateTrigger("2", func(e *TriggerEntry) {
		nine := 900
		e.Trigger.SetPoll(&nine)
	})
	selected := belt.triggerIndex("1")
	entryOne := belt.Triggers[selected]
	belt.MoveTrigger("1", 1)
	if d := testkit.Diff(entryOne, belt.Triggers[belt.triggerIndex("1")]); d != "" {
		t.Fatalf("entry 1 changed when it moved:\n%s", d)
	}
	if got := btIDs(belt.Triggers); testkit.Diff([]string{"3", "1", "2"}, got) != "" {
		t.Fatalf("order = %v, want 3 1 2", got)
	}
	again := btDecode[Belt](t, btEncode(t, belt))
	if d := testkit.Diff(belt.Triggers, again.Triggers); d != "" {
		t.Fatalf("triggers differ after a round trip:\n%s", d)
	}
	if again.Base != "develop" || again.Policies.RetriageOnEdit || again.Afk != AfkModeAlways {
		t.Fatalf("base %q, retriage %v, afk %q", again.Base, again.Policies.RetriageOnEdit, again.Afk)
	}
	if d := testkit.Diff(belt.Stages, again.Stages); d != "" {
		t.Fatalf("stages differ:\n%s", d)
	}
	second := again.Triggers[again.triggerIndex("2")]
	if d := testkit.Diff(btIssuesOf(IssueFilter{Label: "bug", Assignee: TriggerAssigneeMe, Poll: 900}), second.Trigger); d != "" {
		t.Errorf("entry 2 trigger:\n%s", d)
	}
	if d := testkit.Diff([]string{"acme/web", "acme/platform"}, second.Repos); d != "" {
		t.Errorf("entry 2 repos:\n%s", d)
	}
	if d := testkit.Diff([]string{"acme/billing", "acme/infra"}, again.Triggers[again.triggerIndex("3")].Repos); d != "" {
		t.Errorf("entry 3 repos:\n%s", d)
	}
}

// TestLegacyFilesDecodeToTheSameEntryAndDoNotRewrite reads a belt saved with one
// trigger twice: both reads give the same entry, the one with the id 1.
func TestLegacyFilesDecodeToTheSameEntryAndDoNotRewrite(t *testing.T) {
	t.Parallel()
	json := `{"id":"b","name":"B","version":3,"stages":[],"repos":["a/b"],"trigger":{"kind":"jira","jql":"project = PAY","poll":30,"base":"develop","retriageOnEdit":false,"stopOnClose":true,"commentOnHold":false}}`
	first := btDecode[Belt](t, json)
	second := btDecode[Belt](t, json)
	if d := testkit.Diff(first.Triggers, []TriggerEntry{{ID: "1", Trigger: btJiraOf(JiraQuery{Jql: "project = PAY", Poll: 30}), Repos: []string{"a/b"}}}); d != "" {
		t.Fatalf("legacy entry:\n%s", d)
	}
	if d := testkit.Diff(first, second); d != "" {
		t.Fatalf("two reads differ:\n%s", d)
	}
	if first.Base != "develop" {
		t.Fatalf("base = %q, want develop", first.Base)
	}
	if strings.Contains(json, `"triggers"`) {
		t.Fatal("the legacy fixture unexpectedly has triggers")
	}
}

// TestEditingOneEntryLeavesTheOther changes one entry's trigger and repositories and checks the other.
func TestEditingOneEntryLeavesTheOther(t *testing.T) {
	t.Parallel()
	belt := btMixed()
	belt.Triggers[0].Trigger = btReviewsOf(ReviewFilter{Reviewers: TriggerReviewersMeAndTeams, Poll: 30})
	belt.Triggers[0].Repos = []string{"acme/other"}
	if d := testkit.Diff(btIssuesOf(IssueFilter{Label: "bug", Assignee: TriggerAssigneeMe, Poll: 300}), belt.Triggers[1].Trigger); d != "" {
		t.Fatalf("entry 2 trigger changed:\n%s", d)
	}
	if d := testkit.Diff([]string{"acme/web", "acme/platform"}, belt.Triggers[1].Repos); d != "" {
		t.Fatalf("entry 2 repos changed:\n%s", d)
	}
	summary := belt.Summary()
	for _, want := range []string{"Review requests", "GitHub issues", "Jira tickets"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q does not name %q", summary, want)
		}
	}
}

// TestAnUpdateKeepsMatchedRepositoriesAndLeavesTheRestEmpty covers
// keepingRepositories: matched entries keep their repositories, the rest are
// left empty and reported as unmatched; a single entry matches by kind.
func TestAnUpdateKeepsMatchedRepositoriesAndLeavesTheRestEmpty(t *testing.T) {
	t.Parallel()
	local := NewBeltFromTrigger(BeltFromTriggerArgs{ID: "belt-1", Name: "Local", Version: new(2), Trigger: btIssuesOf(IssueFilter{Label: "agent"}),
		Repos: []string{"local/one"}, Base: "release", Stages: []Stage{btApproval()}, Afk: new(AfkModeAlways)})
	local.Triggers = []TriggerEntry{
		btEntry("1", btIssuesOf(IssueFilter{Label: "agent"}), "local/one"),
		btEntry("2", btManual(), "local/two"),
	}
	incoming := DefaultBelt("pkg", "Pkg", []TriggerEntry{
		btEntry("2", btManual()),
		btEntry("1", btIssuesOf(IssueFilter{Label: "bug"})),
		btEntry("3", btJiraOf(JiraQuery{Jql: "project = PAY"}), "should-not-stick"),
		btEntry("2", btReviewsOf(DefaultReviewFilter())),
	}, []Stage{btApproval()})
	incoming.Version = 3
	kept, unmatched := incoming.KeepingRepositories(local)
	if d := testkit.Diff([]string{"local/two"}, kept.Triggers[0].Repos); d != "" {
		t.Errorf("entry 0 repos:\n%s", d)
	}
	if d := testkit.Diff([]string{"local/one"}, kept.Triggers[1].Repos); d != "" {
		t.Errorf("entry 1 repos:\n%s", d)
	}
	if len(kept.Triggers[2].Repos) != 0 || len(kept.Triggers[3].Repos) != 0 {
		t.Errorf("unmatched entries kept repositories: %v and %v", kept.Triggers[2].Repos, kept.Triggers[3].Repos)
	}
	if d := testkit.Diff([]int{2, 3}, unmatched); d != "" {
		t.Errorf("unmatched = %v, want [2 3]", unmatched)
	}
	legacyLocal := NewBeltFromTrigger(BeltFromTriggerArgs{ID: "belt-1", Name: "Local", Trigger: btIssuesOf(IssueFilter{Label: "agent"}),
		Repos: []string{"local/legacy"}, Stages: []Stage{btApproval()}})
	legacyIncoming := DefaultBelt("pkg", "Pkg", []TriggerEntry{btEntry("other", btIssuesOf(IssueFilter{Label: "agent"}))}, []Stage{btApproval()})
	got, _ := legacyIncoming.KeepingRepositories(legacyLocal)
	if d := testkit.Diff([]string{"local/legacy"}, got.Repos()); d != "" {
		t.Errorf("single-entry match repos:\n%s", d)
	}
}

// TestAddRemoveAndUpdateTriggers covers AddTrigger, RemoveTrigger and NextTriggerID.
func TestAddRemoveAndUpdateTriggers(t *testing.T) {
	t.Parallel()
	belt := btMixed()
	if got := belt.NextTriggerID(); got != "4" {
		t.Fatalf("next id = %q, want 4", got)
	}
	belt.AddTrigger(btManual())
	if got := btIDs(belt.Triggers); testkit.Diff([]string{"1", "2", "3", "4"}, got) != "" {
		t.Fatalf("ids after add = %v", got)
	}
	if len(belt.Triggers[3].Repos) != 0 {
		t.Errorf("added entry repos = %v, want none", belt.Triggers[3].Repos)
	}
	belt.RemoveTrigger("2")
	if got := btIDs(belt.Triggers); testkit.Diff([]string{"1", "3", "4"}, got) != "" {
		t.Fatalf("ids after remove = %v", got)
	}
	if got := belt.NextTriggerID(); got != "5" {
		t.Errorf("next id after remove = %q, want 5 (ids are not reused below the top)", got)
	}
}

// TestFocusingAndRemoveRepositories covers Focusing and RemoveRepositories.
func TestFocusingAndRemoveRepositories(t *testing.T) {
	t.Parallel()
	belt := btMixed()
	focused := belt.Focusing(belt.Triggers[2])
	if len(focused.Triggers) != 1 || focused.Triggers[0].ID != "3" {
		t.Fatalf("focused entries = %v", btIDs(focused.Triggers))
	}
	if d := testkit.Diff([]string{"acme/billing", "acme/infra"}, focused.Repos()); d != "" {
		t.Errorf("focused repos:\n%s", d)
	}
	if len(belt.Triggers) != 3 {
		t.Errorf("Focusing changed the original belt: %d entries", len(belt.Triggers))
	}
	copyBelt := belt
	copyBelt.RemoveRepositories()
	for _, e := range copyBelt.Triggers {
		if len(e.Repos) != 0 {
			t.Errorf("entry %s kept repositories %v", e.ID, e.Repos)
		}
	}
	if len(belt.Triggers[0].Repos) == 0 {
		t.Error("RemoveRepositories on a copy changed the original's repositories")
	}
}

// TestMenuInputsFollowEveryEntry covers CommonProvides, MenuInputs and StageKind.
func TestMenuInputsFollowEveryEntry(t *testing.T) {
	t.Parallel()
	mixed := btMixed()
	if d := testkit.Diff([]Input{InputBaseBranch, InputTicket}, btInputs(mixed.CommonProvides())); d != "" {
		t.Errorf("common provides (issues, jira):\n%s", d)
	}
	if d := testkit.Diff([]Input{InputBaseBranch, InputBranch, InputPullRequest, InputTicket}, btInputs(mixed.MenuInputs())); d != "" {
		t.Errorf("menu inputs:\n%s", d)
	}
	if mixed.StageKind() != TriggerKindIssues {
		t.Errorf("stage kind = %q, want issues", mixed.StageKind())
	}
	reviews := DefaultBelt("r", "R", []TriggerEntry{btEntry("1", btReviewsOf(DefaultReviewFilter()), "a/b")}, nil)
	if d := testkit.Diff([]Input{InputBranch, InputPullRequest}, btInputs(reviews.CommonProvides())); d != "" {
		t.Errorf("review common provides:\n%s", d)
	}
	if reviews.StageKind() != TriggerKindReviews {
		t.Errorf("review stage kind = %q, want reviews", reviews.StageKind())
	}
	if !mixed.CanHave(StageTypeWatch) || !reviews.CanHave(StageTypeWatch) {
		t.Error("a watch needs a pull request, which every trigger kind here provides")
	}
	if empty := (Belt{}); len(empty.CommonProvides()) != 0 || len(empty.MenuInputs()) != 0 {
		t.Error("a belt with no entries provides nothing")
	}
}

// TestInputsSubsetComparesEveryInput covers the empty set, a full match and a
// missing input.
func TestInputsSubsetComparesEveryInput(t *testing.T) {
	t.Parallel()
	provided := inputSet(InputBranch, InputPullRequest)
	if !InputsSubset(map[Input]struct{}{}, provided) {
		t.Error("an empty need is met by anything")
	}
	if !InputsSubset(inputSet(InputBranch), provided) {
		t.Error("a need that is provided is met")
	}
	if InputsSubset(inputSet(InputBranch, InputTicket), provided) {
		t.Error("a need the belt does not provide is not met")
	}
}

// btIDs lists the ids of entries, in order.
func btIDs(entries []TriggerEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}
