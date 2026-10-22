// Tests of reviews and approvals on a watch: which reviews count after arrival, the latest state of
// each reviewer, approvals that qualify on any commit, and how a dismissal or a higher event leaves them.

package coordinator

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestFirstPollIncludesReviewsSubmittedAfterArrival
func TestFirstPollIncludesReviewsSubmittedAfterArrival(t *testing.T) {
	t.Parallel()
	started := sessionSampleT0
	type reviewCase struct {
		name   string
		date   *time.Time
		commit string
	}
	cases := []reviewCase{
		{name: "before", date: new(started.Add(-time.Second)), commit: "a"},
		{name: "at", date: new(started), commit: "a"},
		{name: "after", date: new(started.Add(time.Second)), commit: "a"},
		{name: "missing", date: nil, commit: "a"},
		{name: "earlier", date: new(started.Add(-time.Second)), commit: "old"},
	}
	for _, reviewed := range cases {
		f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
		f.open()
		if reviewed.commit == "old" {
			f.gh.SetHead(201, new("b"))
		}
		commit := reviewed.commit
		if reviewed.commit == "old" {
			commit = "a"
		}
		f.gh.SetReview(201, reviewed.name, core.PullRequestReviewStateApproved, new(commit), reviewed.date, new(reviewed.name))
		f.check()
		coordEqual(t, reviewed.name+" outcomes", f.watchOutcomes(), []string{"is approved"})
		coordCheck(t, f.lastDetail() == "#201 is approved by "+reviewed.name, "%s: detail = %q", reviewed.name, f.lastDetail())
	}
	saved := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	saved.open()
	saved.gh.SetHead(201, new("b"))
	saved.gh.SetReview(201, "bob", core.PullRequestReviewStateApproved, new("a"), new(started.Add(-time.Second)), new("saved"))
	restarted := saved.restart()
	checkWatches(restarted, true)
	coordCheck(t, coordLastDetail(restarted.Runs()[0], "watch") == "#201 is approved by bob", "restarted detail differs")
	coordCheck(t, restarted.Runs()[0].State == core.RunStateHeld, "restarted state = %v, want held", restarted.Runs()[0].State)
}

// TestChangesRequestedIsLatestReviewerState
func TestChangesRequestedIsLatestReviewerState(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventChangesRequested}})
	f.open()
	f.gh.RequestChanges(201, "carol", t0Before(1))
	f.check()
	coordEqual(t, "outcomes", f.watchOutcomes(), []string{"changes requested"})
	coordCheck(t, f.lastDetail() == "#201: carol asked for changes", "detail = %q", f.lastDetail())
	coordCheck(t, f.stored()[0].State == core.RunStateHeld, "saved state is not held")
	f.resume()
	f.gh.RequestChanges(201, "carol", time.Now())
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after repeat = %v, want running", f.run().State)
	f.gh.AddPullComment(201, "carol", t0Before(1))
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after comment = %v, want running", f.run().State)
	f.gh.RequestChanges(201, "carol", time.Now())
	f.check()
	coordEqual(t, "outcomes", f.watchOutcomes(), []string{"changes requested", "changes requested"})
	f.resume()
	f.gh.Dismiss(201, "carol")
	f.check()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after dismissal = %v, want running", f.run().State)
	f.gh.RequestChanges(201, "carol", time.Now())
	f.check()
	coordCheck(t, len(f.watchOutcomes()) == 3, "outcomes = %v, want 3", f.watchOutcomes())
}

