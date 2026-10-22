// Contract tests of run command refusals and protocol: a request with no Host header is refused, and
// opaque run IDs resolve as one exact path segment.

package servertest

import (
	"net/http"
	"slices"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// runCommandOf is the core command each run command endpoint's case names.
var runCommandOf = map[string]core.RunCommand{
	"runDecide": core.RunCommandDecide, "runResume": core.RunCommandResume, "runAnswer": core.RunCommandAnswer,
	"runAbort": core.RunCommandAbort, "runDrop": core.RunCommandDrop, "runPark": core.RunCommandPark,
	"runUnpark": core.RunCommandUnpark, "runPause": core.RunCommandPause, "runResumeRun": core.RunCommandResumeRun,
	"runRetryContinuation": core.RunCommandRetryContinuation, "runStartNow": core.RunCommandStartNow,
	"runContinueSession": core.RunCommandContinueSession, "runAdjustStage": core.RunCommandAdjustStage,
	"runAfk": core.RunCommandAfk, "runErase": core.RunCommandErase, "runOverruleAfk": core.RunCommandOverruleAfk,
}

// postCommandCases are the POST endpoints of the runs area, in registry order (tasks included).
func postCommandCases() []string {
	var cases []string
	for _, endpoint := range routes.EndpointsIn(routes.AreaRuns) {
		if endpoint.Method == routes.EndpointMethodPost {
			cases = append(cases, endpoint.Case)
		}
	}
	return cases
}

// commandRequestBody is the body a command's request sends in the matrix tests:
// nil for a command that takes the empty object.
func commandRequestBody(t *testing.T, contractCase string) []byte {
	t.Helper()
	switch contractCase {
	case "runDecide":
		return mustJSON(t, api.RunDecideRequest{Outcome: "approved"})
	case "runResume":
		return mustJSON(t, api.RunResumeRequest{})
	case "runAnswer":
		return mustJSON(t, api.RunAnswerRequest{Answer: new("yes")})
	case "runContinueSession":
		return mustJSON(t, api.RunAttemptRequest{Attempt: "missing"})
	case "runAdjustStage":
		return mustJSON(t, api.RunAdjustStageRequest{Stage: "triage", Model: "fixture-model"})
	case "runAfk":
		return mustJSON(t, api.RunAfkRequest{Mode: "always"})
	case "runOverruleAfk":
		return mustJSON(t, api.RunOverruleAfkRequest{Decision: "missing", Stage: "triage", Comments: "why", AcceptedWarning: true})
	case "tasks":
		return mustJSON(t, api.RunTaskRequest{Belt: "missing", Repo: "acme/billing", Title: "A task", Body: ""})
	}
	return nil
}

// TestEveryCommandRefusesAnUnavailableRunWithTheSharedReason
func TestEveryCommandRefusesAnUnavailableRunWithTheSharedReason(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	shipped := findRun(t, f.Coordinator, "finished", func(run core.Run) bool { return !run.State.IsActive() })
	decision := core.NewAfkDecision(core.AfkDecisionArgs{
		ID: "contract-shipped", Run: shipped,
		Situation: core.AfkSituation{Kind: core.AfkSituationKindApproval, Key: "contract-shipped"},
		StartedAt: jsonxDate(ContractNow),
	})
	decision.Status = core.AfkDecisionStatusApplied
	f.Coordinator.AddAfkDecisionForContract(decision)
	before := inventory(t, f.Root)

	stage := shipped.StageID
	if stage == "" {
		stage = "triage"
	}
	for _, contractCase := range postCommandCases() {
		if contractCase == "runErase" || contractCase == "tasks" || contractCase == "runAdjustSwitch" {
			continue
		}
		var body []byte
		switch contractCase {
		case "runOverruleAfk":
			body = mustJSON(t, api.RunOverruleAfkRequest{Decision: decision.ID, Stage: stage, Comments: "why", AcceptedWarning: true})
		case "runContinueSession":
			attempt := "missing"
			if len(shipped.Attempts) > 0 {
				attempt = shipped.Attempts[0].ID
			}
			body = mustJSON(t, api.RunAttemptRequest{Attempt: attempt})
		default:
			body = commandRequestBody(t, contractCase)
		}
		reply := postRunCase(f, casePath(contractCase, shipped.ID, ""), body)
		if reply.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: expected a refusal, got %d %s", contractCase, reply.Status, reply.Text())
		}
		context := f.Coordinator.RunActionContext(*f.Coordinator.ReadRun(shipped.ID))
		var expected *string
		if contractCase == "runOverruleAfk" {
			expected = core.RunAvailabilityRefusal(core.RunCommandOverruleAfk, context,
				core.RunActionTarget{Stage: new(stage), Decision: new(decision.ID)})
		} else {
			expected = core.RunAvailabilityRefusal(runCommandOf[contractCase], context, core.RunActionTarget{})
		}
		if expected == nil || errorMessage(t, reply) != *expected {
			t.Fatalf("%s: message %q, want the shared refusal %v", contractCase, errorMessage(t, reply), expected)
		}
	}

	active := findRun(t, f.Coordinator, "active", func(run core.Run) bool { return run.State.IsActive() })
	erase := postRunCase(f, casePath("runErase", active.ID, ""), nil)
	if erase.Status != http.StatusUnprocessableEntity {
		t.Fatalf("erasing an active run: HTTP %d, want 422", erase.Status)
	}
	activeContext := f.Coordinator.RunActionContext(*f.Coordinator.ReadRun(active.ID))
	want := core.RunAvailabilityRefusal(core.RunCommandErase, activeContext, core.RunActionTarget{})
	if want == nil || errorMessage(t, erase) != *want || *want != "Drop the run before erasing it." {
		t.Fatalf("erase message %q, want %v", errorMessage(t, erase), want)
	}
	if !sameInventory(inventory(t, f.Root), before) {
		t.Fatal("refused commands changed the data folder")
	}
	found := slices.ContainsFunc(f.Coordinator.ReadRuns(), func(run core.Run) bool {
		return run.ID == shipped.ID
	})
	if !found {
		t.Fatal("the shipped run is gone")
	}
}

