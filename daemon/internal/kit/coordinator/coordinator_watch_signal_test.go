// The checks, behind, retry,
// early-review and comment tests. The fixture is in coordinator_watch_fixture_5m3_test.go;
// the rest of the file's tests are in the other coordinator_watch_signal_*_5m3_test.go files.

package coordinator

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
)

// TestChecksWaitUntilFinishedAndRecordCounts
func TestChecksWaitUntilFinishedAndRecordCounts(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{})
	f.open()
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after first poll = %v, want running", f.run().State)
	f.gh.SetChecks(201, 1, 14, true)
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state while checks run = %v, want running", f.run().State)
	f.gh.SetChecks(201, 1, 14, false)
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"checks failing"})
	coordCheck(t, coordLastDetail(f.run(), "watch") == "#201 checks failed (1 of 14)", "detail = %q", f.lastDetail())
	stored := f.stored()
	coordCheck(t, len(stored) == 1 && stored[0].PullRequest() != nil && *stored[0].PullRequest().Checks == core.PullRequestCheckStateFailing,
		"saved checks are not failing")
}

// watchAttempts are the run's attempts at the watch stage.
func watchAttempts(run core.Run) []core.Attempt {
	var out []core.Attempt
	for _, attempt := range run.Attempts {
		if attempt.StageID == "watch" {
			out = append(out, attempt)
		}
	}
	return out
}

// TestBehindNeedsTwoObservationsAndIsMeasuredEachCheck
func TestBehindNeedsTwoObservationsAndIsMeasuredEachCheck(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventMerged, core.WatchEventClosed, core.WatchEventBehind}})
	f.open()
	f.gh.SetBehind(201, 3)
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after one poll = %v, want running", f.run().State)
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"is behind"})
	coordCheck(t, f.lastDetail() == "#201 is 3 commits behind main", "detail = %q", f.lastDetail())
	want := []github.FakeCall{commitsBehindCall("a"), commitsBehindCall("a")}
	coordCalls(t, f.gh.Calls(), want)
	f.resume()
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after resume = %v, want running", f.run().State)
	f.gh.SetHead(201, new("b"))
	f.gh.SetBehind(201, 0)
	f.check()
	coordCheck(t, len(f.watchOutcomes()) == 1, "outcomes after caught up = %v, want 1", f.watchOutcomes())
	f.gh.SetHead(201, new("c"))
	f.gh.SetBehind(201, 1)
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after one behind = %v, want running", f.run().State)
	f.check()
	coordCheck(t, len(f.watchOutcomes()) == 2, "outcomes = %v, want 2", f.watchOutcomes())
	coordCheck(t, len(f.gh.Calls()) == 6, "calls = %d, want 6", len(f.gh.Calls()))
}

// commitsBehindCall is the recorded call for a behind check of head a on acme/billing against main.
func commitsBehindCall(head string) github.FakeCall {
	return github.FakeCall{Kind: github.FakeCallCommitsBehind, Repo: "acme/billing", Base: "main", Head: head}
}

// coordCalls compares recorded GitHub calls with want.
func coordCalls(t *testing.T, got, want []github.FakeCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("calls = %+v, want %+v", got, want)
		}
	}
}

// TestABaseThatMovesOnMakesTheSameCommitBehind
func TestABaseThatMovesOnMakesTheSameCommitBehind(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventMerged, core.WatchEventClosed, core.WatchEventBehind}})
	f.open()
	f.check()
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state = %v, want running", f.run().State)
	// Other pull requests merge into main; this one's head stays where it was.
	f.gh.SetBehind(201, 2)
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after base moved = %v, want running", f.run().State)
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"is behind"})
	coordCheck(t, f.lastDetail() == "#201 is 2 commits behind main", "detail = %q", f.lastDetail())
}

// TestConfirmedBehindLosesToConflictsAndChecks
func TestConfirmedBehindLosesToConflictsAndChecks(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{})
	f.open()
	f.gh.SetBehind(201, 3)
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string(nil))
	f.gh.SetConflicted(201, true)
	f.gh.SetChecks(201, 1, 14, false)
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"has conflicts"})
	f.resume()
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"has conflicts", "checks failing"})
	f.resume()
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"has conflicts", "checks failing", "is behind"})
	coordCheck(t, len(f.gh.Calls()) == 4, "calls = %d, want 4", len(f.gh.Calls()))
}

