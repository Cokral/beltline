// Tests of the gh-backed watch reads: the field lists, decoding and comparisons, and the pull fields
// of old pinned stages and review priority.

package github

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// watchSet is a set of watch events, as the watch passes them to the reads.
func watchSet(events ...core.WatchEvent) map[core.WatchEvent]struct{} {
	set := map[core.WatchEvent]struct{}{}
	for _, e := range events {
		set[e] = struct{}{}
	}
	return set
}

// shellCalls is the commands a branchShell recorded, as executable and arguments.
func shellCalls(sh *branchShell) [][]string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return append([][]string(nil), sh.calls...)
}

// TestGhUsesOnlyRequestedFieldsAndComparesInTheRightDirection
func TestGhUsesOnlyRequestedFieldsAndComparesInTheRightDirection(t *testing.T) {
	t.Parallel()
	sh := &branchShell{replies: []shell.Result{
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN"}`},
		{Status: 0, Stdout: `[{"number":201,"url":"u","state":"OPEN","headRefOid":"abc","mergeable":"CONFLICTING"}]`},
		{Status: 0, Stdout: "3\n"},
	}}
	gh := NewGHCLI(sh)
	ctx := context.Background()
	old, err := gh.PullRequestWithEvents(ctx, "a/b", 201, watchSet(core.WatchEventMerged, core.WatchEventClosed))
	if err != nil || old.HeadSha != nil {
		t.Fatalf("old pull = %+v, %v; want no head sha", old, err)
	}
	conflict, err := gh.PullRequestOnHeadWithEvents(ctx, "a/b", "feature", watchSet(core.WatchEventConflicted))
	if err != nil || conflict == nil || conflict.Conflicted == nil || !*conflict.Conflicted {
		t.Fatalf("conflict = %+v, %v; want conflicted", conflict, err)
	}
	if conflict.HeadSha == nil || *conflict.HeadSha != "abc" {
		t.Fatalf("conflict head = %v, want abc", conflict.HeadSha)
	}
	if behind, err := gh.CommitsBehind(ctx, "a/b", "main", "abc"); err != nil || behind != 3 {
		t.Fatalf("behind = %d, %v; want 3", behind, err)
	}
	want := [][]string{
		{"gh", "pr", "view", "201", "-R", "a/b", "--json", "number,url,state"},
		{"gh", "pr", "list", "-R", "a/b", "--head", "feature", "--state", "all", "--limit", "1", "--json", "number,url,state,headRefOid,mergeable"},
		{"gh", "api", "repos/a/b/compare/main...abc", "-q", ".behind_by"},
	}
	if got := shellCalls(sh); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// TestGhBehindLookupAsksOnlyForWhatItNeeds
func TestGhBehindLookupAsksOnlyForWhatItNeeds(t *testing.T) {
	t.Parallel()
	sh := &branchShell{replies: []shell.Result{
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN","headRefOid":"abc","baseRefName":"main"}`},
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN","headRefOid":"abc","statusCheckRollup":[{"status":"COMPLETED","conclusion":"FAILURE"}]}`},
	}}
	gh := NewGHCLI(sh)
	ctx := context.Background()
	behind, err := gh.PullRequestWithEvents(ctx, "a/b", 201, watchSet(core.WatchEventBehind))
	if err != nil {
		t.Fatal(err)
	}
	if behind.HeadSha == nil || *behind.HeadSha != "abc" || behind.BaseRef == nil || *behind.BaseRef != "main" {
		t.Fatalf("behind pull = %+v, want head abc on main", behind)
	}
	if behind.Checks != nil || behind.Conflicted != nil {
		t.Fatalf("behind pull reads checks or mergeability: %+v", behind)
	}
	checks, err := gh.PullRequestWithEvents(ctx, "a/b", 201, watchSet(core.WatchEventChecksFailed))
	if err != nil {
		t.Fatal(err)
	}
	if checks.Checks == nil || *checks.Checks != core.PullRequestCheckStateFailing {
		t.Fatalf("checks = %v, want failing", checks.Checks)
	}
	if checks.CheckFailures == nil || *checks.CheckFailures != 1 {
		t.Fatalf("check failures = %v, want 1", checks.CheckFailures)
	}
	if checks.BaseRef != nil {
		t.Fatalf("checks pull reads the base: %v", *checks.BaseRef)
	}
	want := [][]string{
		{"gh", "pr", "view", "201", "-R", "a/b", "--json", "number,url,state,headRefOid,baseRefName"},
		{"gh", "pr", "view", "201", "-R", "a/b", "--json", "number,url,state,headRefOid,statusCheckRollup"},
	}
	if got := shellCalls(sh); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// TestGhReviewsDecodeAndTerminalPullsNeverReadReviews
func TestGhReviewsDecodeAndTerminalPullsNeverReadReviews(t *testing.T) {
	t.Parallel()
	sh := &branchShell{replies: []shell.Result{
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN"}`},
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN","headRefOid":"abc","reviews":[{"id":"R1","author":{"login":"alice"},"state":"APPROVED","commit":{"oid":"abc"},"submittedAt":"2026-01-01T12:00:00Z"},{"id":"R2","author":{"login":"carol"},"state":"CHANGES_REQUESTED","commit":{"oid":"abc"}},{"id":"R3","state":"DISMISSED","commit":{"oid":"old"}},{"id":"R4","state":"PENDING"}]}`},
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"MERGED"}`},
		{Status: 0, Stdout: `[{"number":201,"url":"u","state":"CLOSED"}]`},
	}}
	gh := NewGHCLI(sh)
	ctx := context.Background()
	pull, err := gh.PullRequestWithEvents(ctx, "a/b", 201, core.WatchEventReviewEvents)
	if err != nil {
		t.Fatal(err)
	}
	var states []core.PullRequestReviewState
	for _, review := range pull.Reviews {
		states = append(states, review.State)
	}
	if want := []core.PullRequestReviewState{core.PullRequestReviewStateApproved, core.PullRequestReviewStateChangesRequested,
		core.PullRequestReviewStateDismissed, core.PullRequestReviewStateOther}; !reflect.DeepEqual(states, want) {
		t.Fatalf("review states = %v, want %v", states, want)
	}
	if len(pull.Reviews) == 0 || pull.Reviews[0].ID == nil || *pull.Reviews[0].ID != "R1" || pull.Reviews[0].SubmittedAt == nil {
		t.Fatalf("first review = %+v, want R1 with a submission time", pull.Reviews)
	}
	if n := len(pull.HeadReviews()); n != 2 {
		t.Fatalf("head reviews = %d, want 2", n)
	}
	if merged, err := gh.PullRequestWithEvents(ctx, "a/b", 201, core.WatchEventReviewEvents); err != nil || merged.State != core.PullRequestStateMerged {
		t.Fatalf("merged pull = %+v, %v", merged, err)
	}
	if closed, err := gh.PullRequestOnHeadWithEvents(ctx, "a/b", "feature", core.WatchEventReviewEvents); err != nil || closed == nil || closed.State != core.PullRequestStateClosed {
		t.Fatalf("closed pull = %+v, %v", closed, err)
	}
	calls := shellCalls(sh)
	if len(calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(calls))
	}
	if last := calls[1][len(calls[1])-1]; last != "number,url,state,headRefOid,reviews" {
		t.Fatalf("review read fields = %q", last)
	}
	for _, call := range calls[2:] {
		if last := call[len(call)-1]; last != "number,url,state" {
			t.Fatalf("terminal read fields = %q, want number,url,state", last)
		}
	}
}

// TestGhNewChangesReadsOnlyTheHeadForEitherLookup
func TestGhNewChangesReadsOnlyTheHeadForEitherLookup(t *testing.T) {
	t.Parallel()
	sh := &branchShell{replies: []shell.Result{
		{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN","headRefOid":"abc"}`},
		{Status: 0, Stdout: `[{"number":201,"url":"u","state":"OPEN","headRefOid":"def"}]`},
	}}
	gh := NewGHCLI(sh)
	ctx := context.Background()
	byNumber, err := gh.PullRequestWithEvents(ctx, "a/b", 201, watchSet(core.WatchEventNewChanges))
	if err != nil {
		t.Fatal(err)
	}
	if byNumber.HeadSha == nil || *byNumber.HeadSha != "abc" {
		t.Fatalf("head = %v, want abc", byNumber.HeadSha)
	}
	if byNumber.Checks != nil || byNumber.Conflicted != nil || byNumber.BaseRef != nil || byNumber.Reviews != nil {
		t.Fatalf("new changes read more than the head: %+v", byNumber)
	}
	byBranch, err := gh.PullRequestOnHeadWithEvents(ctx, "a/b", "feature", watchSet(core.WatchEventNewChanges))
	if err != nil || byBranch == nil || byBranch.HeadSha == nil || *byBranch.HeadSha != "def" {
		t.Fatalf("by branch = %+v, %v; want head def", byBranch, err)
	}
	if got := pullFields(watchSet(core.WatchEventNewChanges)); got != "number,url,state,headRefOid" {
		t.Fatalf("fields = %q", got)
	}
	want := [][]string{
		{"gh", "pr", "view", "201", "-R", "a/b", "--json", "number,url,state,headRefOid"},
		{"gh", "pr", "list", "-R", "a/b", "--head", "feature", "--state", "all", "--limit", "1", "--json", "number,url,state,headRefOid"},
	}
	if got := shellCalls(sh); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// TestUnknownMergeabilityAndInvalidComparisonsAreNotRecovery
func TestUnknownMergeabilityAndInvalidComparisonsAreNotRecovery(t *testing.T) {
	t.Parallel()
	var raw pullJSON
	if err := json.Unmarshal([]byte(`{"number":201,"url":"u","state":"OPEN","mergeable":"UNKNOWN"}`), &raw); err != nil {
		t.Fatal(err)
	}
	if pull := raw.pullRequest(); pull.Conflicted != nil {
		t.Fatalf("unknown mergeability read as conflicted = %v", *pull.Conflicted)
	}
	gh := NewGHCLI(&branchShell{replies: []shell.Result{{Status: 0, Stdout: "null"}}})
	if _, err := gh.CommitsBehind(context.Background(), "a/b", "main", "a"); !isFailure(err) {
		t.Fatalf("an invalid comparison was not refused: %v", err)
	}
}

// TestNoneAddsNoGitHubFields
func TestNoneAddsNoGitHubFields(t *testing.T) {
	t.Parallel()
	if got := pullFields(watchSet(core.WatchEventNone)); got != "number,url,state" {
		t.Fatalf("none fields = %q", got)
	}
	if pullFields(watchSet(core.WatchEventMerged, core.WatchEventClosed, core.WatchEventNone)) != pullFields(watchSet(core.WatchEventMerged, core.WatchEventClosed)) {
		t.Fatal("none adds fields to merged and closed")
	}
	if pullFields(watchSet(core.WatchEventApproved, core.WatchEventNone)) != pullFields(watchSet(core.WatchEventApproved)) {
		t.Fatal("none adds fields to approved")
	}
	sh := &branchShell{replies: []shell.Result{{Status: 0, Stdout: `{"number":201,"url":"u","state":"OPEN"}`}}}
	pull, err := NewGHCLI(sh).PullRequestWithEvents(context.Background(), "a/b", 201, watchSet(core.WatchEventNone, core.WatchEventMerged))
	if err != nil {
		t.Fatal(err)
	}
	if pull.Reviews != nil || pull.Checks != nil {
		t.Fatalf("none read reviews or checks: %+v", pull)
	}
	want := [][]string{{"gh", "pr", "view", "201", "-R", "a/b", "--json", "number,url,state"}}
	if got := shellCalls(sh); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// TestPullFieldsForOldPinnedStages checks the pull fields of old pinned stages:
// they ignore new signals and make no comparison.
func TestPullFieldsForOldPinnedStages(t *testing.T) {
	t.Parallel()
	if got := pullFields(watchSet(core.WatchEventMerged, core.WatchEventClosed)); got != "number,url,state" {
		t.Fatalf("merged and closed fields = %q", got)
	}
	if got := pullFields(watchSet(core.WatchEventChecksFailed)); slices.Contains(strings.Split(got, ","), "mergeable") {
		t.Fatalf("checks fields %q read mergeability", got)
	}
}

// TestPullFieldsForReviewPriority checks the pull fields of review priority and details.
func TestPullFieldsForReviewPriority(t *testing.T) {
	t.Parallel()
	if got := pullFields(watchSet(core.WatchEventApproved)); !slices.Contains(strings.Split(got, ","), "reviews") {
		t.Fatalf("approved fields %q do not read reviews", got)
	}
	if got := pullFields(watchSet(core.WatchEventCommented)); !slices.Contains(strings.Split(got, ","), "headRefOid") {
		t.Fatalf("commented fields %q do not read the head", got)
	}
}