// TestStaleEnabledReadsAreRefusedAtSubmission
func TestStaleEnabledReadsAreRefusedAtSubmission(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	listed := getCase(f, casePath("runs", "", ""))
	var list api.RunsResponse
	if err := routes.JSONDecode(listed.Body, &list); err != nil {
		t.Fatalf("runs: %v", err)
	}
	var snapshot *api.RunSnapshot
	for i := range list.Runs {
		if list.Runs[i].Run.ID == run.ID {
			snapshot = &list.Runs[i]
		}
	}
	if snapshot == nil {
		t.Fatal("the queued run is not listed")
	}
	enabled := slices.ContainsFunc(snapshot.Actions, func(action core.RunAction) bool {
		return action.ID == string(core.RunCommandStartNow) && action.IsUnscoped() && action.Enabled
	})
	if !enabled {
		t.Fatal("start-now is not listed as enabled")
	}

	path := casePath("runStartNow", run.ID, "")
	if first := postRunCase(f, path, nil); first.Status != http.StatusOK {
		t.Fatalf("first start-now: HTTP %d", first.Status)
	}
	second := postRunCase(f, path, nil)
	if second.Status != http.StatusUnprocessableEntity {
		t.Fatalf("second start-now: HTTP %d, want 422", second.Status)
	}
	after := *f.Coordinator.ReadRun(run.ID)
	want := core.RunAvailabilityRefusal(core.RunCommandStartNow, f.Coordinator.RunActionContext(after), core.RunActionTarget{})
	if want == nil || errorMessage(t, second) != *want {
		t.Fatalf("message %q, want %v", errorMessage(t, second), want)
	}
	if after.State != core.RunStateRunning {
		t.Fatalf("state %s, want running", after.State)
	}
	count := 0
	for _, attempt := range after.Attempts {
		if attempt.StageID == run.StageID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d attempts at the stage, want 1", count)
	}
}

