// Contract tests of the belt writes: every draft operation, the belt library list, and the empty-
// collection goldens.

package servertest

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// TestEveryDraftOperationReturnsItsDraftAndLeavesEveryFileAlone checks each draft operation's reply shape and that no file changes.
func TestEveryDraftOperationReturnsItsDraftAndLeavesEveryFileAlone(t *testing.T) {
	g := Start(t, Options{})
	before := contractInventory(t, g.Root)
	for _, draft := range ContractDraftRequests() {
		reply := postJSON(g, draft.Path, contractBody(t, draft.Request))
		CheckResponse(t, reply, ExpectJSON)
		var response api.BeltDraftResponse
		if err := decodeContract(reply.Body, &response); err != nil {
			t.Fatalf("%s: %v", draft.Case, err)
		}
		if response.Draft.ID != draft.Request.Draft.ID || response.Draft.Version != 0 {
			t.Fatalf("%s: the draft's id or version changed", draft.Case)
		}
		if reflect.DeepEqual(response.Draft, draft.Request.Draft) {
			t.Fatalf("%s should change the draft", draft.Case)
		}
		if draft.Case == "beltsDraftStageUpdate" && response.Placeholders == nil {
			t.Fatalf("%s: no placeholder help", draft.Case)
		}
	}
	requireSameInventory(t, before, g.Root)
}

// beltContextDescribesTheDraftAndMatchesItsGolden is WriteContractTests.beltContextDescribes….
func TestBeltContextDescribesTheDraftAndMatchesItsGolden(t *testing.T) {
	g := Start(t, Options{})
	reply := postJSON(g, contractEndpoint(t, "beltContext").Path,
		contractBody(t, api.NewBeltContextRequest(ContractDraft(), new("check"))))
	requireStatus(t, reply, 200)
	tree := contractTree(t, reply.Body)
	var ids []string
	for _, stage := range contractMember(t, tree, "stages").([]any) {
		ids = append(ids, contractMember(t, stage, "id").(string))
	}
	if !slices.Contains(ids, "check") {
		t.Fatalf("context stages %v lack check", ids)
	}
	contractMember(t, tree, "placeholders")
	var keys []string
	for _, policy := range contractMember(t, tree, "policies").([]any) {
		keys = append(keys, contractMember(t, policy, "key").(string))
	}
	if !reflect.DeepEqual(keys, []string{"retriageOnEdit", "stopOnClose", "commentOnHold", "waitForBlockers"}) {
		t.Fatalf("policy keys %v", keys)
	}
	CheckBody(t, "beltContext", contractEndpoint(t, "beltContext").GoldenName, reply.Body, g.Root)
}

// aRejectedDraftEditNamesTheStageItIsAbout is WriteContractTests.aRejectedDraftEditNamesTheStageItIsAbout.
func TestARejectedDraftEditNamesTheStageItIsAbout(t *testing.T) {
	g := Start(t, Options{})
	request := core.BeltDraftRequest{Draft: ContractDraft(), Op: core.BeltDraftOperationStageRemove, StageID: new("missing")}
	reply := postJSON(g, contractEndpoint(t, "beltsDraftStageRemove").Path, contractBody(t, request))
	requireProblems(t, reply, []string{"stage-remove needs a stage id that is on the belt."})
	tree := contractTree(t, reply.Body)
	locations := contractMember(t, tree, "locations").([]any)
	if len(locations) != 1 || contractMember(t, locations[0], "kind") != "stage" || contractMember(t, locations[0], "id") != "missing" {
		t.Fatalf("locations %v, want the stage missing", locations)
	}
}

// twoDraftsForTheSameBeltDoNotOverwriteEachOther is WriteContractTests.twoDraftsForTheSameBelt….
func TestTwoDraftsForTheSameBeltDoNotOverwriteEachOther(t *testing.T) {
	g := Start(t, Options{})
	first := ContractDraft()
	remove := core.BeltDraftRequest{Draft: first, Op: core.BeltDraftOperationStageRemove, StageID: new("check")}
	rename := core.BeltDraftRequest{Draft: first, Op: core.BeltDraftOperationStageUpdate, StageID: new("build"),
		Update: &core.StageEdit{Name: new("Different")}}
	firstReply := postJSON(g, contractEndpoint(t, "beltsDraftStageRemove").Path, contractBody(t, remove))
	secondReply := postJSON(g, contractEndpoint(t, "beltsDraftStageUpdate").Path, contractBody(t, rename))
	var removed, renamed api.BeltDraftResponse
	if err := decodeContract(firstReply.Body, &removed); err != nil {
		t.Fatal(err)
	}
	if err := decodeContract(secondReply.Body, &renamed); err != nil {
		t.Fatal(err)
	}
	if hasStage(removed.Draft, "check") || !hasStageNamed(removed.Draft, "build", "Build") {
		t.Fatal("the removal did not leave the build stage named Build without check")
	}
	if !hasStageNamed(renamed.Draft, "build", "Different") || !hasStage(renamed.Draft, "check") {
		t.Fatal("the rename did not keep check beside the renamed build")
	}
}