// TestOnlyACurrentApprovalQualifiesAndADismissalStaysQuiet
func TestOnlyACurrentApprovalQualifiesAndADismissalStaysQuiet(t *testing.T) {
	t.Parallel()
	for _, state := range []core.PullRequestReviewState{core.PullRequestReviewStateDismissed, core.PullRequestReviewStateCommented, core.PullRequestReviewStateChangesRequested, core.PullRequestReviewStateOther} {
		f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved, core.WatchEventCommented, core.WatchEventChangesRequested}})
		f.open()
		f.gh.SetHead(201, new("b"))
		f.gh.SetReview(201, "alice", state, new("a"), new(sessionSampleT0), new("old-"+string(state)))
		f.check()
		coordCheck(t, len(f.watchOutcomes()) == 0, "%s: outcomes = %v, want none", state, f.watchOutcomes())
		coordCheck(t, f.run().State == core.RunStateRunning, "%s: state = %v, want running", state, f.run().State)
	}

	comments := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventCommented, core.WatchEventChangesRequested}})
	comments.open()
	comments.gh.SetHead(201, new("b"))
	comments.gh.SetReview(201, "dave", core.PullRequestReviewStateCommented, new("a"), new(sessionSampleT0), new("old-comment"))
	comments.gh.SetReview(201, "carol", core.PullRequestReviewStateChangesRequested, new("a"), new(sessionSampleT0), new("old-request"))
	comments.check()
	coordCheck(t, len(comments.watchOutcomes()) == 0, "comments: outcomes = %v, want none", comments.watchOutcomes())
	comments.gh.AddPullComment(201, "dave", time.Now())
	comments.check()
	coordCheck(t, comments.lastDetail() == "#201: dave commented", "comments: detail = %q", comments.lastDetail())

	dismissed := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	dismissed.open()
	dismissed.check()
	dismissed.gh.Approve(201, "alice", time.Now(), new("a"))
	dismissed.check()
	coordEqual(t, "dismissed outcomes", dismissed.watchOutcomes(), []string{"is approved"})
	dismissed.resume()
	dismissed.gh.Dismiss(201, "alice")
	dismissed.gh.SetHead(201, new("b"))
	dismissed.check()
	coordEqual(t, "dismissed outcomes after dismissal", dismissed.watchOutcomes(), []string{"is approved"})
	coordCheck(t, dismissed.run().State == core.RunStateRunning, "dismissed state = %v, want running", dismissed.run().State)

	revisited := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	revisited.setOutcome(core.WatchEventApproved, gotoThen("build"))
	revisited.open()
	revisited.check()
	revisited.gh.Approve(201, "alice", time.Now(), new("a"))
	revisited.check()
	revisited.gh.Dismiss(201, "alice")
	revisited.gh.SetHead(201, new("b"))
	revisited.finish("done")
	revisited.check()
	coordEqual(t, "revisited outcomes", revisited.watchOutcomes(), []string{"is approved"})
	coordCheck(t, revisited.run().State == core.RunStateRunning, "revisited state = %v, want running", revisited.run().State)
}