// TestMalformedBodiesWrongMethodsAndUnknownCommandsTouchesNothing
func TestMalformedBodiesWrongMethodsAndUnknownCommandsTouchesNothing(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "active", func(run core.Run) bool { return run.State.IsActive() })
	before := inventory(t, f.Root)
	decidePath := casePath("runDecide", run.ID, "")

	if malformed := postRunCase(f, decidePath, []byte("not json")); malformed.Status != http.StatusBadRequest {
		t.Fatalf("malformed body: HTTP %d, want 400", malformed.Status)
	}
	if missing := postRunCase(f, decidePath, []byte{}); missing.Status != http.StatusBadRequest {
		t.Fatalf("missing body: HTTP %d, want 400", missing.Status)
	}
	if wrong := getCase(f, decidePath); wrong.Status == http.StatusOK {
		t.Fatal("a GET on a command path answered 200")
	}
	if unknown := postRunCase(f, casePath("runDecide", run.ID, "")+"/not-a-command", nil); unknown.Status != http.StatusNotFound {
		t.Fatalf("unknown command: HTTP %d, want 404", unknown.Status)
	}
	missingRun := postRunCase(f, "/api/v1/runs/not-a-run/decide", mustJSON(t, api.RunDecideRequest{Outcome: "approved"}))
	if missingRun.Status != http.StatusNotFound {
		t.Fatalf("missing run: HTTP %d, want 404", missingRun.Status)
	}
	CheckBody(t, "missing run", "error-not-found.json", missingRun.Body, f.Root)

	if afk := postRunCase(f, casePath("runAfk", run.ID, ""), []byte(`{"mode":"sometimes"}`)); afk.Status != http.StatusUnprocessableEntity || errorMessage(t, afk) != "mode must be always, never or global." {
		t.Fatalf("invalid mode: HTTP %d %q", afk.Status, errorMessage(t, afk))
	}
	if thinking := postRunCase(f, casePath("runAdjustStage", run.ID, ""), []byte(`{"stage":"triage","model":"fixture-model","thinking":"extreme"}`)); thinking.Status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid thinking: HTTP %d, want 422", thinking.Status)
	}
	if missingField := postRunCase(f, casePath("runAdjustStage", run.ID, ""), []byte(`{"stage":"triage"}`)); missingField.Status != http.StatusBadRequest {
		t.Fatalf("missing field: HTTP %d, want 400", missingField.Status)
	}
	if outcome := postRunCase(f, decidePath, mustJSON(t, api.RunDecideRequest{Outcome: "not-an-outcome"})); outcome.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad outcome: HTTP %d, want 422", outcome.Status)
	}
	if stage := postRunCase(f, casePath("runResume", run.ID, ""), mustJSON(t, api.RunResumeRequest{Stage: new("nope")})); stage.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad stage: HTTP %d, want 422", stage.Status)
	}
	if attempt := postRunCase(f, casePath("runContinueSession", run.ID, ""), mustJSON(t, api.RunAttemptRequest{Attempt: "nope"})); attempt.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad attempt: HTTP %d, want 422", attempt.Status)
	}
	if leg := postRunCase(f, casePath("runAbort", run.ID, ""), mustJSON(t, api.RunLegRequest{Leg: new("nope")})); leg.Status != http.StatusUnprocessableEntity {
		t.Fatalf("bad leg: HTTP %d, want 422", leg.Status)
	}
	if !sameInventory(inventory(t, f.Root), before) {
		t.Fatal("malformed commands changed the data folder")
	}
}