// invalidDraftArgumentsAreRejectedWithoutMutation is WriteContractTests.invalidDraftArguments….
func TestInvalidDraftArgumentsAreRejectedWithoutMutation(t *testing.T) {
	g := Start(t, Options{})
	before := contractInventory(t, g.Root)
	cases := []struct {
		name    string
		path    string
		request core.BeltDraftRequest
	}{
		{"beltsDraftStageRemove", "beltsDraftStageRemove", core.BeltDraftRequest{Draft: ContractDraft(), Op: core.BeltDraftOperationStageRemove, StageID: new("missing")}},
		{"beltsDraftStageMove", "beltsDraftStageMove", core.BeltDraftRequest{Draft: ContractDraft(), Op: core.BeltDraftOperationStageMove, StageID: new("build"), Step: new(0)}},
		{"beltsDraftStageAdd", "beltsDraftStageAdd", core.BeltDraftRequest{Draft: ContractDraft(), Op: core.BeltDraftOperationStageAdd, Stage: new(ContractAddedStage()), Route: new("missing")}},
		{"beltsDraftForkRemove", "beltsDraftForkRemove", core.BeltDraftRequest{Draft: ContractDraft(), Op: core.BeltDraftOperationForkRemove, ForkID: new("missing")}},
	}
	for _, c := range cases {
		reply := postJSON(g, contractEndpoint(t, c.path).Path, contractBody(t, c.request))
		requireStatus(t, reply, 422)
		tree := contractTree(t, reply.Body)
		problems, _ := contractMember(t, tree, "problems").([]any)
		if len(problems) == 0 {
			t.Fatalf("%s: no problems", c.name)
		}
		if contractMember(t, tree, "message") != joinProblems(problems) {
			t.Fatalf("%s: message is not the problems joined", c.name)
		}
	}
	requireSameInventory(t, before, g.Root)
}

// unknownDraftOperationsAreNotFoundAndWrongBodiesAreRejected is WriteContractTests.unknownDraftOperations….
func TestUnknownDraftOperationsAreNotFoundAndWrongBodiesAreRejected(t *testing.T) {
	g := Start(t, Options{})
	before := contractInventory(t, g.Root)
	unknown := postJSON(g, "/api/v1/belts/draft/not-an-op", []byte("{}"))
	requireStatus(t, unknown, 404)
	if string(unknown.Body) != string(contractBody(t, api.ErrorNotFound)) {
		t.Fatalf("unknown operation body %s, want the not-found body", unknown.Body)
	}
	path := contractEndpoint(t, "beltsDraftStageRemove").Path
	requireStatus(t, postJSON(g, path, nil), 422)
	requireStatus(t, postJSON(g, path, []byte("not json")), 422)
	wrongOp := core.BeltDraftRequest{Draft: ContractDraft(), Op: core.BeltDraftOperationStageAdd}
	requireStatus(t, postJSON(g, path, contractBody(t, wrongOp)), 422)
	if reply := g.Do(Request{Method: "GET", Path: path, Token: ContractToken}); reply.Status == 200 {
		t.Fatal("a GET reached a draft operation")
	}
	requireSameInventory(t, before, g.Root)
}

