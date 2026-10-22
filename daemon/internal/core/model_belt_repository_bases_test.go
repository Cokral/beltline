package core

import (
	"reflect"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// repositoryBasesTestBelt is two entries: entry "1" lists acme/app, entry "2"
// lists acme/app and acme/web, so acme/app is listed twice.
func repositoryBasesTestBelt() Belt {
	belt := draftTestBelt([]Stage{})
	belt.Triggers[0].Repos = []string{"acme/app"}
	belt.Triggers = append(belt.Triggers, TriggerEntry{ID: "2", Trigger: TriggerKindIssues.Fresh(), Repos: []string{"acme/app", "acme/web"}})
	belt.RepositoryBases = map[string]string{"acme/app": "release/app", "acme/web": "release/web"}
	return belt
}

// TestRepositoryBasesEncodeOnlyNonemptyOverrides: empty and blank overrides are
// not written, an empty map is not written at all, and a saved override reads back.
func TestRepositoryBasesEncodeOnlyNonemptyOverrides(t *testing.T) {
	t.Parallel()
	belt := repositoryBasesTestBelt()
	belt.RepositoryBases["acme/web"] = "  "
	belt.RepositoryBases["acme/none"] = ""
	data, err := jsonx.Marshal(belt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"repositoryBases":{"acme/app":"release/app"}`) {
		t.Fatalf("encoded belt = %s, want only the nonempty override", data)
	}
	var back Belt
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.RepositoryBases, map[string]string{"acme/app": "release/app"}) {
		t.Fatalf("read back = %v", back.RepositoryBases)
	}
	empty := repositoryBasesTestBelt()
	empty.RepositoryBases = map[string]string{"acme/app": ""}
	data, err = jsonx.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "repositoryBases") {
		t.Fatalf("an empty map was written: %s", data)
	}
}

// TestBeltsSavedWithoutOverridesDecodeWithNone: a belt with no key, including one
// saved with a single nested trigger, decodes with no overrides and the same base.
func TestBeltsSavedWithoutOverridesDecodeWithNone(t *testing.T) {
	t.Parallel()
	legacy := `{"id":"old","name":"Old","version":1,"trigger":{"kind":"issues","issues":{"labels":["agent"],"assignee":"anyone","poll":300}},` +
		`"repos":["acme/app"],"base":"develop","policies":{"retriageOnEdit":true,"stopOnClose":true,"commentOnHold":false},"stages":[],"afk":"global"}`
	var belt Belt
	if err := json.Unmarshal([]byte(legacy), &belt); err != nil {
		t.Fatal(err)
	}
	if belt.RepositoryBases != nil || belt.Base != "develop" {
		t.Fatalf("legacy belt = base %q, overrides %v; want develop and none", belt.Base, belt.RepositoryBases)
	}
	if got := belt.RepositoryBase("acme/app"); got != "" {
		t.Fatalf("override = %q, want none", got)
	}
}

// TestDraftOverridesFollowTheListings: a repository listed twice keeps one
// override; reordering keeps it; removing one listing keeps it while another
// lists the repository; removing the last listing, by repository or by trigger,
// discards it; adding it back starts without one.
func TestDraftOverridesFollowTheListings(t *testing.T) {
	t.Parallel()
	belt := repositoryBasesTestBelt()
	if got := belt.ListedRepositories(); !reflect.DeepEqual(got, []string{"acme/app", "acme/web"}) {
		t.Fatalf("listed = %v, want each repository once in order", got)
	}
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerMove, EntryID: new("2"), Offset: new(-1)})
	if !reflect.DeepEqual(belt.RepositoryBases, map[string]string{"acme/app": "release/app", "acme/web": "release/web"}) {
		t.Fatalf("after reorder = %v", belt.RepositoryBases)
	}
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationRepositoryRemove, EntryID: new("1"), Repository: new("acme/app")})
	if belt.RepositoryBase("acme/app") != "release/app" {
		t.Fatalf("removing one of two listings dropped the override: %v", belt.RepositoryBases)
	}
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationRepositoryRemove, EntryID: new("2"), Repository: new("acme/app")})
	if _, ok := belt.RepositoryBases["acme/app"]; ok {
		t.Fatalf("removing the last listing kept the override: %v", belt.RepositoryBases)
	}
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationRepositoryAdd, EntryID: new("1"), Repository: new("acme/app")})
	if belt.RepositoryBase("acme/app") != "" {
		t.Fatalf("re-added repository starts with an override %q", belt.RepositoryBase("acme/app"))
	}
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerRemove, EntryID: new("2")})
	if _, ok := belt.RepositoryBases["acme/web"]; ok {
		t.Fatalf("removing the trigger that listed acme/web kept its override: %v", belt.RepositoryBases)
	}
	if belt.RepositoryBase("acme/app") != "" {
		t.Fatal("the override of the kept listing changed")
	}
}

// TestUnlistedOverrideIsRefusedWhereItIsLocated: an override for a repository no
// trigger lists is a save refusal naming the field. An empty one is not a problem.
func TestUnlistedOverrideIsRefusedWhereItIsLocated(t *testing.T) {
	t.Parallel()
	belt := repositoryBasesTestBelt()
	belt.RepositoryBases["acme/other"] = "release/other"
	refusals := BeltStructureRefusals(belt)
	if len(refusals) != 1 || refusals[0].Location == nil || refusals[0].Location.Kind != "belt" ||
		refusals[0].Location.Field == nil || *refusals[0].Location.Field != "repositoryBase:acme/other" {
		t.Fatalf("refusals = %+v, want one located at repositoryBase:acme/other", refusals)
	}
	if !strings.Contains(refusals[0].Message, "acme/other") {
		t.Fatalf("message %q does not name the repository", refusals[0].Message)
	}
	if located := BeltStructureLocated(belt); len(located) != 1 {
		t.Fatalf("located problems = %+v, want the same one", located)
	}
	belt.RepositoryBases["acme/other"] = " "
	if refusals := BeltStructureRefusals(belt); len(refusals) != 0 {
		t.Fatalf("an empty override is a refusal: %+v", refusals)
	}
}

// TestRepositoryBaseFieldsFollowTheTriggers: the shared card's fields are one per
// distinct repository in first-appearance order, and only when a trigger offers a
// base branch; a review-only belt has none.
func TestRepositoryBaseFieldsFollowTheTriggers(t *testing.T) {
	t.Parallel()
	form := TriggerFormPresentationMake(repositoryBasesTestBelt())
	if !form.ShowsBase || form.RepositoryBases == nil {
		t.Fatalf("form = %+v, want a branch group", form)
	}
	group := form.RepositoryBases
	if group.Placeholder != "Use shared base branch" || group.Heading != "Repository base branches" || group.Help == "" {
		t.Fatalf("group = %+v", group)
	}
	var repos []string
	for _, field := range group.Fields {
		repos = append(repos, field.Repository)
	}
	if !reflect.DeepEqual(repos, []string{"acme/app", "acme/web"}) {
		t.Fatalf("fields = %v", repos)
	}
	review := draftTestBelt([]Stage{})
	review.Triggers = []TriggerEntry{{ID: "1", Trigger: TriggerKindReviews.Fresh(), Repos: []string{"acme/app"}}}
	review.RepositoryBases = map[string]string{"acme/app": "release/app"}
	reviewForm := TriggerFormPresentationMake(review)
	if reviewForm.ShowsBase || reviewForm.RepositoryBases != nil {
		t.Fatalf("review-only form offers branch controls: %+v", reviewForm)
	}
}

// TestRunKeepsTheChoicesItStartedWith: a new run copies the belt's overrides, so
// later belt edits and the run's own copy never reach each other.
func TestRunKeepsTheChoicesItStartedWith(t *testing.T) {
	t.Parallel()
	belt := repositoryBasesTestBelt()
	run := NewRun(RunArgs{Belt: belt, Subject: DefaultSubject("acme/app", "T"), Now: testT0})
	belt.RepositoryBases["acme/app"] = "changed"
	delete(belt.RepositoryBases, "acme/web")
	if run.RepositoryBases["acme/app"] != "release/app" || run.RepositoryBases["acme/web"] != "release/web" {
		t.Fatalf("run choices changed with the belt: %v", run.RepositoryBases)
	}
	run.RepositoryBases["acme/app"] = "run-only"
	if belt.RepositoryBases["acme/app"] != "changed" {
		t.Fatal("editing the run's copy changed the belt")
	}
	data, err := jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var back Run
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.RepositoryBases["acme/app"] != "run-only" || back.RepositoryBases["acme/web"] != "release/web" {
		t.Fatalf("saved and reloaded run = %v", back.RepositoryBases)
	}
}

// TestRunsSavedWithoutOverridesKeepTheirBase: an old run without the key reads
// its shared base and no overrides.
func TestRunsSavedWithoutOverridesKeepTheirBase(t *testing.T) {
	t.Parallel()
	run := NewRun(RunArgs{Belt: repositoryBasesTestBelt(), Subject: DefaultSubject("acme/app", "T"), Now: testT0})
	run.RepositoryBases = nil
	data, err := jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "repositoryBases") {
		t.Fatalf("a run without overrides wrote the key: %s", data)
	}
	var back Run
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.RequestedBase("acme/app"); got != nil {
		t.Fatalf("requested = %v, want the default (the belt had no shared base)", *got)
	}
}

// TestALegacyRunKeepsItsBaseWhenItsLabelsChange: a run saved without overrides
// and without the base-from-label flag asks for the base it saved, as it did
// before overrides existed, even when a base: label is added to its subject later.
func TestALegacyRunKeepsItsBaseWhenItsLabelsChange(t *testing.T) {
	t.Parallel()
	belt := repositoryBasesTestBelt()
	belt.Base = "develop"
	run := NewRun(RunArgs{Belt: belt, Subject: DefaultSubject("acme/app", "T"), Now: testT0})
	run.RepositoryBases = nil
	run.BaseFromLabel = nil
	data, err := jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "baseFromLabel") || strings.Contains(string(data), "repositoryBases") {
		t.Fatalf("a run without a label or overrides wrote a new key: %s", data)
	}
	var back Run
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	back.Subject.Labels = []string{"base:release"}
	if got := back.RequestedBase("acme/app"); got == nil || *got != "develop" {
		t.Fatalf("requested = %v, want the saved develop", got)
	}
}

// TestALabelAddedAfterAdmissionDoesNotChangeTheRun: a run admitted without a
// base: label asks for the shared base in each repository, whatever labels its
// subject gets later; a run admitted with a label keeps that label.
func TestALabelAddedAfterAdmissionDoesNotChangeTheRun(t *testing.T) {
	t.Parallel()
	belt := repositoryBasesTestBelt()
	belt.Base = "develop"
	run := NewRun(RunArgs{Belt: belt, Subject: DefaultSubject("acme/app", "T"), Now: testT0})
	run.Subject.Labels = []string{"base:release"}
	if got := run.RequestedBase("acme/app"); got == nil || *got != "release/app" {
		t.Fatalf("requested = %v, want the app override release/app", got)
	}
	if got := run.RequestedBase("acme/web"); got == nil || *got != "release/web" {
		t.Fatalf("requested = %v, want the web override release/web", got)
	}
	labelled := DefaultSubject("acme/app", "T")
	labelled.Labels = []string{"base:feature/epic"}
	withLabel := NewRun(RunArgs{Belt: belt, Subject: labelled, Now: testT0})
	withLabel.Subject.Labels = nil
	if got := withLabel.RequestedBase("acme/web"); got == nil || *got != "feature/epic" {
		t.Fatalf("requested = %v, want the label feature/epic it started with", got)
	}
}
