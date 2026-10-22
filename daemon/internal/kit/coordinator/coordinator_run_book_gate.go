// The run book's gate: which work may still touch a run. Every part of the
// coordinator that is about to change a run, or look after one, asks here
// with what it is about to do, instead of restating the checks itself.

package coordinator

import "github.com/robbertmanders/beltline/daemon/internal/core"

// runWork is what a part of the coordinator is about to do with a run. Each
// kind of work is kept off a run by its own bars, listed in runWorkBars.
type runWork int

const (
	// workEvent applies an engine event or another result of work that
	// started earlier (book.apply, book.accepts).
	workEvent runWork = iota
	// workWrite changes the run's saved fields (book.update). An overrule saves
	// and clears its restart intent through it, so a restart does not bar it.
	workWrite
	// workCheck looks after a session that is already running.
	workCheck
	// workAdvance moves the run on by itself: starting a stage's session or a
	// watch pass looking at its pull requests or ticket.
	workAdvance
	// workDecide starts an AFK decision for a run that waits for you.
	workDecide
	// workIntake tells the run what its source saw.
	workIntake
	// workIntakeForced tells the run what its source saw, past a pause
	// (Resume reconciling the run).
	workIntakeForced
	// workContinue keeps a Continue session that has just launched.
	workContinue
)

// runBar is one thing that keeps work off a run.
type runBar int

const (
	barNone runBar = iota
	// barParked: the run is parked.
	barParked
	// barErasing: the run's erasure is recorded and not yet finished.
	barErasing
	// barRestarting: an overrule is between saving its intent and queuing the replacement.
	barRestarting
	// barRestartIntent: the run carries a restart intent that has not been carried out.
	barRestartIntent
	// barPaused: the run's pause blocks progress.
	barPaused
	// barBusy: a whole-run operation is in flight.
	barBusy
)

// runWorkBars is, per kind of work, what keeps it off a run, checked in this
// order: bar reports the first that applies.
var runWorkBars = map[runWork][]runBar{
	workEvent:        {barErasing, barRestarting, barRestartIntent},
	workWrite:        {barErasing},
	workCheck:        {barErasing, barRestarting, barRestartIntent},
	workAdvance:      {barParked, barErasing, barRestarting, barRestartIntent, barPaused},
	workDecide:       {barParked, barPaused, barBusy, barErasing, barRestarting, barRestartIntent},
	workIntake:       {barPaused, barErasing, barBusy},
	workIntakeForced: {barErasing, barBusy},
	workContinue:     {barParked, barErasing},
}

// bar is the first thing that keeps work off run, or barNone when nothing does.
// Lock: runs with c.mu held.
func (b *RunBook) bar(run *core.Run, work runWork) runBar {
	for _, bar := range runWorkBars[work] {
		if b.holds(bar, run) {
			return bar
		}
	}
	return barNone
}

// holds reports whether bar applies to run.
// Lock: runs with c.mu held.
func (b *RunBook) holds(bar runBar, run *core.Run) bool {
	switch bar {
	case barParked:
		return run.Parking != nil
	case barErasing:
		return b.isErasing(run.ID)
	case barRestarting:
		_, ok := b.restarting[run.ID]
		return ok
	case barRestartIntent:
		return run.Restart != nil && run.Restart.Phase == core.RestartIntentPhaseIntent
	case barPaused:
		return run.Pause != nil && run.Pause.BlocksProgress()
	case barBusy:
		_, ok := b.busy[run.ID]
		return ok
	}
	return false
}

// may reports whether work may touch run now.
// Lock: runs with c.mu held.
func (b *RunBook) may(run core.Run, work runWork) bool {
	return b.bar(&run, work) == barNone
}

// liveIndex is the position of the run with id when work may still touch it:
// nothing bars it and, when given, its generation and operation are still the
// ones the work started with. -1 otherwise.
// Lock: runs with c.mu held.
func (b *RunBook) liveIndex(id string, work runWork, generation, operation *int) int {
	index := b.index(id)
	if index < 0 {
		return -1
	}
	run := &b.runs[index]
	if generation != nil && run.Generation != *generation {
		return -1
	}
	if operation != nil && run.Operation != *operation {
		return -1
	}
	if b.bar(run, work) != barNone {
		return -1
	}
	return index
}
