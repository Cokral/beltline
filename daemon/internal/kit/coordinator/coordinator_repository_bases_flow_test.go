package coordinator

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// basesRecordingWorkspaces wraps the workspaces a coordinator prepares checkouts
// with, and records each spec it is asked to prepare.
type basesRecordingWorkspaces struct {
	runner.Workspaces
	mu    sync.Mutex
	specs []runner.WorkspaceSpec
}

// Prepare records spec, then prepares it with the wrapped workspaces.
func (w *basesRecordingWorkspaces) Prepare(ctx context.Context, spec runner.WorkspaceSpec) (string, error) {
	w.mu.Lock()
	w.specs = append(w.specs, spec)
	w.mu.Unlock()
	return w.Workspaces.Prepare(ctx, spec)
}

// prepared is a copy of the specs recorded so far.
func (w *basesRecordingWorkspaces) prepared() []runner.WorkspaceSpec {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]runner.WorkspaceSpec(nil), w.specs...)
}

// recordingWorkspacesFor is the flowOptions.workspaces that records into rec.
func recordingWorkspacesFor(rec *basesRecordingWorkspaces) func(runner.Workspaces) runner.Workspaces {
	return func(inner runner.Workspaces) runner.Workspaces {
		rec.Workspaces = inner
		return rec
	}
}

// TestEachRepositoryOfAJiraRunChecksOutItsOwnBase: each repository's checkout
// starts from that repository's own override, never the shared base where an
// override is set, and each brief names the same branch.
func TestEachRepositoryOfAJiraRunChecksOutItsOwnBase(t *testing.T) {
	t.Parallel()
	belt := flowBelt(core.TriggerKindJira, "", nil)
	belt.Triggers[0].Repos = []string{"acme/billing", "acme/infra"}
	belt.Base = "develop"
	belt.RepositoryBases = map[string]string{"acme/billing": "release/billing", "acme/infra": "release/infra"}
	rec := &basesRecordingWorkspaces{}
	h := newFlowHarness(t, flowOptions{belts: []core.Belt{belt}, tickets: []core.Subject{flowTicket}, now: flowT0, noPulls: true, workspaces: recordingWorkspacesFor(rec)})
	h.Tick()
	if run := h.Run(); run == nil {
		t.Fatal("no run")
	}
	want := map[string]string{"acme/billing": "release/billing", "acme/infra": "release/infra"}
	seen := map[string]string{}
	for _, spec := range rec.prepared() {
		seen[spec.Repo] = spec.Base
		if expected, ok := want[spec.Repo]; !ok || spec.Base != expected {
			t.Fatalf("checkout of %s starts from %q, want %q", spec.Repo, spec.Base, want[spec.Repo])
		}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("checkouts = %v, want one per repository at its own base %v", seen, want)
	}
}

// TestEachRepositoryOfAJiraRunBriefsItsOwnBase: a ticket's run over two
// repositories with two overrides names each repository's branch in its brief,
// and the shared base is not used where an override is set.
func TestEachRepositoryOfAJiraRunBriefsItsOwnBase(t *testing.T) {
	t.Parallel()
	belt := flowBelt(core.TriggerKindJira, "", nil)
	belt.Triggers[0].Repos = []string{"acme/billing", "acme/infra"}
	belt.Base = "develop"
	belt.RepositoryBases = map[string]string{"acme/billing": "release/billing", "acme/infra": "release/infra"}
	ticket := flowTicket
	h := newFlowHarness(t, flowOptions{belts: []core.Belt{belt}, tickets: []core.Subject{ticket}, now: flowT0, noPulls: true})
	h.Tick()
	run := h.Run()
	if run == nil {
		t.Fatal("no run")
	}
	if run.RepositoryBases["acme/billing"] != "release/billing" || run.RepositoryBases["acme/infra"] != "release/infra" {
		t.Fatalf("the run kept %v, want the belt's choices", run.RepositoryBases)
	}
	brief := readText(t, filepath.Join(flowFolder(h, *run), "01-triage-1", "brief.md"))
	for _, want := range []string{"- acme/billing: ", "merges into `release/billing`", "- acme/infra: ", "merges into `release/infra`"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "merges into `develop`") {
		t.Fatal("the brief names the shared base where each repository has an override")
	}
}

// TestASavedRunKeepsItsRepositoryBasesAfterANewerBeltVersion: a newer belt version
// that changes or removes the choices leaves the run's own choices alone, also
// after a restart reads them back from the run's saved file.
func TestASavedRunKeepsItsRepositoryBasesAfterANewerBeltVersion(t *testing.T) {
	t.Parallel()
	belt := flowBelt(core.TriggerKindJira, "", nil)
	belt.Triggers[0].Repos = []string{"acme/billing", "acme/infra"}
	belt.Base = "develop"
	belt.RepositoryBases = map[string]string{"acme/billing": "release/billing", "acme/infra": "release/infra"}
	h := newFlowHarness(t, flowOptions{belts: []core.Belt{belt}, tickets: []core.Subject{flowTicket}, now: flowT0, noPulls: true})
	h.Tick()
	run := h.Run()
	if run == nil {
		t.Fatal("no run")
	}
	changed := h.Coord().Belts()[0].Belt
	changed.Base = "main"
	changed.Triggers[0].Repos = []string{"acme/infra"}
	changed.RepositoryBases = map[string]string{"acme/infra": "release/other"}
	if _, err := h.Coord().SaveBelt(changed); err != nil {
		t.Fatalf("save: %v", err)
	}
	want := map[string]string{"acme/billing": "release/billing", "acme/infra": "release/infra"}
	for _, coord := range []*Coordinator{h.Coord(), h.Restart()} {
		saved := coord.Run(run.ID)
		if saved == nil {
			t.Fatal("the run is gone")
		}
		if !reflect.DeepEqual(saved.RepositoryBases, want) {
			t.Fatalf("the run's choices = %v, want %v", saved.RepositoryBases, want)
		}
		if got := saved.RequestedBase("acme/billing"); got == nil || *got != "release/billing" {
			t.Fatalf("requested base in acme/billing = %v, want release/billing", got)
		}
	}
	if got := h.Restart().Belts()[0].Belt.RepositoryBases; !reflect.DeepEqual(got, map[string]string{"acme/infra": "release/other"}) {
		t.Fatalf("the newer belt version = %v, want its own choices", got)
	}
}