// TestBehindRetriesAreBoundedAndNewCommitsNeedTwoChecks
func TestBehindRetriesAreBoundedAndNewCommitsNeedTwoChecks(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventMerged, core.WatchEventClosed, core.WatchEventBehind}, retry: true})
	f.open()
	for index := 1; index <= 3; index++ {
		f.gh.SetHead(201, new("commit-"+strconv.Itoa(index)))
		f.gh.SetBehind(201, index)
		f.check()
		coordCheck(t, f.run().State == core.RunStateRunning, "round %d: state after one poll = %v, want running", index, f.run().State)
		coordCheck(t, len(f.watchOutcomes()) == index-1, "round %d: outcomes = %v", index, f.watchOutcomes())
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == index, "round %d: outcomes after two polls = %v", index, f.watchOutcomes())
		if index < 3 {
			coordCheck(t, f.run().StageID == "build", "round %d: stage = %s, want build", index, f.run().StageID)
			f.finish("done")
		}
	}
	coordCheck(t, f.run().State == core.RunStateHeld, "state = %v, want held", f.run().State)
	coordCheck(t, f.run().Reason != nil && *f.run().Reason == "is behind, 2 times already", "reason = %v", f.run().Reason)
	coordCheck(t, len(f.gh.Calls()) == 6, "calls = %d, want 6", len(f.gh.Calls()))
	stored := f.stored()
	coordCheck(t, len(stored) == 1 && stored[0].PullRequest() != nil && *stored[0].PullRequest().Behind == 3, "saved behind is not 3")
}

// TestClearingAndNewCommitsRearmWithoutReportingRecovery
func TestClearingAndNewCommitsRearmWithoutReportingRecovery(t *testing.T) {
	t.Parallel()
	for _, event := range []core.WatchEvent{core.WatchEventChecksFailed, core.WatchEventConflicted} {
		f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventMerged, core.WatchEventClosed, event}})
		f.open()
		if event == core.WatchEventChecksFailed {
			f.gh.SetChecks(201, 1, 14, false)
		} else {
			f.gh.SetConflicted(201, true)
		}
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == 1, "%s: outcomes = %v, want 1", event, f.watchOutcomes())
		coordCheck(t, len(f.gh.Calls()) == 0, "%s: calls = %v, want none", event, f.gh.Calls())
		f.resume()
		f.check()
		coordCheck(t, f.run().State == core.RunStateRunning, "%s: state after resume = %v", event, f.run().State)
		if event == core.WatchEventChecksFailed {
			f.gh.SetChecks(201, 0, 14, false)
		} else {
			f.gh.SetConflicted(201, false)
		}
		f.check()
		coordCheck(t, f.run().State == core.RunStateRunning, "%s: state after clearing = %v", event, f.run().State)
		coordCheck(t, len(f.watchOutcomes()) == 1, "%s: outcomes after clearing = %v, want 1", event, f.watchOutcomes())
		if event == core.WatchEventChecksFailed {
			f.gh.SetChecks(201, 1, 14, false)
		} else {
			f.gh.SetConflicted(201, true)
		}
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == 2, "%s: outcomes after recurring = %v, want 2", event, f.watchOutcomes())
		f.resume()
		f.gh.SetHead(201, new("new-commit"))
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == 3, "%s: outcomes after new commit = %v, want 3", event, f.watchOutcomes())
	}
}

// TestRetriesAreBoundedAndACommitAfterBuildRearmsChecks
func TestRetriesAreBoundedAndACommitAfterBuildRearmsChecks(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventMerged, core.WatchEventClosed, core.WatchEventChecksFailed}, retry: true})
	f.open()
	f.gh.SetChecks(201, 1, 14, false)
	for index := 1; index <= 3; index++ {
		f.gh.SetHead(201, new("commit-"+strconv.Itoa(index)))
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == index, "round %d: outcomes = %v", index, f.watchOutcomes())
		if index < 3 {
			coordCheck(t, f.run().StageID == "build", "round %d: stage = %s, want build", index, f.run().StageID)
			f.finish("done")
		}
	}
	coordCheck(t, f.run().State == core.RunStateHeld, "state = %v, want held", f.run().State)
	coordCheck(t, f.run().Reason != nil && *f.run().Reason == "checks failing, 2 times already", "reason = %v", f.run().Reason)
}

