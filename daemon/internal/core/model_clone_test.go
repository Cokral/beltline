// Tests of value semantics: a Clone must share no storage
// with its original, including arrays and dictionaries.

package core

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// cloneTestOutcome is an outcome with a goto, a route and a repeat limit.
func cloneTestOutcome() Outcome {
	limit := 2
	return Outcome{ID: "done", Name: "Done", When: "done", Tone: ToneGood, Then: Then{Action: ThenActionGoto, To: []string{"verify"}, Routes: []string{"a"}, Max: &limit}}
}

// cloneTestStage is an agent stage with a pinned agent, a thinking level, a route and alternates.
func cloneTestStage() Stage {
	backend := BackendPi
	thinking := ThinkingHigh
	route := "a"
	return NewStage(StageArgs{
		ID: "build", Type: StageTypeAgent, Agent: &AgentRef{ID: "build", Version: 1},
		Outcomes: []Outcome{cloneTestOutcome()}, Thinking: &thinking, Route: &route,
		Alternates: []SessionSettings{{Runner: &backend, Model: "m"}},
	})
}

// cloneTestFork is a fork with a line route and a feeding route, placed at stage 0.
func cloneTestFork() Fork {
	parent := "a"
	at := 0
	return Fork{ID: "fork", Routes: []Route{{ID: "a", Name: "A"}, {ID: "b", Name: "Feed in", Feeds: true}}, Parent: &parent, At: &at}
}

// cloneTestTrigger is an issues trigger with the default filter.
func cloneTestTrigger() Trigger {
	filter := DefaultIssueFilter()
	filter.Label = "bug"
	return Trigger{Kind: TriggerKindIssues, Issues: &filter}
}

// cloneTestEntry is a trigger entry with one repository.
func cloneTestEntry() TriggerEntry {
	return TriggerEntry{ID: "1", Trigger: cloneTestTrigger(), Repos: []string{"acme/app"}}
}

// cloneTestBelt is a belt with a trigger entry, a stage with an outcome, a watch stage and a fork.
func cloneTestBelt() Belt {
	watch := NewStage(StageArgs{ID: "merge", Type: StageTypeWatch, Outcomes: []Outcome{cloneTestOutcome()}})
	belt := DefaultBelt("b", "B", []TriggerEntry{cloneTestEntry()}, []Stage{cloneTestStage(), watch})
	belt.Forks = []Fork{cloneTestFork()}
	return belt
}

// cloneTestAdmission is an admission with a model list, a legacy repair and a legacy setting.
func cloneTestAdmission() ModelAdmission {
	ctx := 8000
	return NewModelAdmission(
		map[string][]ModelChoice{"pi": {{ID: "m", Variants: map[string]string{"x": "y"}, ContextTokens: &ctx}}},
		map[string]struct{}{"build": {}},
		map[string]SessionSettings{"build": {Model: "m"}},
	)
}

// cloneTestAttempt is a started attempt with settings.
func cloneTestAttempt() Attempt {
	attempt := NewAttempt(AttemptArgs{StageID: "build", Number: 1, StartedAt: jsonx.Date{Time: testT0}, Folder: "build"})
	attempt.Settings = &SessionSettings{Model: "m"}
	return attempt
}

// cloneTestRun is a queued run of cloneTestBelt, with attempts, routes, labels and observations set.
func cloneTestRun() Run {
	run := NewRun(RunArgs{Belt: cloneTestBelt(), Subject: Subject{Kind: SubjectKindIssue, Repo: "acme/app", Number: 1, Labels: []string{"bug"}}, Now: testT0})
	run.Attempts = []Attempt{cloneTestAttempt()}
	run.SetRoutes(map[string][]string{"fork": {"a", "b"}})
	run.WatchObservations = map[string]WatchObservation{"pr": {Approvals: map[string]struct{}{"me": {}}, ReviewAuthors: []string{"r"}}}
	return run
}

// assertNoSharedStorage fails for every slice, map or pointer reachable from a
// through exported fields whose backing storage b also reaches. Zero-length
// slices and zero-sized pointees are skipped: Go may share their storage.
func assertNoSharedStorage(t *testing.T, path string, a, b reflect.Value) {
	t.Helper()
	switch a.Kind() {
	case reflect.Slice:
		if a.IsNil() || a.Len() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s shares its slice with the original", path)
			return
		}
		for i := range min(a.Len(), b.Len()) {
			assertNoSharedStorage(t, fmt.Sprintf("%s[%d]", path, i), a.Index(i), b.Index(i))
		}
	case reflect.Map:
		if a.IsNil() {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s shares its map with the original", path)
			return
		}
		for _, key := range a.MapKeys() {
			assertNoSharedStorage(t, fmt.Sprintf("%s[%v]", path, key), a.MapIndex(key), b.MapIndex(key))
		}
	case reflect.Pointer:
		if a.IsNil() || a.Type().Elem().Size() == 0 {
			return
		}
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s shares its pointer with the original", path)
			return
		}
		assertNoSharedStorage(t, "*"+path, a.Elem(), b.Elem())
	case reflect.Struct:
		for i := range a.NumField() {
			if a.Type().Field(i).IsExported() {
				assertNoSharedStorage(t, path+"."+a.Type().Field(i).Name, a.Field(i), b.Field(i))
			}
		}
	}
}

