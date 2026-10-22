// The run view cases: the shared projection at
// each width and layout fixture, the machines and metro looks, and the width rejections.
package servertest

import (
	"bytes"
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// runViewPath is the run page's path for id, with the id escaped as a path segment.
func runViewPath(id string) string { return "/api/v1/views/run/" + EncodeSegment(id) }

// decodeRunView decodes a run view reply's body.
func decodeRunView(t *testing.T, reply Reply) api.RunViewResponse {
	t.Helper()
	var response api.RunViewResponse
	if err := routes.JSONDecode(reply.Body, &response); err != nil {
		t.Fatalf("run view: %v", err)
	}
	return response
}

// notFound is the 404 every missing run view answers with.
var notFound = Expect{Status: http.StatusNotFound, ContentType: "application/json"}

// TestRunViewUsesSharedProjectionForWidthsAndLayoutFixtures ports
// RunReadTests.runViewUsesSharedProjectionForWidthsAndLayoutFixtures.
func TestRunViewUsesSharedProjectionForWidthsAndLayoutFixtures(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := f.Coordinator.ReadRun(f.ActionRunID)
	if run == nil {
		t.Fatal("the action run is gone")
	}
	for _, width := range []float64{420, 620, 621, 1080} {
		reply := f.Do(Request{Method: "GET", Path: runViewPath(run.ID) + "?width=" + formatWidth(width), Token: ContractToken})
		if reply.Status != http.StatusOK {
			t.Fatalf("width %v: status %d", width, reply.Status)
		}
		snapshot := f.Coordinator.ReadRunViewSnapshot(run.ID, width, nil)
		if snapshot == nil {
			t.Fatalf("width %v: no snapshot", width)
		}
		if !bytes.Equal(reply.Body, mustMarshal(t, api.NewRunViewResponse(*snapshot))) {
			t.Fatalf("width %v: the response is not the shared projection", width)
		}
		response := decodeRunView(t, reply)
		if response.Look != "boxes" {
			t.Fatalf("width %v: look %q, want boxes", width, response.Look)
		}
		if response.Map != nil {
			t.Fatalf("width %v: a boxes look sends no map", width)
		}
		if response.Boxes == nil || len(response.Boxes.Map.Nodes) != len(run.Stages)+1 {
			t.Fatalf("width %v: the boxes do not hold the trigger and every stage", width)
		}
		if !reflect.DeepEqual(response.Actions, f.Coordinator.RunActions(*run)) {
			t.Fatalf("width %v: actions differ from the coordinator's", width)
		}
	}

	type layoutCase struct {
		name      string
		predicate func(core.Run) bool
	}
	cases := []layoutCase{
		{"straight", func(r core.Run) bool { return len(r.Layout().Forks) == 0 }},
		{"forked", func(r core.Run) bool { return len(r.Layout().Forks) > 0 && !r.Layout().Nested() }},
		{"nested", func(r core.Run) bool { return r.Layout().Nested() }},
		{"held", func(r core.Run) bool { return r.State == core.RunStateHeld }},
		{"parked", func(r core.Run) bool { return r.State == core.RunStateParked }},
	}
	runs := f.Coordinator.ReadRuns()
	for _, layout := range cases {
		var example *core.Run
		for i := range runs {
			if layout.predicate(runs[i]) {
				example = &runs[i]
				break
			}
		}
		if example == nil {
			t.Fatalf("fixture needs a %s run", layout.name)
		}
		reply := f.Do(Request{Method: "GET", Path: runViewPath(example.ID), Token: ContractToken})
		if reply.Status != http.StatusOK {
			t.Fatalf("%s: status %d", layout.name, reply.Status)
		}
		snapshot := f.Coordinator.ReadRunViewSnapshot(example.ID, 1080, nil)
		if snapshot == nil {
			t.Fatalf("%s: no snapshot", layout.name)
		}
		if !bytes.Equal(reply.Body, mustMarshal(t, api.NewRunViewResponse(*snapshot))) {
			t.Fatalf("%s: the response is not the shared projection", layout.name)
		}
		CheckGolden(t, "run view "+layout.name, "views-run-"+layout.name+".json", reply, f.Root)
	}

	f.Coordinator.AppendRunForContract(SplitRun(ContractNow))
	splitReply := f.Do(Request{Method: "GET", Path: runViewPath("split/view"), Token: ContractToken})
	if splitReply.Status != http.StatusOK {
		t.Fatalf("split: status %d", splitReply.Status)
	}
	split := decodeRunView(t, splitReply)
	if split.Boxes == nil {
		t.Fatal("split: no boxes")
	}
	if !reflect.DeepEqual(split.Boxes.TakenRoutes, []string{"scope:simple", "scope:thorough"}) {
		t.Fatalf("split: taken routes %v", split.Boxes.TakenRoutes)
	}
	if node := split.Boxes.Node("simple"); node == nil || node.State != core.RunBoxesStateRunning {
		t.Fatal("split: the simple stage is not running")
	}
	if node := split.Boxes.Node("thorough"); node == nil || node.State != core.RunBoxesStateTodo {
		t.Fatal("split: the thorough stage is not todo")
	}
	CheckGolden(t, "split run view", "views-run-split.json", splitReply, f.Root)

	missing := f.Do(Request{Method: "GET", Path: runViewPath("no-such-run"), Token: ContractToken})
	if missing.Status != http.StatusNotFound {
		t.Fatalf("missing: status %d", missing.Status)
	}
	CheckGoldenAs(t, notFound, "missing run view", "error-not-found.json", missing, f.Root)

	if err := store.NewStorage(store.Paths{Root: f.Root}).RecordErased("erased-read-run", 0); err != nil {
		t.Fatalf("record erased: %v", err)
	}
	erased := f.Do(Request{Method: "GET", Path: runViewPath("erased-read-run"), Token: ContractToken})
	if erased.Status != http.StatusNotFound {
		t.Fatalf("erased: status %d", erased.Status)
	}
	CheckGoldenAs(t, notFound, "erased run view", "error-not-found.json", erased, f.Root)
}

// formatWidth writes a whole-point width as the query takes it.
func formatWidth(width float64) string { return strconv.Itoa(int(width)) }

// TestMachinesLookSendsTheMachineMapInsteadOfBoxes checks the machines look sends the machine map and no boxes.
func TestMachinesLookSendsTheMachineMapInsteadOfBoxes(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{Look: core.BeltLookMachines})
	run := firstRun(t, f, func(r core.Run) bool { return len(r.Layout().Forks) > 0 && !r.Layout().Nested() })
	reply := f.Do(Request{Method: "GET", Path: runViewPath(run.ID), Token: ContractToken})
	if reply.Status != http.StatusOK {
		t.Fatalf("status %d", reply.Status)
	}
	response := decodeRunView(t, reply)
	if response.Look != "machines" {
		t.Fatalf("look %q, want machines", response.Look)
	}
	if response.Boxes != nil || response.Metro != nil {
		t.Fatal("a machines look sends neither boxes nor metro")
	}
	if response.Map == nil || len(response.Map.Tracks) == 0 {
		t.Fatal("a machines look sends a map with tracks")
	}
	snapshot := f.Coordinator.ReadRunViewSnapshot(run.ID, 1080, nil)
	if snapshot == nil {
		t.Fatal("no snapshot")
	}
	if !bytes.Equal(reply.Body, mustMarshal(t, api.NewRunViewResponse(*snapshot))) {
		t.Fatal("the response is not the shared projection")
	}
	CheckGolden(t, "run view machines", "views-run-machines.json", reply, f.Root)
}

