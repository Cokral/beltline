package core

import (
	"strconv"
	"strings"
)

// watchNotesApproved is the note for approving reviewers. Copy from WatchNotes.seen.
const watchNotesApproved = "approved by "

// watchNotesChangesRequested is the note for reviewers asking for changes. Copy from WatchNotes.seen.
const watchNotesChangesRequested = " asked for changes"

// watchNotesConflicts is the note for merge conflicts. Copy from WatchNotes.seen.
const watchNotesConflicts = "merge conflicts"

// watchNotesChecksFailing is the note for failing checks. Copy from WatchNotes.seen.
const watchNotesChecksFailing = " checks failing"

// watchNotesBehind is the note for a branch behind its base. Copy from WatchNotes.seen.
const watchNotesBehind = " commits behind "

// watchNotesFallbackBase names the base when the pull request has none. Copy from WatchNotes.seen.
const watchNotesFallbackBase = "its base"

// WatchNotesSeen is what a watch has seen on a pull request, as one line: approvals,
// requested changes, conflicts, failing checks and a branch behind its base. Review
// bodies and plain comments never count. named prefixes the line with the repository
// and number. Nil when nothing has been seen.
func WatchNotesSeen(pull PullRequest, named bool) *string {
	var notes []string
	var approving, requesting []string
	for _, review := range pull.LatestReviews() {
		switch review.State {
		case PullRequestReviewStateApproved:
			approving = append(approving, review.Author)
		case PullRequestReviewStateChangesRequested:
			requesting = append(requesting, review.Author)
		}
	}
	if len(approving) > 0 {
		notes = append(notes, watchNotesApproved+strings.Join(approving, ", "))
	}
	if len(requesting) > 0 {
		notes = append(notes, strings.Join(requesting, ", ")+watchNotesChangesRequested)
	}
	if pull.Conflicted != nil && *pull.Conflicted {
		notes = append(notes, watchNotesConflicts)
	}
	if pull.Checks != nil && *pull.Checks == PullRequestCheckStateFailing {
		failures, total := 0, 0
		if pull.CheckFailures != nil {
			failures = *pull.CheckFailures
		}
		if pull.CheckTotal != nil {
			total = *pull.CheckTotal
		}
		notes = append(notes, strconv.Itoa(failures)+" of "+strconv.Itoa(total)+watchNotesChecksFailing)
	}
	if pull.Behind != nil && *pull.Behind > 0 {
		base := ValueOr(pull.BaseRef, watchNotesFallbackBase)
		notes = append(notes, strconv.Itoa(*pull.Behind)+watchNotesBehind+base)
	}
	if len(notes) == 0 {
		return nil
	}
	line := strings.Join(notes, ", ")
	if named {
		line = pull.Repo + "#" + strconv.Itoa(pull.Number) + ": " + line
	}
	return &line
}