// checkClone checks that clone(value) equals value and shares no storage with it.
func checkClone[T any](t *testing.T, name string, value T, clone func(T) T) {
	t.Helper()
	copied := clone(value)
	if d := testkit.Diff(value, copied); d != "" {
		t.Errorf("%s: clone differs:\n%s", name, d)
	}
	assertNoSharedStorage(t, name, reflect.ValueOf(value), reflect.ValueOf(copied))
}

// TestStageCloneSharesNothing checks Stage.Clone is a deep copy, including its unexported savedWrites.
func TestStageCloneSharesNothing(t *testing.T) {
	t.Parallel()
	stage := cloneTestStage()
	stage.SetWrites("notes.md")
	checkClone(t, "stage", stage, Stage.Clone)
	copied := stage.Clone()
	copied.Outcomes[0].Then.To[0] = "changed"
	if stage.Outcomes[0].Then.To[0] != "verify" {
		t.Error("changing a cloned stage's outcome changed the stage")
	}
}

// TestForkCloneSharesNothing checks Fork.Clone is a deep copy.
func TestForkCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "fork", cloneTestFork(), Fork.Clone)
}

// TestRouteCloneSharesNothing checks Route.Clone returns the route.
func TestRouteCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "route", Route{ID: "a", Name: "A", Feeds: true}, Route.Clone)
}

// TestTriggerCloneSharesNothing checks Trigger.Clone copies the kind payload it points to.
func TestTriggerCloneSharesNothing(t *testing.T) {
	t.Parallel()
	trigger := cloneTestTrigger()
	checkClone(t, "trigger", trigger, Trigger.Clone)
	copied := trigger.Clone()
	copied.Issues.Label = "changed"
	if trigger.Issues.Label != "bug" {
		t.Error("changing a cloned trigger changed the trigger")
	}
}

// TestTriggerEntryCloneSharesNothing checks TriggerEntry.Clone copies its repositories and trigger.
func TestTriggerEntryCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "trigger entry", cloneTestEntry(), TriggerEntry.Clone)
}

// TestBeltCloneSharesNothing checks Belt.Clone deep-copies every trigger entry, stage and fork.
func TestBeltCloneSharesNothing(t *testing.T) {
	t.Parallel()
	belt := cloneTestBelt()
	checkClone(t, "belt", belt, Belt.Clone)
	copied := belt.Clone()
	copied.Stages[0].Outcomes[0].Name = "changed"
	copied.Triggers[0].Repos[0] = "other/repo"
	if belt.Stages[0].Outcomes[0].Name != "Done" || belt.Triggers[0].Repos[0] != "acme/app" {
		t.Error("changing a cloned belt changed the belt")
	}
}

// TestOutcomeCloneSharesNothing checks Outcome.Clone copies its then.
func TestOutcomeCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "outcome", cloneTestOutcome(), Outcome.Clone)
}

// TestThenCloneSharesNothing checks Then.Clone copies its goto targets, routes and repeat limit.
func TestThenCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "then", cloneTestOutcome().Then, Then.Clone)
}

// TestModelAdmissionCloneSharesNothing checks ModelAdmission.Clone copies its maps and lists.
func TestModelAdmissionCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "admission", cloneTestAdmission(), ModelAdmission.Clone)
}

// TestRouteSetCloneSharesNothing checks RouteSet.Clone copies its map and each list.
func TestRouteSetCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "route set", RouteSet{Routes: map[string][]string{"fork": {"a", "b"}}}, RouteSet.Clone)
}

// TestRunCloneSharesNothing checks Run.Clone copies every list, map and nested value of a run.
func TestRunCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "run", cloneTestRun(), Run.Clone)
}

// TestAttemptCloneSharesNothing checks Attempt.Clone copies its settings.
func TestAttemptCloneSharesNothing(t *testing.T) {
	t.Parallel()
	checkClone(t, "attempt", cloneTestAttempt(), Attempt.Clone)
}

