package core

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestLegEncodesOnlyWhatIsSet checks that a leg writes its set keys, and writes
// departJoins only when it has some.
func TestLegEncodesOnlyWhatIsSet(t *testing.T) {
	t.Parallel()
	bare, err := jsonx.Marshal(Leg{ID: "main", State: RunStateQueued})
	if err != nil {
		t.Fatal(err)
	}
	if string(bare) != `{"id":"main","state":"queued"}` {
		t.Errorf("bare leg = %s", bare)
	}
	stage := "build"
	departing := Leg{ID: "r:x", State: RunStateRunning, StageID: &stage, DepartJoins: []string{"j1"}}
	data, err := jsonx.Marshal(departing)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"departJoins":["j1"],"id":"r:x","stageId":"build","state":"running"}`; !sameJSON(t, data, want) {
		t.Errorf("departing leg = %s, want %s", data, want)
	}
	var back Leg
	testDecode(t, string(data), &back)
	if d := testDiffEmpty(departing, back); d != "" {
		t.Fatalf("round trip: %s", d)
	}
}

// TestLegFromIDKeepsTheLineFree checks that the line before a fork has no leg id to save.
func TestLegFromIDKeepsTheLineFree(t *testing.T) {
	t.Parallel()
	main, route := "main", "r:simple"
	if LegFromID(&main) != nil || LegFromID(nil) != nil {
		t.Error("main or nil saved as a leg id")
	}
	if got := LegFromID(&route); got == nil || *got != route {
		t.Errorf("route leg = %v", got)
	}
}

// TestLegCanAbortByStageKind checks that a running agent or watch can be
// aborted, a waiting approval can, and nothing else, including a leg with no stage.
func TestLegCanAbortByStageKind(t *testing.T) {
	t.Parallel()
	run := Run{Stages: []Stage{
		{ID: "build", Type: StageTypeAgent},
		{ID: "approve", Type: StageTypeApprove},
		{ID: "watch", Type: StageTypeWatch},
	}}
	cases := []struct {
		name string
		leg  Leg
		want bool
	}{
		{"running agent", Leg{ID: "main", StageID: new("build"), State: RunStateRunning}, true},
		{"running watch", Leg{ID: "main", StageID: new("watch"), State: RunStateRunning}, true},
		{"waiting approval", Leg{ID: "main", StageID: new("approve"), State: RunStateWaiting}, true},
		{"running approval", Leg{ID: "main", StageID: new("approve"), State: RunStateRunning}, false},
		{"held agent", Leg{ID: "main", StageID: new("build"), State: RunStateHeld}, false},
		{"no stage", Leg{ID: "main", State: RunStateRunning}, false},
		{"stage not in the run", Leg{ID: "main", StageID: new("gone"), State: RunStateRunning}, false},
	}
	for _, tc := range cases {
		if got := tc.leg.CanAbort(run); got != tc.want {
			t.Errorf("%s: canAbort %t, want %t", tc.name, got, tc.want)
		}
	}
}