// beltCommandsReturnSavedResourcesAndGoldens is WriteContractTests.beltCommandsReturnSavedResourcesAndGoldens.
func TestBeltCommandsReturnSavedResourcesAndGoldens(t *testing.T) {
	seq := contractSequence(t, "belt-commands", beltCommandReplies)
	for _, name := range []string{"beltNew", "beltCopy", "beltSave", "beltArchive", "beltUnarchive", "beltPause",
		"beltUpdateStage", "beltUpdateAllStages", "beltAfk", "beltShadowSave", "beltShadowAccept", "beltShadowDiscard"} {
		checkSequenceReply(t, seq, name)
	}
	var newDraft api.BeltDraftResponse
	mustDecode(t, seq.replies["beltNew"], &newDraft)
	if newDraft.Draft.Version != 0 || len(newDraft.Draft.Stages) != 0 {
		t.Fatal("a new belt is not an empty version 0 draft")
	}
	var copied api.BeltDraftResponse
	mustDecode(t, seq.replies["beltCopy"], &copied)
	if copied.Draft.ID == "contract-draft" || copied.Draft.Version != 0 || copied.Draft.Name != "Copy of Contract draft" {
		t.Fatalf("copy is %s %d %q", copied.Draft.ID, copied.Draft.Version, copied.Draft.Name)
	}
	if len(copied.Draft.Stages) != len(ContractSaveDraft().Stages) {
		t.Fatal("the copy has a different number of stages")
	}
	saved := resourceOf(t, seq.replies["beltSave"])
	savedWarnings, _ := contractMember(t, contractTree(t, seq.replies["beltSave"].Body), "warnings").([]any)
	if contractMember(t, contractMember(t, saved, "draft"), "version") != float64(1) || len(savedWarnings) != 0 {
		t.Fatal("the save did not write version 1 without warnings")
	}
	if contractMember(t, resourceOf(t, seq.replies["beltArchive"]), "archived") != true ||
		contractMember(t, resourceOf(t, seq.replies["beltUnarchive"]), "archived") != false ||
		contractMember(t, resourceOf(t, seq.replies["beltPause"]), "paused") != true {
		t.Fatal("archive, unarchive or pause did not change the belt")
	}
	var updated api.BeltDraftResponse
	mustDecode(t, seq.replies["beltUpdateStage"], &updated)
	if !hasStageVersion(updated.Draft, "build", 2) {
		t.Fatal("update-stage did not move build to its newer version")
	}
	if contractMember(t, contractMember(t, resourceOf(t, seq.replies["beltAfk"]), "draft"), "afk") != string(core.AfkModeAlways) {
		t.Fatal("the AFK command did not set always")
	}
	shadow := contractMember(t, resourceOf(t, seq.replies["beltShadowSave"]), "shadow")
	if contractMember(t, shadow, "test") != "t1" || contractMember(t, shadow, "version") != float64(3) {
		t.Fatalf("the shadow save did not start test t1 on v3: %v", shadow)
	}
	accepted := resourceOf(t, seq.replies["beltShadowAccept"])
	if _, ok := accepted.(map[string]any)["shadow"]; ok || contractMember(t, contractMember(t, accepted, "draft"), "version") != float64(3) {
		t.Fatal("accept did not make the shadow live and end the test")
	}
	discarded := resourceOf(t, seq.replies["beltShadowDiscard"])
	if _, ok := discarded.(map[string]any)["shadow"]; ok || contractMember(t, contractMember(t, discarded, "draft"), "version") != float64(3) {
		t.Fatal("discard changed the live version or kept the test")
	}
}

// aWarnedBeltSaveSucceedsPausedAndAResumeIsRefused is WriteContractTests.aWarnedBeltSave….
func TestAWarnedBeltSaveSucceedsPausedAndAResumeIsRefused(t *testing.T) {
	g := Start(t, Options{})
	warned := ContractDraft()
	warned.Stages = []core.Stage{}
	saveReply := postJSON(g, contractEndpoint(t, "beltSave").Path, contractBody(t, api.BeltSaveRequest{Belt: warned}))
	requireStatus(t, saveReply, 200)
	resource := resourceOf(t, saveReply)
	if contractMember(t, resource, "paused") != true || contractMember(t, resource, "effectivelyPaused") != true {
		t.Fatal("a warned belt is not paused")
	}
	warnings, _ := contractMember(t, contractTree(t, saveReply.Body), "warnings").([]any)
	if !slices.Contains(anyStrings(warnings), "Add at least one stage.") || contractMember(t, contractMember(t, resource, "draft"), "version") != float64(1) {
		t.Fatalf("warnings %v", warnings)
	}
	resume := postJSON(g, "/api/v1/belts/contract-draft/pause", contractBody(t, api.BeltPauseRequest{Paused: false}))
	requireProblems(t, resume, []string{core.BeltAvailabilityResumeBlocked})
	if record := g.Coordinator.ReadBelt("contract-draft"); record == nil || !record.Paused {
		t.Fatal("the refused resume changed the belt")
	}
}

// missingSavedIDsAreNotFound is WriteContractTests.missingSavedIDsAreNotFound (the belt paths).
func TestMissingSavedIDsAreNotFound(t *testing.T) {
	g := Start(t, Options{})
	before := contractInventory(t, g.Root)
	for _, path := range []string{"/api/v1/belts/nope/archive", "/api/v1/belts/nope/unarchive",
		"/api/v1/belts/nope/pause", "/api/v1/belts/nope/afk"} {
		reply := postJSON(g, path, []byte("{}"))
		requireStatus(t, reply, 404)
		if string(reply.Body) != string(contractBody(t, api.ErrorNotFound)) {
			t.Fatalf("%s: not the not-found body", path)
		}
	}
	requireSameInventory(t, before, g.Root)
}