// TestPostMatrixChecksAuthHostAndOriginBeforeLookupAndWritesNothing
func TestPostMatrixChecksAuthHostAndOriginBeforeLookupAndWritesNothing(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "active", func(run core.Run) bool { return run.State.IsActive() })
	before := inventory(t, f.Root)
	for _, contractCase := range postCommandCases() {
		path := casePath(contractCase, run.ID, "")
		if contractCase == "tasks" {
			path = casePath("tasks", "", "")
		}
		body := commandRequestBody(t, contractCase)
		for _, reply := range []Reply{
			f.Do(Request{Method: "POST", Path: path, Body: body}),
			f.Do(Request{Method: "POST", Path: path, Body: body, Token: "wrong"}),
			f.Do(Request{Method: "POST", Path: path, Body: body, Header: http.Header{"Cookie": {"beltline_auth=wrong"}}}),
		} {
			if reply.Status != http.StatusUnauthorized {
				t.Fatalf("%s: unauthenticated HTTP %d, want 401", contractCase, reply.Status)
			}
			CheckBody(t, contractCase, "error-unauthorized.json", reply.Body, f.Root)
		}
		foreign := http.Header{"Origin": {"https://foreign.invalid"}}
		for _, token := range []string{ContractToken, ""} {
			reply := f.Do(Request{Method: "POST", Path: path, Body: body, Header: foreign, Token: token})
			if reply.Status != http.StatusForbidden {
				t.Fatalf("%s: foreign origin HTTP %d, want 403", contractCase, reply.Status)
			}
			CheckBody(t, contractCase, "error-forbidden.json", reply.Body, f.Root)
		}
		wrongHost := f.DoRaw(t, "POST "+path+" HTTP/1.1\r\nHost: localhost:40123\r\nAuthorization: Bearer "+ContractToken+"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		if wrongHost.Status != http.StatusForbidden {
			t.Fatalf("%s: wrong host HTTP %d, want 403", contractCase, wrongHost.Status)
		}
		// A request with no Host header is refused by the middleware, as the contract requires (403). Over a
		// listener net/http answers 400 first (the recorded deviation; the listener tests check it).
		missingHost := f.DoRaw(t, "POST "+path+" HTTP/1.1\r\nAuthorization: Bearer "+ContractToken+"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		if missingHost.Status != http.StatusForbidden {
			t.Fatalf("%s: missing host HTTP %d, want 403", contractCase, missingHost.Status)
		}
	}
	if !sameInventory(inventory(t, f.Root), before) {
		t.Fatal("refused requests changed the data folder")
	}
}

// TestDecideRefusesABlankNoteForAnOutcomeThatAsksForOutput
func TestDecideRefusesABlankNoteForAnOutcomeThatAsksForOutput(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	var chosen *core.Run
	for _, run := range f.Coordinator.ReadRuns() {
		if run.State != core.RunStateWaiting || stageKind(run) != core.StageKindGate {
			continue
		}
		for _, outcome := range run.Stage().Outcomes {
			if outcome.Then.WithOutput {
				run := run
				chosen = &run
				break
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		t.Fatal("the contract demo has no waiting gate with an outcome that asks for output")
	}
	var asking core.Outcome
	for _, outcome := range chosen.Stage().Outcomes {
		if outcome.Then.WithOutput {
			asking = outcome
			break
		}
	}

	blank := postRunCase(f, casePath("runDecide", chosen.ID, ""), mustJSON(t, api.RunDecideRequest{Outcome: asking.Name, Note: new("   ")}))
	if blank.Status != http.StatusUnprocessableEntity {
		t.Fatalf("blank note: HTTP %d, want 422", blank.Status)
	}
	if got := errorMessage(t, blank); got != core.RunAvailabilityNoteRequired {
		t.Fatalf("message %q, want the note requirement", got)
	}
	if after := f.Coordinator.ReadRun(chosen.ID); after == nil || after.State != core.RunStateWaiting {
		t.Fatal("a refused decision changed the run")
	}

	withNote := postRunCase(f, casePath("runDecide", chosen.ID, ""), mustJSON(t, api.RunDecideRequest{Outcome: asking.Name, Note: new("Because.")}))
	if withNote.Status != http.StatusOK {
		t.Fatalf("with a note: HTTP %d, want 200", withNote.Status)
	}
	if after := f.Coordinator.ReadRun(chosen.ID); after == nil || after.State == core.RunStateWaiting {
		t.Fatal("the decided run is still waiting")
	}
}

// TestAfkAcceptsEveryModeAndRefusesAFinishedRun
func TestAfkAcceptsEveryModeAndRefusesAFinishedRun(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "active", func(run core.Run) bool { return run.State.IsActive() })
	for _, mode := range core.AllAfkMode {
		reply := postRunCase(f, casePath("runAfk", run.ID, ""), mustJSON(t, api.RunAfkRequest{Mode: string(mode)}))
		if reply.Status != http.StatusOK {
			t.Fatalf("mode %s: HTTP %d, want 200", mode, reply.Status)
		}
		if got := decodeRunReply(t, reply).Afk(); got != mode {
			t.Fatalf("mode %s: saved %s", mode, got)
		}
	}
	terminal := findRun(t, f.Coordinator, "finished", func(run core.Run) bool { return !run.State.IsActive() })
	refused := postRunCase(f, casePath("runAfk", terminal.ID, ""), mustJSON(t, api.RunAfkRequest{Mode: string(core.AfkModeNever)}))
	if refused.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a finished run: HTTP %d, want 422", refused.Status)
	}
	want := core.RunAvailabilityRefusal(core.RunCommandAfk, f.Coordinator.RunActionContext(*f.Coordinator.ReadRun(terminal.ID)), core.RunActionTarget{})
	if want == nil || errorMessage(t, refused) != *want {
		t.Fatalf("message %q, want %v", errorMessage(t, refused), want)
	}
}

