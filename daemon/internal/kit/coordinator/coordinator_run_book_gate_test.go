package coordinator

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// gateBook is a run book holding one run, "r", with only bar applied.
func gateBook(bar runBar) *RunBook {
	run := core.Run{ID: "r", Generation: 2, Operation: 5}
	book := &RunBook{busy: map[string]struct{}{}, restarting: map[string]struct{}{}, erasing: map[string]struct{}{}}
	switch bar {
	case barParked:
		run.Parking = &core.Parking{}
	case barErasing:
		book.erasing["r"] = struct{}{}
	case barRestarting:
		book.restarting["r"] = struct{}{}
	case barRestartIntent:
		run.Restart = &core.RestartIntent{Phase: core.RestartIntentPhaseIntent}
	case barPaused:
		run.Pause = &core.RunPause{Phase: core.RunPausePhasePaused}
	case barBusy:
		book.busy["r"] = struct{}{}
	}
	book.runs = []core.Run{run}
	return book
}

// TestRunWorkBars pins what keeps each kind of work off a run. A change here is
// a change to when the coordinator touches a run, so each row is written out
// rather than read from runWorkBars.
func TestRunWorkBars(t *testing.T) {
	t.Parallel()
	all := []runBar{barParked, barErasing, barRestarting, barRestartIntent, barPaused, barBusy}
	want := map[runWork][]runBar{
		workEvent:        {barErasing, barRestarting, barRestartIntent},
		workWrite:        {barErasing},
		workCheck:        {barErasing, barRestarting, barRestartIntent},
		workAdvance:      {barParked, barErasing, barRestarting, barRestartIntent, barPaused},
		workDecide:       {barParked, barErasing, barRestarting, barRestartIntent, barPaused, barBusy},
		workIntake:       {barErasing, barPaused, barBusy},
		workIntakeForced: {barErasing, barBusy},
		workContinue:     {barParked, barErasing},
	}
	if len(want) != len(runWorkBars) {
		t.Fatalf("runWorkBars has %d kinds of work, the test %d", len(runWorkBars), len(want))
	}
	for work, bars := range want {
		barred := map[runBar]bool{}
		for _, bar := range bars {
			barred[bar] = true
		}
		for _, bar := range all {
			book := gateBook(bar)
			got := book.bar(&book.runs[0], work)
			if barred[bar] && got != bar {
				t.Errorf("work %d, bar %d: got %d, want it barred", work, bar, got)
			}
			if !barred[bar] && got != barNone {
				t.Errorf("work %d, bar %d: got %d, want it let through", work, bar, got)
			}
			if index := book.liveIndex("r", work, nil, nil); (index < 0) != barred[bar] {
				t.Errorf("work %d, bar %d: liveIndex %d", work, bar, index)
			}
		}
	}
}

// TestRunBookLiveIndexMatchesTheWorksTokens covers the generation and operation a
// piece of work started with, and a run that is gone.
func TestRunBookLiveIndexMatchesTheWorksTokens(t *testing.T) {
	t.Parallel()
	book := gateBook(barNone)
	generation, operation, other := 2, 5, 9
	cases := []struct {
		name                  string
		id                    string
		generation, operation *int
		want                  int
	}{
		{"no tokens", "r", nil, nil, 0},
		{"both match", "r", &generation, &operation, 0},
		{"other generation", "r", &other, &operation, -1},
		{"other operation", "r", &generation, &other, -1},
		{"gone", "x", nil, nil, -1},
	}
	for _, c := range cases {
		if got := book.liveIndex(c.id, workEvent, c.generation, c.operation); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	if !book.accepts("r", 2, &operation) || book.accepts("r", 3, nil) {
		t.Error("accepts disagrees with liveIndex")
	}
}
