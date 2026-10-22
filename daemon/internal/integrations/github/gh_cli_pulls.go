// The pull request part of the GHCLI adapter:
// the pull request JSON, its checks and reviews, and the pull request reads.

package github

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// pullCheckJSON is one entry of a pull request's status check rollup.
type pullCheckJSON struct {
	Status     *string `json:"status,omitzero"`
	Conclusion *string `json:"conclusion,omitzero"`
	State      *string `json:"state,omitzero"`
}

// finished reports whether the check has ended.
func (c pullCheckJSON) finished() bool {
	if c.Status != nil {
		return *c.Status == "COMPLETED"
	}
	return c.State != nil && *c.State != "PENDING" && *c.State != "EXPECTED"
}

// failed reports whether the check ended badly.
func (c pullCheckJSON) failed() bool {
	switch core.ValueOr(c.Conclusion, core.ValueOf(c.State)) {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
		return true
	}
	return false
}

// submittedReviewJSON is one review of a pull request as gh writes it.
type submittedReviewJSON struct {
	ID     *string `json:"id,omitzero"`
	Author *struct {
		Login *string `json:"login,omitzero"`
	} `json:"author,omitzero"`
	State  string `json:"state"`
	Commit *struct {
		Oid *string `json:"oid,omitzero"`
	} `json:"commit,omitzero"`
	SubmittedAt *string `json:"submittedAt,omitzero"`
}

// review is the core review this JSON reads as.
func (r submittedReviewJSON) review() core.PullRequestReview {
	state := core.PullRequestReviewStateOther
	switch r.State {
	case "APPROVED":
		state = core.PullRequestReviewStateApproved
	case "CHANGES_REQUESTED":
		state = core.PullRequestReviewStateChangesRequested
	case "COMMENTED":
		state = core.PullRequestReviewStateCommented
	case "DISMISSED":
		state = core.PullRequestReviewStateDismissed
	}
	author := "unknown"
	if r.Author != nil && r.Author.Login != nil {
		author = *r.Author.Login
	}
	var commit *string
	if r.Commit != nil {
		commit = r.Commit.Oid
	}
	return core.PullRequestReview{
		ID: r.ID, Author: author, State: state, Commit: commit,
		SubmittedAt: parseISODate(r.SubmittedAt),
	}
}

// parseISODate reads an ISO 8601 date with an offset and whole seconds, as
// ISO8601DateFormatter does by default; nil when it does not.
func parseISODate(text *string) *jsonx.Date {
	if text == nil || strings.Contains(*text, ".") {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *text)
	if err != nil {
		return nil
	}
	return &jsonx.Date{Time: t}
}

// pullJSON is gh's pull request object, as the pull request reads ask for it.
type pullJSON struct {
	Number            int                   `json:"number"`
	URL               string                `json:"url"`
	State             string                `json:"state"`
	HeadRefOid        *string               `json:"headRefOid,omitzero"`
	BaseRefName       *string               `json:"baseRefName,omitzero"`
	Mergeable         *string               `json:"mergeable,omitzero"`
	StatusCheckRollup []pullCheckJSON       `json:"statusCheckRollup,omitzero"`
	Reviews           []submittedReviewJSON `json:"reviews,omitzero"`
}

// pullRequest is the pull request this JSON reads as.
func (p pullJSON) pullRequest() core.PullRequest {
	state := core.PullRequestStateOpen
	switch strings.ToUpper(p.State) {
	case "MERGED":
		state = core.PullRequestStateMerged
	case "CLOSED":
		state = core.PullRequestStateClosed
	}
	rollup := p.StatusCheckRollup
	failures := 0
	allFinished := true
	for _, c := range rollup {
		if c.failed() {
			failures++
		}
		if !c.finished() {
			allFinished = false
		}
	}
	var checks *core.PullRequestCheckState
	var checkFailures, checkTotal *int
	if len(rollup) > 0 {
		var c core.PullRequestCheckState
		switch {
		case !allFinished:
			c = core.PullRequestCheckStatePending
		case failures > 0:
			c = core.PullRequestCheckStateFailing
		default:
			c = core.PullRequestCheckStatePassing
		}
		checks = &c
		f, total := failures, len(rollup)
		checkFailures, checkTotal = &f, &total
	}
	var conflicted *bool
	switch core.ValueOf(p.Mergeable) {
	case "CONFLICTING":
		conflicted = new(true)
	case "MERGEABLE":
		conflicted = new(false)
	}
	var reviews []core.PullRequestReview
	if p.Reviews != nil {
		reviews = make([]core.PullRequestReview, 0, len(p.Reviews))
		for _, r := range p.Reviews {
			reviews = append(reviews, r.review())
		}
	}
	return core.PullRequest{
		Number: p.Number, URL: p.URL, State: state, HeadSha: p.HeadRefOid, BaseRef: p.BaseRefName,
		Checks: checks, CheckFailures: checkFailures, CheckTotal: checkTotal, Conflicted: conflicted,
		Reviews: reviews,
	}
}

