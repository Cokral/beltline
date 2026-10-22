// Coordinator tests of shadow tests: new runs alternate between live and the
// shadow and keep their version, and accept and discard end the test.

package coordinator

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestShadowTestSplitsNewRunsAndEndsOnAcceptOrDiscard walks one belt through a
// test that is accepted and one that is discarded.
func TestShadowTestSplitsNewRunsAndEndsOnAcceptOrDiscard(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Issues: []core.Subject{}, Belts: []core.Belt{manualBelt("tasks")}})
	c := h.coord
	c.Hold()
	defer func() { c.Release(); c.Idle() }()

	live := c.ReadBelt("tasks").Belt
	if refusal := c.BeltCommandRefusal(core.BeltCommandShadowAccept, "tasks", false); refusal == nil || *refusal != core.BeltAvailabilityNoShadow {
		t.Fatalf("accept without a shadow: %v", refusal)
	}
	draft := live.Clone()
	draft.Stages[0].Name = "Shadow triage"
	draft.Triggers = nil
	record, err := c.SaveShadowCommand(draft)
	if err != nil {
		t.Fatal(err)
	}
	test := record.ShadowTest()
	if test == nil || test.ID != "t1" || test.Live != live.Version || record.Shadow == nil || record.Shadow.Version != live.Version+1 {
		t.Fatalf("after the first shadow save %+v", record.ShadowTests)
	}
	if len(record.Shadow.Triggers) != len(live.Triggers) || record.Belt.Version != live.Version {
		t.Fatalf("the shadow lost live's triggers or live moved: %+v", record.Shadow.Triggers)
	}
	if refusal := c.BeltCommandRefusal(core.BeltCommandShadowCreate, "tasks", false); refusal == nil {
		t.Fatal("a second shadow could start while one runs")
	}

	arms := map[core.ShadowArm]int{}
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		run, err := c.StartTaskCommand("tasks", "acme/web", title, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if run.Shadow == nil || run.Shadow.Test != "t1" {
			t.Fatalf("run %s has no mark: %+v", title, run.Shadow)
		}
		arms[run.Shadow.Arm]++
		wantVersion, wantName := live.Version, live.Stages[0].Name
		if run.Shadow.Arm == core.ShadowArmShadow {
			wantVersion, wantName = record.Shadow.Version, "Shadow triage"
		}
		if run.BeltVersion != wantVersion || run.Stages[0].Name != wantName {
			t.Fatalf("run on %s started on v%d with %q", run.Shadow.Arm, run.BeltVersion, run.Stages[0].Name)
		}
	}
	if arms[core.ShadowArmLive] != 2 || arms[core.ShadowArmShadow] != 2 {
		t.Fatalf("arms %v", arms)
	}

	draft.Stages[0].Name = "Shadow triage, again"
	record, err = c.SaveShadowCommand(draft)
	if err != nil || record.ShadowTest().ID != "t1" || record.Shadow.Version != live.Version+2 {
		t.Fatalf("second shadow save %+v, %v", record.ShadowTests, err)
	}
	record, err = c.AcceptShadowCommand("tasks")
	if err != nil {
		t.Fatal(err)
	}
	if record.ShadowTest() != nil || record.Shadow != nil || record.Belt.Version != live.Version+2 || record.Belt.Stages[0].Name != "Shadow triage, again" {
		t.Fatalf("after accept v%d %+v", record.Belt.Version, record.ShadowTests)
	}
	if record.ShadowTests[0].Result != core.ShadowResultAccepted {
		t.Fatalf("test result %+v", record.ShadowTests[0])
	}
	for _, run := range c.Runs() {
		if run.BeltVersion != live.Version && run.BeltVersion != live.Version+1 {
			t.Fatalf("a run moved to v%d", run.BeltVersion)
		}
	}
	if run, err := c.StartTaskCommand("tasks", "acme/web", "After", "", nil); err != nil || run.Shadow != nil || run.BeltVersion != live.Version+2 {
		t.Fatalf("a run after the test %+v, %v", run.Shadow, err)
	}

	if _, err := c.SaveShadowCommand(draft); err != nil {
		t.Fatal(err)
	}
	record, err = c.DiscardShadowCommand("tasks")
	if err != nil {
		t.Fatal(err)
	}
	if record.ShadowTest() != nil || record.Belt.Version != live.Version+2 || len(record.ShadowTests) != 2 || record.ShadowTests[1].Result != core.ShadowResultDiscarded {
		t.Fatalf("after discard v%d %+v", record.Belt.Version, record.ShadowTests)
	}
}
