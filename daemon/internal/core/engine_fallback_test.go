package core

import "testing"

// TestMissingOutputHolds: a session that wrote no output holds with a failure reason.
func TestMissingOutputHolds(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishWithoutOutput(&run)
	if run.State != RunStateHeld || engineStr(run.Reason) != "Triage failed" {
		t.Errorf("state %q reason %q", run.State, engineStr(run.Reason))
	}
	if ValueOf(run.Attempts[len(run.Attempts)-1].Tone) != ToneProblem {
		t.Error("the attempt tone is not problem")
	}
}

// TestUnknownOutcomeUsesTheFallback: an outcome line the stage doesn't know holds.
func TestUnknownOutcomeUsesTheFallback(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "ship it")
	if run.State != RunStateHeld {
		t.Fatalf("state = %q, want held", run.State)
	}
	if detail := run.Attempts[len(run.Attempts)-1].Detail; detail == nil || *detail != "no known outcome in triage.md" {
		t.Errorf("detail = %q", engineStr(detail))
	}
}

// TestRetryFallbackRerunsOnceThenHolds: a retry fallback runs the stage again, then
// holds when it fails again.
func TestRetryFallbackRerunsOnceThenHolds(t *testing.T) {
	t.Parallel()
	belt := engineIssueBelt()
	belt.Stages[0].Fallback = FallbackRetry
	run := engineStartRun(belt)
	engineFinishWithoutOutput(&run)
	if run.State != RunStateQueued || run.StageID != "triage" {
		t.Fatalf("first failure: state %q stage %q", run.State, run.StageID)
	}
	engineFinishWithoutOutput(&run)
	if run.State != RunStateHeld {
		t.Errorf("second failure: state %q, want held", run.State)
	}
}

// TestDropFallbackDrops: a drop fallback ends the run and cleans up.
func TestDropFallbackDrops(t *testing.T) {
	t.Parallel()
	belt := engineIssueBelt()
	belt.Stages[0].Fallback = FallbackDrop
	run := engineStartRun(belt)
	effects := engineFinishWithoutOutput(&run)
	if run.State != RunStateDropped {
		t.Errorf("state = %q, want dropped", run.State)
	}
	if !engineHasEffect(effects, EffectKindCleanUp) {
		t.Errorf("effects = %+v, want cleanUp", effects)
	}
}

// TestAClosedPullRequestUsesItsOutcome: a closed pull request holds with the closed
// outcome's name as the reason.
func TestAClosedPullRequestUsesItsOutcome(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineToWatch(&run)
	EngineAdvance(&run, Event{Kind: EventKindWatched, Watch: WatchEventClosed, Detail: new("#201")}, engineT0)
	if run.State != RunStateHeld || engineStr(run.Reason) != "closed" {
		t.Errorf("state %q reason %q", run.State, engineStr(run.Reason))
	}
}

// TestABlockedWatchHoldsWhateverItsOutcomesSay: a blocked watch holds whatever its
// outcomes say, and posts no comment.
func TestABlockedWatchHoldsWhateverItsOutcomesSay(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineToWatch(&run)
	index := engineWatchIndex(t, run)
	run.Stages[index].Outcomes[1].Then = Then{Action: ThenActionRetry}
	const reason = "no pull request from beltline/issue-142"
	effects := EngineAdvance(&run, Event{Kind: EventKindBlocked, Reason: reason}, engineT0)
	if run.State != RunStateHeld || engineStr(run.Reason) != reason {
		t.Errorf("state %q reason %q", run.State, engineStr(run.Reason))
	}
	last := run.Attempts[len(run.Attempts)-1]
	if ValueOf(last.Tone) != ToneProblem || last.EndedAt == nil {
		t.Errorf("attempt = %+v", last)
	}
	if engineHasEffect(effects, EffectKindComment) {
		t.Errorf("effects = %+v, want no comment", effects)
	}
}

// TestOnlyAStageAtWorkCanBeBlocked: a blocked report on a held run changes nothing.
func TestOnlyAStageAtWorkCanBeBlocked(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "needs info")
	if run.State != RunStateHeld {
		t.Fatalf("state = %q, want held", run.State)
	}
	EngineAdvance(&run, Event{Kind: EventKindBlocked, Reason: "later"}, engineT0)
	if engineStr(run.Reason) != "needs info" {
		t.Errorf("reason = %q, want needs info", engineStr(run.Reason))
	}
}