// beltResourcesCarryActionsAndStableRevisions is WriteContractTests.beltResourcesCarryActionsAndStableRevisions.
func TestBeltResourcesCarryActionsAndStableRevisions(t *testing.T) {
	g := Start(t, Options{})
	list := contractTree(t, getCase(g, "/api/v1/belts").Body)
	rows := contractMember(t, list, "belts").([]any)
	first := rows[0]
	if len(contractMember(t, first, "actions").([]any)) == 0 || contractMember(t, first, "revision") == "" {
		t.Fatal("the first belt has no actions or no revision")
	}
	id := contractMember(t, contractMember(t, first, "draft"), "id").(string)
	detail := contractTree(t, getCase(g, "/api/v1/belts/"+id).Body)
	if !reflect.DeepEqual(contractMember(t, detail, "actions"), contractMember(t, first, "actions")) ||
		contractMember(t, detail, "revision") != contractMember(t, first, "revision") {
		t.Fatal("the detail's actions or revision differ from the list's")
	}
	again := contractTree(t, getCase(g, "/api/v1/belts/"+id).Body)
	if contractMember(t, again, "revision") != contractMember(t, first, "revision") {
		t.Fatal("the revision changed without a change")
	}
	var live any
	for _, row := range rows {
		if contractMember(t, row, "archived") == false && contractMember(t, row, "paused") == false {
			live = row
			break
		}
	}
	if live == nil {
		t.Fatal("the demo has no live belt")
	}
	liveID := contractMember(t, contractMember(t, live, "draft"), "id").(string)
	pause := contractTree(t, postJSON(g, "/api/v1/belts/"+liveID+"/pause", contractBody(t, api.BeltPauseRequest{Paused: true})).Body)
	resource := contractMember(t, pause, "resource")
	if reflect.DeepEqual(contractMember(t, resource, "revision"), contractMember(t, live, "revision")) ||
		reflect.DeepEqual(contractMember(t, resource, "actions"), contractMember(t, live, "actions")) {
		t.Fatal("pausing did not change the revision and actions")
	}
}

// placeholdersReturnTheSharedHelp is WriteContractTests.placeholdersReturnTheSharedHelp.
func TestPlaceholdersReturnTheSharedHelp(t *testing.T) {
	g := ReadOnlyFixture(t)
	belt := g.Coordinator.ReadBelts()[0]
	reply := getCase(g, "/api/v1/placeholders?belt="+belt.Belt.ID)
	requireStatus(t, reply, 200)
	contractSameAsValue(t, "placeholders", contractMember(t, contractTree(t, reply.Body), "placeholders"),
		g.Coordinator.PlaceholderHelp(belt.Belt, nil))
	requireStatus(t, getCase(g, "/api/v1/placeholders?belt=nope"), 404)
	requireStatus(t, getCase(g, "/api/v1/placeholders"), 422)
}

// libraryListQueriesOnlyChangeTheDaemonBrowserProjection is the belt half of ContractTests.libraryListQueries….
func TestLibraryListQueriesOnlyChangeTheDaemonBrowserProjection(t *testing.T) {
	g := ReadOnlyFixture(t)
	reply := getCase(g, "/api/v1/belts?search=issue&filter=running")
	requireStatus(t, reply, 200)
	tree := contractTree(t, reply.Body)
	if len(contractMember(t, tree, "belts").([]any)) != len(g.Coordinator.ReadBelts()) {
		t.Fatal("the query changed the belt count")
	}
	contractSameAsValue(t, "belt browser", contractMember(t, tree, "browser"), g.Coordinator.ReadBeltBrowser("issue", "running"))
}

// supplementalAgentGoldenCapturesAFieldOmittedByThePrimaryDemo is ContractTests.supplementalAgentGolden….
func TestSupplementalAgentGoldenCapturesAFieldOmittedByThePrimaryDemo(t *testing.T) {
	primary := Start(t, Options{})
	reply := getCase(primary, "/api/v1/agents")
	requireStatus(t, reply, 200)
	if strings.Contains(string(reply.Body), `"thinking"`) {
		t.Fatal("the primary demo's agents carry thinking")
	}
	CheckGolden(t, "agents", contractEndpoint(t, "agents").GoldenName, reply, primary.Root)

	supplemental := Start(t, Options{Supplemental: true})
	reply = getCase(supplemental, "/api/v1/agents/triage")
	requireStatus(t, reply, 200)
	agent := contractMember(t, contractMember(t, contractTree(t, reply.Body), "agent"), "thinking")
	if agent != "high" {
		t.Fatalf("triage thinking %v, want high", agent)
	}
	CheckBody(t, "supplemental agent with thinking", "agent-thinking-supplemental.json", reply.Body, supplemental.Root)
}