// pullFields is the --json list a pull request read asks for, given the events
// the watch waits on.
func pullFields(events map[core.WatchEvent]struct{}) string {
	fields := "number,url,state"
	if hasAny(events, core.WatchEventChecksFailed, core.WatchEventConflicted, core.WatchEventBehind, core.WatchEventApproved,
		core.WatchEventCommented, core.WatchEventChangesRequested, core.WatchEventNewChanges) {
		fields += ",headRefOid"
	}
	if has(events, core.WatchEventChecksFailed) {
		fields += ",statusCheckRollup"
	}
	if has(events, core.WatchEventConflicted) {
		fields += ",mergeable"
	}
	if has(events, core.WatchEventBehind) {
		fields += ",baseRefName"
	}
	if hasAnyIn(events, core.WatchEventReviewEvents) {
		fields += ",reviews"
	}
	return fields
}

func has(set map[core.WatchEvent]struct{}, e core.WatchEvent) bool {
	_, ok := set[e]
	return ok
}

func hasAny(set map[core.WatchEvent]struct{}, events ...core.WatchEvent) bool {
	return slices.ContainsFunc(events, func(e core.WatchEvent) bool { return has(set, e) })
}

// hasAnyIn reports whether the two sets share a member.
func hasAnyIn(a, b map[core.WatchEvent]struct{}) bool {
	for e := range a {
		if has(b, e) {
			return true
		}
	}
	return false
}

// subtractReviews removes the review events from events.
func subtractReviews(events map[core.WatchEvent]struct{}) map[core.WatchEvent]struct{} {
	out := map[core.WatchEvent]struct{}{}
	for e := range events {
		if !has(core.WatchEventReviewEvents, e) {
			out[e] = struct{}{}
		}
	}
	return out
}

// PullRequest reads a pull request with no events.
func (g GHCLI) PullRequest(ctx context.Context, repo string, number int) (core.PullRequest, error) {
	return g.PullRequestWithEvents(ctx, repo, number, map[core.WatchEvent]struct{}{})
}

// PullRequestOnHead reads the newest pull request from branch with no events.
func (g GHCLI) PullRequestOnHead(ctx context.Context, repo, branch string) (*core.PullRequest, error) {
	return g.PullRequestOnHeadWithEvents(ctx, repo, branch, map[core.WatchEvent]struct{}{})
}

// PullRequestWithEvents reads a pull request with the fields its events need.
func (g GHCLI) PullRequestWithEvents(ctx context.Context, repo string, number int, events map[core.WatchEvent]struct{}) (core.PullRequest, error) {
	basic := subtractReviews(events)
	text, err := g.gh(ctx, []string{"pr", "view", strconv.Itoa(number), "-R", repo, "--json", pullFields(basic)}, nil)
	if err != nil {
		return core.PullRequest{}, err
	}
	var first pullJSON
	if err := decodeGH(text, &first); err != nil {
		return core.PullRequest{}, err
	}
	pull := first.pullRequest()
	if pull.State != core.PullRequestStateOpen || !hasAnyIn(events, core.WatchEventReviewEvents) {
		return pull, nil
	}
	text, err = g.gh(ctx, []string{"pr", "view", strconv.Itoa(number), "-R", repo, "--json", pullFields(events)}, nil)
	if err != nil {
		return core.PullRequest{}, err
	}
	var read pullJSON
	if err := decodeGH(text, &read); err != nil {
		return core.PullRequest{}, err
	}
	return read.pullRequest(), nil
}

// PullRequestOnHeadWithEvents reads the newest pull request from head, whatever its
// state, with the fields its events need; nil when there is none.
func (g GHCLI) PullRequestOnHeadWithEvents(ctx context.Context, repo, head string, events map[core.WatchEvent]struct{}) (*core.PullRequest, error) {
	text, err := g.gh(ctx, []string{"pr", "list", "-R", repo, "--head", head, "--state", "all", "--limit", "1", "--json", pullFields(subtractReviews(events))}, nil)
	if err != nil {
		return nil, err
	}
	var list []pullJSON
	if err := decodeGH(text, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	pull := list[0].pullRequest()
	if pull.State != core.PullRequestStateOpen || !hasAnyIn(events, core.WatchEventReviewEvents) {
		return &pull, nil
	}
	text, err = g.gh(ctx, []string{"pr", "view", strconv.Itoa(pull.Number), "-R", repo, "--json", pullFields(events)}, nil)
	if err != nil {
		return nil, err
	}
	var read pullJSON
	if err := decodeGH(text, &read); err != nil {
		return nil, err
	}
	result := read.pullRequest()
	return &result, nil
}
