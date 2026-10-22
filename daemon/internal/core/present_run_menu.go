// The run page's Run actions menu: which rows it shows, in order, their words
// and the action record each is enabled from. The page draws the rows; it
// decides none of them.

package core

// RunMenuOpens is what choosing a menu row does.
type RunMenuOpens string

const (
	// RunMenuOpensCommand sends the row's command right away.
	RunMenuOpensCommand RunMenuOpens = "command"
	// RunMenuOpensSend opens the Send sheet, choosing among the row's targets.
	RunMenuOpensSend RunMenuOpens = "send"
	// RunMenuOpensSendAgain opens the Send sheet to run the row's stage again.
	RunMenuOpensSendAgain RunMenuOpens = "sendAgain"
	// RunMenuOpensConfirm asks before it sends the row's command.
	RunMenuOpensConfirm RunMenuOpens = "confirm"
)

// RunMenuRow is one row of the run page's Run actions menu.
type RunMenuRow struct {
	Command RunCommand `json:"command"`
	Label   string     `json:"label"`
	// Icon names the row's glyph: pause, play, again, send, park, unpark, stop, drop or erase.
	Icon string `json:"icon"`
	// Danger is set on the rows that end something.
	Danger bool         `json:"danger"`
	Opens  RunMenuOpens `json:"opens"`
	// Divided is set on a row with a line above it.
	Divided bool `json:"divided"`
	// Stage is the stage the row acts on: the one Run again runs, or the one the Send sheet opens on.
	Stage *string `json:"stage,omitzero"`
	// Targets are the stages the Send sheet offers, in the run's order.
	Targets []RunMenuTarget `json:"targets,omitzero"`
	// Action is the record the row is enabled from. Absent on a row that only reports, such as Pausing….
	Action *RunAction `json:"action,omitzero"`
}

// RunMenuTarget is one stage the Send sheet offers.
type RunMenuTarget struct {
	Stage string `json:"stage"`
	Name  string `json:"name"`
}

// RunMenu is the rows of the run's Run actions menu, given the actions the run
// page offers. An active run has its pause row, Park or Unpark, Run again when
// held, Send to, then Abort and Drop; a finished one has Erase alone.
func RunMenu(run Run, actions []RunAction) []RunMenuRow {
	if !run.State.IsActive() {
		return []RunMenuRow{runMenuRow(actions, RunCommandErase, "Erase…", "erase", RunMenuOpensConfirm, nil)}
	}
	rows := []RunMenuRow{runMenuPauseRow(run, actions)}
	if run.State == RunStateParked {
		rows = append(rows, runMenuRow(actions, RunCommandUnpark, "Unpark", "unpark", RunMenuOpensCommand, nil))
	} else if runMenuAction(actions, RunCommandPark, nil) != nil {
		rows = append(rows, runMenuRow(actions, RunCommandPark, "Park", "park", RunMenuOpensCommand, nil))
	}
	current := run.stageRef()
	if run.State == RunStateHeld {
		name := "it"
		if current != nil {
			name = current.Name
		}
		stage := run.StageID
		rows = append(rows, runMenuRow(actions, RunCommandResume, "Run "+name+" again", "again", RunMenuOpensSendAgain, &stage))
	}
	send := runMenuRow(actions, RunCommandResume, "Send to…", "send", RunMenuOpensSend, nil)
	send.Targets = []RunMenuTarget{}
	for _, stage := range run.Stages {
		if stage.ID != run.StageID {
			send.Targets = append(send.Targets, RunMenuTarget{Stage: stage.ID, Name: stage.Name})
		}
	}
	first := run.StageID
	if len(send.Targets) > 0 {
		first = send.Targets[0].Stage
	}
	send.Stage = &first
	send.Action = runMenuAction(actions, RunCommandResume, &first)
	rows = append(rows, send)
	abort := runMenuRow(actions, RunCommandAbort, "Abort…", "stop", RunMenuOpensConfirm, nil)
	abort.Divided = true
	return append(rows, abort, runMenuRow(actions, RunCommandDrop, "Drop…", "drop", RunMenuOpensConfirm, nil))
}

// runMenuPauseRow follows the run's pause phase: Pause, Pausing…, Resume, Retry pause or Retry.
func runMenuPauseRow(run Run, actions []RunAction) RunMenuRow {
	var phase RunPausePhase
	if run.Pause != nil {
		phase = run.Pause.Phase
	}
	switch phase {
	case RunPausePhasePausing:
		row := runMenuRow(actions, RunCommandPause, "Pausing…", "pause", RunMenuOpensCommand, nil)
		row.Action = nil
		return row
	case RunPausePhasePaused, RunPausePhaseResuming:
		return runMenuRow(actions, RunCommandResumeRun, "Resume", "play", RunMenuOpensCommand, nil)
	case RunPausePhaseIncomplete:
		return runMenuRow(actions, RunCommandPause, "Retry pause", "again", RunMenuOpensCommand, nil)
	case RunPausePhaseReconcileFailed:
		return runMenuRow(actions, RunCommandResumeRun, "Retry", "again", RunMenuOpensCommand, nil)
	}
	return runMenuRow(actions, RunCommandPause, "Pause", "pause", RunMenuOpensCommand, nil)
}

// runMenuRow is a row for command, enabled from its record for stage (the
// command's own record when stage is nil).
func runMenuRow(actions []RunAction, command RunCommand, label, icon string, opens RunMenuOpens, stage *string) RunMenuRow {
	danger := command == RunCommandAbort || command == RunCommandDrop || command == RunCommandErase
	return RunMenuRow{Command: command, Label: label, Icon: icon, Danger: danger, Opens: opens, Stage: stage,
		Action: runMenuAction(actions, command, stage)}
}

// runMenuAction is the record of command for stage, or its unscoped record
// when stage is nil; nil when the actions have none.
func runMenuAction(actions []RunAction, command RunCommand, stage *string) *RunAction {
	for i := range actions {
		action := actions[i]
		if action.ID != string(command) {
			continue
		}
		if (stage == nil && action.IsUnscoped()) || (stage != nil && action.Stage != nil && *action.Stage == *stage) {
			return &action
		}
	}
	return nil
}