// TestSubjectCloneSharesNothing checks Subject.Clone copies its label, assignee and blocker lists.
func TestSubjectCloneSharesNothing(t *testing.T) {
	t.Parallel()
	subject := Subject{Kind: SubjectKindIssue, Repo: "a/b", Number: 1, Labels: []string{"x"}, Assignees: []string{"me"}, Blockers: []string{"#2"}}
	checkClone(t, "subject", subject, Subject.Clone)
}

// TestTriggerStashCloneSharesNothing checks TriggerStash.Clone copies its trigger map.
func TestTriggerStashCloneSharesNothing(t *testing.T) {
	t.Parallel()
	stash := TriggerStash{Entry: "1", Triggers: map[string]Trigger{"issues": cloneTestTrigger()}}
	checkClone(t, "stash", stash, TriggerStash.Clone)
}

// TestNewRunDoesNotShareTheBeltsStages checks that changing a run's stage outcome leaves the belt it came from unchanged.
func TestNewRunDoesNotShareTheBeltsStages(t *testing.T) {
	t.Parallel()
	belt := cloneTestBelt()
	run := NewRun(RunArgs{Belt: belt, Subject: Subject{Kind: SubjectKindIssue, Repo: "a/b", Number: 1}, Now: testT0})
	run.Stages[0].Outcomes[0].Then.To[0] = "changed"
	run.Forks[0].Routes[0].Name = "changed"
	run.Repos[0] = "other/repo"
	if d := testkit.Diff(cloneTestBelt(), belt); d != "" {
		t.Errorf("the belt changed:\n%s", d)
	}
}

// TestRepairingACopiedAdmissionLeavesTheOriginal checks that a copied admission can be repaired without changing the original.
func TestRepairingACopiedAdmissionLeavesTheOriginal(t *testing.T) {
	t.Parallel()
	original := cloneTestAdmission()
	repaired := original.Clone()
	repaired.Models["pi"][0].ID = "repaired"
	repaired.Models["pi"][0].Variants["x"] = "repaired"
	delete(repaired.LegacyRepairs, "build")
	if original.Models["pi"][0].ID != "m" || original.Models["pi"][0].Variants["x"] != "y" || len(original.LegacyRepairs) != 1 {
		t.Errorf("repairing the copy changed the original: %+v", original)
	}
	// NewModelAdmission takes the caller's maps, which the caller may then change.
	models := map[string][]ModelChoice{"pi": {{ID: "m"}}}
	admission := NewModelAdmission(models, map[string]struct{}{}, nil)
	models["pi"][0].ID = "changed"
	if admission.Models["pi"][0].ID != "m" {
		t.Error("changing the caller's models changed the admission")
	}
}

// TestRunRoutesIsACopy checks that changing the map Run.Routes returns leaves the run's routes unchanged.
func TestRunRoutesIsACopy(t *testing.T) {
	t.Parallel()
	run := cloneTestRun()
	routes := run.Routes()
	routes["fork"][0] = "changed"
	routes["other"] = []string{"x"}
	if got := run.Routes()["fork"]; len(got) != 2 || got[0] != "a" || len(run.Routes()) != 1 {
		t.Errorf("changing Routes() changed the run: %v", run.Routes())
	}
	routes = map[string][]string{"fork": {"a"}}
	run.SetRoutes(routes)
	routes["fork"][0] = "changed"
	if run.Routes()["fork"][0] != "a" {
		t.Error("changing the map given to SetRoutes changed the run")
	}
}

// TestBeltAccessorsHandOutCopies checks that the stage, fork and outcome a belt, layout or stage hands out are copies.
func TestBeltAccessorsHandOutCopies(t *testing.T) {
	t.Parallel()
	belt := cloneTestBelt()
	if stage := belt.Stage("build"); stage != nil {
		stage.Outcomes[0].Then.To[0] = "changed"
	}
	if fork := belt.Layout().Fork("fork"); fork != nil {
		fork.Routes[0].Name = "changed"
	}
	if outcome := belt.Stages[0].Outcome("done"); outcome != nil {
		outcome.Then.To[0] = "changed"
	}
	if d := testkit.Diff(cloneTestBelt(), belt); d != "" {
		t.Errorf("an accessor changed the belt:\n%s", d)
	}
}

// TestCloneStringsKeepsNilAndCopies checks that cloneStrings keeps nil and returns a copy otherwise.
func TestCloneStringsKeepsNilAndCopies(t *testing.T) {
	t.Parallel()
	if cloneStrings(nil) != nil {
		t.Error("cloneStrings(nil) is not nil")
	}
	original := []string{"a"}
	copied := cloneStrings(original)
	copied[0] = "b"
	if original[0] != "a" {
		t.Error("cloneStrings shares its backing array")
	}
}
