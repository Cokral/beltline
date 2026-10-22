// The runs contract cases that read one run, its attempt output and artifacts, and the run list.
// Each case is registered in servertest_contract_runs.go with the rest of the runs area.
package servertest

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// scenarioRuns serves GET api/v1/runs on a fresh fixture (runs.json).
func scenarioRuns(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	// The matrix of the contract tests seeds the views state before these reads.
	g.SeedViews()
	reply := getCase(g, casePath("runs", "", ""))
	CheckResponse(t, reply, ExpectJSON)
	checkRunsShape(t, g, reply)
	return g, reply
}

// checkRunsShape is the shape ContractTests' checkShape asserts of the runs list: the coordinator's
// runs in their order, each with its actions.
func checkRunsShape(t testing.TB, g *Fixture, reply Reply) {
	t.Helper()
	var response api.RunsResponse
	if err := routes.JSONDecode(reply.Body, &response); err != nil {
		t.Fatalf("runs: %v", err)
	}
	var returned []core.Run
	for _, snapshot := range response.Runs {
		returned = append(returned, snapshot.Run)
		if len(snapshot.Actions) == 0 {
			t.Fatalf("run %s has no actions", snapshot.Run.ID)
		}
	}
	source := g.Coordinator.ReadRuns()
	if fmt.Sprint(runIDs(returned)) != fmt.Sprint(runIDs(source)) {
		t.Fatalf("runs %v, want the coordinator's %v", runIDs(returned), runIDs(source))
	}
	sorted := append([]core.Run(nil), returned...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if !a.CreatedAt.Equal(b.CreatedAt.Time) {
			return a.CreatedAt.Before(b.CreatedAt.Time)
		}
		return a.ID < b.ID
	})
	if fmt.Sprint(runIDs(sorted)) != fmt.Sprint(runIDs(returned)) {
		t.Fatalf("runs are not sorted by creation then id: %v", runIDs(returned))
	}
}

// scenarioRun serves GET api/v1/runs/{id}: the first run with attempts, else the first run (run-detail.json).
func scenarioRun(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	// The matrix of the contract tests seeds the views state before these reads.
	g.SeedViews()
	runs := g.Coordinator.ReadRuns()
	target := runs[0]
	for _, run := range runs {
		if len(run.Attempts) > 0 {
			target = run
			break
		}
	}
	reply := getCase(g, casePath("run", target.ID, ""))
	CheckResponse(t, reply, ExpectJSON)
	var response api.RunResponse
	if err := routes.JSONDecode(reply.Body, &response); err != nil {
		t.Fatalf("run: %v", err)
	}
	if response.Run.Run.ID != target.ID {
		t.Fatalf("run id %s, want %s", response.Run.Run.ID, target.ID)
	}
	if len(response.Run.Actions) == 0 {
		t.Fatal("the run carries no actions")
	}
	latest := g.Coordinator.ReadBelt(response.Run.Run.BeltID)
	if latest == nil {
		t.Fatalf("no belt %s", response.Run.Run.BeltID)
	}
	if response.Run.Run.BeltVersion >= latest.Belt.Version {
		t.Fatalf("run belt version %d is not older than the latest %d", response.Run.Run.BeltVersion, latest.Belt.Version)
	}
	return g, reply
}

// scenarioAttemptOutput serves the output of the action attempt (run-attempt-output.json).
func scenarioAttemptOutput(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	// The matrix of the contract tests seeds the views state before these reads.
	g.SeedViews()
	reply := getCase(g, casePath("runAttemptOutput", g.ActionRunID, g.ActionAttemptID))
	CheckResponse(t, reply, ExpectJSON)
	var response api.AttemptOutputResponse
	if err := routes.JSONDecode(reply.Body, &response); err != nil {
		t.Fatalf("attempt output: %v", err)
	}
	if response.Availability != "available" {
		t.Fatalf("availability %q, want available", response.Availability)
	}
	if len(response.Blocks) == 0 {
		t.Fatal("the attempt output has no blocks")
	}
	return g, reply
}

// scenarioAttemptArtifacts serves the artifacts of the action attempt (run-attempt-artifacts.json).
func scenarioAttemptArtifacts(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	// The matrix of the contract tests seeds the views state before these reads.
	g.SeedViews()
	reply := getCase(g, casePath("runAttemptArtifacts", g.ActionRunID, g.ActionAttemptID))
	CheckResponse(t, reply, ExpectJSON)
	var response api.AttemptArtifactsResponse
	if err := routes.JSONDecode(reply.Body, &response); err != nil {
		t.Fatalf("attempt artifacts: %v", err)
	}
	snapshot := g.Coordinator.ReadArtifactsSnapshot(g.ActionRunID, g.ActionAttemptID)
	if snapshot == nil {
		t.Fatal("no artifacts snapshot for the action attempt")
	}
	if len(response.Artifacts) != len(snapshot.Files) {
		t.Fatalf("%d artifacts, want %d", len(response.Artifacts), len(snapshot.Files))
	}
	for _, artifact := range response.Artifacts {
		if !strings.HasPrefix(artifact.URL, "/artifacts/") {
			t.Fatalf("artifact url %q is not a signed artifact link", artifact.URL)
		}
		if !artifact.ExpiresAt.Equal(ContractNow.Add(60 * time.Second)) {
			t.Fatalf("artifact expires %v, want one minute after the contract clock", artifact.ExpiresAt.Time)
		}
	}
	return g, reply
}