// TestMetroLookSendsTheRunsMetroLine ports RunReadTests.metroLookSendsTheRunsMetroLine.
func TestMetroLookSendsTheRunsMetroLine(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{Look: core.BeltLookMetro})
	run := firstRun(t, f, func(r core.Run) bool { return len(r.Layout().Forks) > 0 && !r.Layout().Nested() })
	reply := f.Do(Request{Method: "GET", Path: runViewPath(run.ID), Token: ContractToken})
	if reply.Status != http.StatusOK {
		t.Fatalf("status %d", reply.Status)
	}
	response := decodeRunView(t, reply)
	if response.Look != "metro" {
		t.Fatalf("look %q, want metro", response.Look)
	}
	if response.Map != nil || response.Boxes != nil {
		t.Fatal("a metro look sends neither map nor boxes")
	}
	if response.Metro == nil || len(response.Metro.Stations) == 0 {
		t.Fatal("a metro look sends a metro line with stations")
	}
	CheckGolden(t, "run view metro", "views-run-metro.json", reply, f.Root)
}

// TestRunViewRejectsInvalidWidthsWithTheShared422Shape ports RunReadTests.runViewRejectsInvalidWidthsWithTheShared422Shape.
func TestRunViewRejectsInvalidWidthsWithTheShared422Shape(t *testing.T) {
	t.Parallel()
	f := ReadOnlyFixture(t)
	run := f.Coordinator.ReadRun(f.ActionRunID)
	if run == nil {
		t.Fatal("the action run is gone")
	}
	path := runViewPath(run.ID) + "?width="
	message := "Width must be a finite positive number of points."
	for _, value := range []string{"nan", "infinity", "-infinity", "0", "-1", "nope"} {
		reply := f.Do(Request{Method: "GET", Path: path + value, Token: ContractToken})
		require422Problem(t, reply, message, []string{message})
	}
	duplicateMessage := "Width must be provided once as a finite positive number of points."
	duplicate := f.Do(Request{Method: "GET", Path: path + "420&width=620", Token: ContractToken})
	require422Problem(t, duplicate, duplicateMessage, []string{duplicateMessage})
}

// firstRun is the first run of the coordinator's list that satisfies predicate; the test fails when none does.
func firstRun(t *testing.T, f *Fixture, predicate func(core.Run) bool) core.Run {
	t.Helper()
	for _, run := range f.Coordinator.ReadRuns() {
		if predicate(run) {
			return run
		}
	}
	t.Fatal("the fixture has no run of the wanted layout")
	return core.Run{}
}