// TestAnApprovalFiresAgainForAHeadChangeARetryAndAReturn
func TestAnApprovalFiresAgainForAHeadChangeARetryAndAReturn(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	f.open()
	f.check()
	f.gh.Approve(201, "alice", time.Now(), new("a"))
	f.check()
	coordEqual(t, "outcomes", f.watchOutcomes(), []string{"is approved"})
	coordCheck(t, f.lastDetail() == "#201 is approved by alice", "detail = %q", f.lastDetail())
	coordCheck(t, coordLastDetail(f.stored()[0], "watch") == "#201 is approved by alice", "saved detail differs")
	f.resume()
	f.check()
	coordEqual(t, "outcomes after resume", f.watchOutcomes(), []string{"is approved"})
	coordCheck(t, f.run().State == core.RunStateRunning, "state = %v, want running", f.run().State)
	reloaded := f.restart()
	checkWatches(reloaded, true)
	coordCheck(t, countOutcome(coordWatchOutcomes(reloaded.Runs()[0]), "is approved") == 1, "reloaded: approvals != 1")
	f.gh.SetHead(201, new("b"))
	checkWatches(reloaded, true)
	coordCheck(t, coordLastDetail(reloaded.Runs()[0], "watch") == "#201 is approved by alice", "reloaded detail after head change differs")
	coordCheck(t, countOutcome(coordWatchOutcomes(reloaded.Runs()[0]), "is approved") == 2, "reloaded: approvals != 2 after head change")
	coordResume(reloaded, f.run().ID)
	f.gh.SetHead(201, new("a"))
	checkWatches(reloaded, true)
	coordCheck(t, countOutcome(coordWatchOutcomes(reloaded.Runs()[0]), "is approved") == 3, "reloaded: approvals != 3 after return")
	coordResume(reloaded, f.run().ID)
	checkWatches(reloaded, true)
	coordCheck(t, countOutcome(coordWatchOutcomes(reloaded.Runs()[0]), "is approved") == 3, "reloaded: approvals changed on a repeat")

	again := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	again.setOutcome(core.WatchEventApproved, retryThen())
	again.open()
	again.check()
	again.gh.Approve(201, "alice", time.Now(), new("a"))
	again.check()
	coordEqual(t, "retry outcomes", again.watchOutcomes(), []string{"is approved"})
	coordCheck(t, again.run().State == core.RunStateRunning, "retry state = %v, want running", again.run().State)
	again.check()
	coordEqual(t, "retry outcomes after poll", again.watchOutcomes(), []string{"is approved", "is approved"})
	coordCheck(t, lastOutcomeDetail(again.run(), "is approved") == "#201 is approved by alice", "retry detail = %q", lastOutcomeDetail(again.run(), "is approved"))

	returned := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	returned.setOutcome(core.WatchEventApproved, gotoThen("build"))
	returned.open()
	returned.check()
	returned.gh.Approve(201, "alice", time.Now(), new("a"))
	returned.check()
	coordCheck(t, returned.run().StageID == "build", "returned stage = %s, want build", returned.run().StageID)
	returned.finish("done")
	coordCheck(t, returned.run().StageID == "watch", "returned stage after build = %s, want watch", returned.run().StageID)
	returned.check()
	coordEqual(t, "returned outcomes", returned.watchOutcomes(), []string{"is approved", "is approved"})
	coordCheck(t, lastOutcomeDetail(returned.run(), "is approved") == "#201 is approved by alice", "returned detail = %q", lastOutcomeDetail(returned.run(), "is approved"))

	second := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	second.open()
	second.check()
	second.gh.Approve(201, "alice", time.Now(), new("a"))
	second.check()
	second.resume()
	second.gh.Approve(201, "alice", time.Now(), new("a"))
	second.check()
	coordEqual(t, "second outcomes", second.watchOutcomes(), []string{"is approved", "is approved"})
	coordCheck(t, second.lastDetail() == "#201 is approved by alice", "second detail = %q", second.lastDetail())

	replaced := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	replaced.open()
	replaced.check()
	replaced.gh.Approve(201, "alice", time.Now(), nil)
	replaced.check()
	replaced.resume()
	branch := *replaced.run().Branch
	replaced.gh.PutPull(pullIn("acme/billing", 201, "a", ""), "elsewhere")
	pull202 := pullIn("acme/billing", 202, "a", "")
	pull202.Reviews = []core.PullRequestReview{{ID: new("carol"), Author: "carol", State: core.PullRequestReviewStateApproved, Commit: new("old")}}
	replaced.gh.PutPull(pull202, branch)
	replaced.update(func(run *core.Run) { run.PullRequests = nil })
	replaced.check()
	coordCheck(t, replaced.lastDetail() == "#202 is approved by carol", "replaced detail = %q", replaced.lastDetail())
}

// TestAMissingHeadDoesNotRearmApprovalsAndTheFirstHeadIsABaseline
func TestAMissingHeadDoesNotRearmApprovalsAndTheFirstHeadIsABaseline(t *testing.T) {
	t.Parallel()
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	f.open()
	f.check()
	f.gh.Approve(201, "alice", time.Now(), new("a"))
	f.check()
	coordEqual(t, "outcomes", f.watchOutcomes(), []string{"is approved"})
	f.resume()
	f.gh.SetHead(201, nil)
	f.check()
	f.gh.SetHead(201, new(""))
	f.check()
	f.gh.SetHead(201, new("a"))
	f.check()
	coordEqual(t, "outcomes after missing heads", f.watchOutcomes(), []string{"is approved"})
	coordCheck(t, f.run().State == core.RunStateRunning, "state = %v, want running", f.run().State)

	first := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{core.WatchEventApproved}})
	first.open()
	first.gh.SetHead(201, nil)
	first.gh.SetReview(201, "alice", core.PullRequestReviewStateApproved, new("a"), nil, new("early"))
	first.check()
	coordEqual(t, "first outcomes", first.watchOutcomes(), []string{"is approved"})
	first.resume()
	first.check()
	coordEqual(t, "first outcomes after resume", first.watchOutcomes(), []string{"is approved"})
	first.gh.SetHead(201, new("a"))
	first.check()
	coordEqual(t, "first outcomes after head", first.watchOutcomes(), []string{"is approved"})
	coordCheck(t, first.run().State == core.RunStateRunning, "first state = %v, want running", first.run().State)
}
