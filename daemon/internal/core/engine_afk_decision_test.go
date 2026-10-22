package core

import "testing"

// TestAfkActionLabelNamesWhatEachOptionDid pins the log label of every option kind,
// with the run's current stage name and a send's target stage name.
func TestAfkActionLabelNamesWhatEachOptionDid(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateWaiting, "watch")
	run.Stages = []Stage{
		{ID: "build", Name: "Build"},
		{ID: "watch", Name: "Watch"},
		{ID: "spec", Name: "Spec"},
	}
	for _, tc := range []struct {
		name   string
		option AfkOption
		want   string
	}{
		{"answer", AfkOption{ID: "answer", Action: AfkAction{Kind: AfkActionKindAnswer}}, "Answered Watch"},
		{"rerun", AfkOption{ID: "rerun", Action: AfkAction{Kind: AfkActionKindRerun}}, "Ran Watch again"},
		{"decide", AfkOption{ID: "decide approved", Action: AfkAction{Kind: AfkActionKindDecide, Outcome: "approved"}}, "Chose “approved” at Watch"},
		{"send", AfkOption{ID: "send spec", Action: AfkAction{Kind: AfkActionKindSend, Stage: "spec"}}, "Sent to Spec"},
		{"send unknown stage", AfkOption{ID: "send gone", Action: AfkAction{Kind: AfkActionKindSend, Stage: "gone"}}, "Sent to gone"},
		{"drop", AfkOption{ID: "drop", Action: AfkAction{Kind: AfkActionKindDrop}}, "Dropped the run"},
		{"leave", AfkOption{ID: "leave", Action: AfkAction{Kind: AfkActionKindLeave}}, "Left it for you"},
	} {
		if got := AfkActionLabel(tc.option, run); got != tc.want {
			t.Errorf("%s: AfkActionLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestAfkActionLabelFallsBackToTheStageID names the stage by its id when the run's
// current stage is not on the run.
func TestAfkActionLabelFallsBackToTheStageID(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateWaiting, "missing")
	run.Stages = []Stage{{ID: "build", Name: "Build"}}
	option := AfkOption{ID: "answer", Action: AfkAction{Kind: AfkActionKindAnswer}}
	if got, want := AfkActionLabel(option, run), "Answered missing"; got != want {
		t.Errorf("AfkActionLabel = %q, want %q", got, want)
	}
}
