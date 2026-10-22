// The reason and empty-list copy of the Sessions sidebar:
// (`fallbackCopy(stageId:in:)` and `emptyCopy(in:)`): the sentences it
// shows when it selects a stage's latest session and when a run has no sessions at all.

package core

// SessionFallbackCopy is the reason a fallback selection shows: the stage named in the
// run, then whether it starts an agent or has no session yet.
func SessionFallbackCopy(run Run, stageID string) string {
	name := stageID
	var stage *Stage
	for i := range run.Stages {
		if run.Stages[i].ID == stageID {
			stage = &run.Stages[i]
			name = stage.Name
			break
		}
	}
	if stage == nil || stage.Kind() != StageKindAgent {
		return name + " runs no agent. Showing the latest session."
	}
	return name + " has no session yet. Showing the latest session."
}

// SessionEmptyCopy is the empty list's copy, naming the first agent stage that starts sessions.
func SessionEmptyCopy(run Run) string {
	name := "an agent"
	for _, stage := range run.Stages {
		if stage.Kind() == StageKindAgent {
			name = stage.Name
			break
		}
	}
	return "No sessions yet. The first one starts when " + name + " picks this issue up."
}