// hasStage reports whether the draft has a stage with id.
func hasStage(draft core.Belt, id string) bool {
	return draft.Stage(id) != nil
}

// hasStageNamed reports whether the draft has a stage with id and name.
func hasStageNamed(draft core.Belt, id, name string) bool {
	stage := draft.Stage(id)
	return stage != nil && stage.Name == name
}

// hasStageVersion reports whether the draft's stage id is pinned to agent version.
func hasStageVersion(draft core.Belt, id string, version int) bool {
	stage := draft.Stage(id)
	return stage != nil && stage.Agent != nil && stage.Agent.Version == version
}

// resourceOf is the resource member of a belt command's reply.
func resourceOf(t testing.TB, reply Reply) any {
	t.Helper()
	return contractMember(t, contractTree(t, reply.Body), "resource")
}

// anyStrings converts a decoded JSON string array.
func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.(string))
	}
	return out
}

// joinProblems joins problem values with single spaces.
func joinProblems(problems []any) string {
	parts := make([]string, 0, len(problems))
	for _, problem := range problems {
		parts = append(parts, problem.(string))
	}
	return strings.Join(parts, " ")
}

// mustDecode decodes a reply's body into v, failing the test on an error.
func mustDecode(t testing.TB, reply Reply, v any) {
	t.Helper()
	if err := decodeContract(reply.Body, v); err != nil {
		t.Fatalf("decode %s: %v", reply.Body, err)
	}
}

// repositoryBasesSaveReadsBackAndAnUnlistedOneIsRefused: overrides for listed
// repositories save and read back as the golden; an override for a repository no
// trigger lists is refused with its field, and the belt on disk is left as it was.
func TestRepositoryBasesSaveReadsBackAndAnUnlistedOneIsRefused(t *testing.T) {
	g := Start(t, Options{})
	draft := ContractSaveDraft()
	draft.Triggers[0].Repos = []string{"acme/app", "acme/web"}
	draft.Base = "develop"
	draft.RepositoryBases = map[string]string{"acme/app": "release/app", "acme/web": "release/web"}
	saved := postJSON(g, contractEndpoint(t, "beltSave").Path, contractBody(t, api.BeltSaveRequest{Belt: draft}))
	CheckGolden(t, "beltSave", "belt-save-repository-bases.json", saved, g.Root)
	savedBelt := contractMember(t, contractMember(t, contractTree(t, saved.Body), "resource"), "draft")
	want := map[string]any{"acme/app": "release/app", "acme/web": "release/web"}
	if bases := contractMember(t, savedBelt, "repositoryBases"); !reflect.DeepEqual(bases, want) {
		t.Fatalf("saved overrides = %v, want both", bases)
	}
	// The saved belt is read back from the API, as the editor reopens it.
	reread := contractTree(t, getCase(g, "/api/v1/belts/"+draft.ID).Body)
	for _, member := range []string{"draft"} {
		if bases := contractMember(t, contractMember(t, reread, member), "repositoryBases"); !reflect.DeepEqual(bases, want) {
			t.Fatalf("read-back %s overrides = %v, want both", member, bases)
		}
	}
}

// anUnlistedRepositoryBaseIsRefusedWithoutSaving: an override for a repository
// no trigger lists is refused with its field, and nothing is written.
func TestAnUnlistedRepositoryBaseIsRefusedWithoutSaving(t *testing.T) {
	g := Start(t, Options{})
	draft := ContractSaveDraft()
	draft.RepositoryBases = map[string]string{"acme/other": "release/other"}
	before := contractInventory(t, g.Root)
	refused := postJSON(g, contractEndpoint(t, "beltSave").Path, contractBody(t, api.BeltSaveRequest{Belt: draft}))
	requireProblems(t, refused, []string{"acme/other has a base branch, but no trigger lists it. Add the repository or remove its base branch."})
	locations := contractMember(t, contractTree(t, refused.Body), "locations").([]any)
	if len(locations) != 1 || contractMember(t, locations[0], "kind") != "belt" || contractMember(t, locations[0], "field") != "repositoryBase:acme/other" {
		t.Fatalf("locations %v, want the belt's repositoryBase:acme/other field", locations)
	}
	requireSameInventory(t, before, g.Root)
}