// TestPercentEncodedOpaqueRunIDsResolveAsOneExactPathSegment checks that runs whose ids hold slashes,
// hashes, colons and spaces resolve as one path segment, for reads and commands.
func TestPercentEncodedOpaqueRunIDsResolveAsOneExactPathSegment(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	belt := opaqueBelt()
	subjects := []core.Subject{
		{Kind: core.SubjectKindIssue, Repo: "team/repo", Number: 42, Title: "Slash and hash"},
		{Kind: core.SubjectKindPullRequest, Repo: "team/repo", Number: 42, Key: "", Title: "Review occurrence", Occurrence: new("review/2 @alice#new")},
		{Kind: core.SubjectKindJira, Repo: "team/repo", Key: "KAN-50/part one#two:@", Title: "Jira ticket"},
		{Kind: core.SubjectKindManual, Repo: "team/repo", Key: "task with spaces/#:@", Title: "Manual task"},
		{Kind: core.SubjectKindScheduled, Repo: "team/repo", Key: "schedule/2026-10-07#09:30 @", Title: "Scheduled task"},
	}
	for _, subject := range subjects {
		f.Coordinator.AppendRunForContract(core.NewRun(core.RunArgs{Belt: belt, Subject: subject, Now: ContractNow}))
	}
	for _, subject := range subjects {
		var run *core.Run
		for _, candidate := range f.Coordinator.ReadRuns() {
			if candidate.Subject.Ref() == subject.Ref() {
				candidate := candidate
				run = &candidate
				break
			}
		}
		if run == nil {
			t.Fatalf("no run for %s", subject.Ref())
		}
		reply := getCase(f, casePath("run", run.ID, ""))
		if reply.Status != http.StatusOK {
			t.Fatalf("run %s: HTTP %d", run.ID, reply.Status)
		}
		if got := decodeRunReply(t, reply); got.ID != run.ID {
			t.Fatalf("run id %s, want %s", got.ID, run.ID)
		}
		if got := f.Coordinator.ReadRun(run.ID); got == nil || !sameRun(*got, *run) {
			t.Fatalf("the saved run %s differs from the one read", run.ID)
		}
		// A command addresses the same run by the same segment.
		afk := postRunCase(f, casePath("runAfk", run.ID, ""), mustJSON(t, api.RunAfkRequest{Mode: "never"}))
		if afk.Status != http.StatusOK || decodeRunReply(t, afk).ID != run.ID {
			t.Fatalf("afk on %s: HTTP %d %s", run.ID, afk.Status, afk.Text())
		}
	}
}

// opaqueBelt is the belt the opaque-id tests run on: a manual belt with one agent stage.
func opaqueBelt() core.Belt {
	stage := core.NewStage(core.StageArgs{ID: "triage", Type: core.StageTypeAgent, Outcomes: []core.Outcome{}})
	return core.Belt{
		ID: "opaque", Name: "Opaque ids", Version: 1, Policies: core.DefaultPolicies(),
		Triggers: []core.TriggerEntry{{ID: "1", Trigger: core.Trigger{Kind: core.TriggerKindManual}, Repos: []string{"team/repo"}}},
		Stages:   []core.Stage{stage},
	}
}