// TestARunBackFromItsAgentOnTheSameCommitIsToldAgain
func TestARunBackFromItsAgentOnTheSameCommitIsToldAgain(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventMerged, core.WatchEventClosed, core.WatchEventChecksFailed}, retry: true})
	f.open()
	f.gh.SetChecks(201, 1, 14, false)
	for index := 1; index <= 3; index++ {
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == index, "round %d: outcomes = %v", index, f.watchOutcomes())
		if index < 3 {
			// The agent finishes without pushing, so the head commit stays "a".
			coordCheck(t, f.run().StageID == "build", "round %d: stage = %s, want build", index, f.run().StageID)
			f.finish("done")
		}
	}
	coordCheck(t, f.run().State == core.RunStateHeld, "state = %v, want held", f.run().State)
	coordCheck(t, f.run().Reason != nil && *f.run().Reason == "checks failing, 2 times already", "reason = %v", f.run().Reason)
}

// TestReviewsAfterArrivalAndPlainComments
func TestReviewsAfterArrivalAndPlainComments(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{})
	f.open()
	f.gh.Approve(201, "old", t0Before(1), nil)
	f.gh.AddPullComment(201, "old", t0Before(1))
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"is approved"})
	coordCheck(t, f.lastDetail() == "#201 is approved by old", "detail = %q", f.lastDetail())
	f.resume()
	if err := f.gh.CommentOnPullRequest(context.Background(), "acme/billing", 201, "agent report"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		f.check()
	}
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"is approved"})
	coordCheck(t, f.run().State == core.RunStateRunning, "state = %v, want running", f.run().State)
	f.gh.Approve(201, "alice", t0After(1), nil)
	f.gh.AddPullComment(201, "dave", t0After(2))
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"is approved", "is approved"})
	coordCheck(t, f.lastDetail() == "#201 is approved by alice", "detail = %q", f.lastDetail())
	f.resume()
	f.check()
	coordEqual(t, "watch outcomes", f.watchOutcomes(), []string{"is approved", "is approved", "a reviewer commented"})
	coordCheck(t, f.lastDetail() == "#201: dave commented", "detail = %q", f.lastDetail())
	f.resume()
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after comment resume = %v, want running", f.run().State)
	f.gh.Approve(201, "robot[bot]", time.Now(), nil)
	f.check()
	coordCheck(t, f.lastDetail() == "#201 is approved by robot[bot]", "detail = %q", f.lastDetail())
	f.resume()
	for _, author := range []string{"one", "two", "three"} {
		f.gh.AddPullComment(201, author, time.Now())
	}
	f.check()
	coordCheck(t, f.lastDetail() == "#201: 3 reviewers commented", "detail = %q", f.lastDetail())
	f.resume()
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state = %v, want running", f.run().State)
	f.gh.SetHead(201, new("b"))
	f.gh.Approve(201, "alice", time.Now(), nil)
	f.gh.AddPullComment(201, "dave", time.Now())
	f.check()
	coordCheck(t, len(f.watchOutcomes()) == 6, "outcomes = %v, want 6", f.watchOutcomes())
	f.resume()
	f.check()
	coordCheck(t, len(f.watchOutcomes()) == 7, "outcomes = %v, want 7", f.watchOutcomes())
}

// TestReviewObservationsSurviveRestartAndDismissedApproval
func TestReviewObservationsSurviveRestartAndDismissedApproval(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	f.open()
	f.check()
	f.gh.Approve(201, "alice", time.Now(), nil)
	f.check()
	f.resume()
	reloaded := f.restart()
	checkWatches(reloaded, true)
	coordCheck(t, reloaded.Runs()[0].State == core.RunStateRunning, "reloaded state = %v, want running", reloaded.Runs()[0].State)
	f.gh.Dismiss(201, "alice")
	checkWatches(reloaded, true)
	f.gh.Approve(201, "bob", time.Now(), nil)
	checkWatches(reloaded, true)
	coordCheck(t, coordLastDetail(reloaded.Runs()[0], "watch") == "#201 is approved by bob", "detail = %q", coordLastDetail(reloaded.Runs()[0], "watch"))
	coordCheck(t, countOutcome(coordWatchOutcomes(reloaded.Runs()[0]), "is approved") == 2, "is approved count != 2")
}

// countOutcome is how many of outcomes are name.
func countOutcome(outcomes []string, name string) int {
	count := 0
	for _, outcome := range outcomes {
		if outcome == name {
			count++
		}
	}
	return count
}

// t0Before and t0After are the clock start minus and plus n.
func t0Before(seconds int) time.Time {
	return sessionSampleT0.Add(-time.Duration(seconds) * time.Second)
}

func t0After(seconds int) time.Time { return sessionSampleT0.Add(time.Duration(seconds) * time.Second) }
