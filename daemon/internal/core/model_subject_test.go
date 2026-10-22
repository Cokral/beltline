package core

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestSubjectDecodesLegacyDefaults checks the defaults of Subject.init(from:): a
// subject saved with only its repository and title is an open issue with no
// labels, assignees or blockers.
func TestSubjectDecodesLegacyDefaults(t *testing.T) {
	t.Parallel()
	var s Subject
	testDecode(t, `{"repo":"acme/web","title":"Export"}`, &s)
	want := Subject{Kind: SubjectKindIssue, Repo: "acme/web", Title: "Export", IsOpen: true}
	if d := testDiffEmpty(want, s); d != "" {
		t.Fatalf("legacy subject: %s", d)
	}
	if s.Base != nil || s.Occurrence != nil || s.Reviewer != nil || s.Parent != nil {
		t.Errorf("optional keys not nil: %+v", s)
	}
	var closed Subject
	testDecode(t, `{"repo":"acme/web","title":"T","isOpen":false,"kind":"jira","key":"PAY-1"}`, &closed)
	if closed.IsOpen || closed.Kind != SubjectKindJira || closed.Key != "PAY-1" {
		t.Errorf("explicit values lost: %+v", closed)
	}
}

// TestSubjectBaseHintFromLabels checks the base: label, case-insensitively, with
// a trimmed value, and that an empty value does not count.
func TestSubjectBaseHintFromLabels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		labels []string
		want   *string
	}{
		{[]string{"agent", "base:beltline/KAN-1"}, new("beltline/KAN-1")},
		{[]string{"BASE:  develop "}, new("develop")},
		{[]string{"base:", "base:main"}, new("main")},
		{[]string{"based:main"}, nil},
		{nil, nil},
	}
	for _, tc := range cases {
		got := Subject{Labels: tc.labels}.BaseHint()
		switch {
		case got == nil && tc.want == nil:
		case got == nil || tc.want == nil || *got != *tc.want:
			t.Errorf("labels %v: base hint %v, want %v", tc.labels, got, tc.want)
		}
	}
}

// TestSubjectAboutAndSessionPart checks which subjects are about a GitHub or
// Jira thing, and the part of a session name each one uses.
func TestSubjectAboutAndSessionPart(t *testing.T) {
	t.Parallel()
	issue := Subject{Kind: SubjectKindIssue, Repo: "a/b", Number: 142, Title: "T"}
	if got := issue.About(); got == nil || *got != (ResourceRef{Kind: ResourceRefKindIssue, Repo: "a/b", Number: 142}) {
		t.Errorf("issue about = %+v", got)
	}
	if got := issue.SessionPart(); got != "142" {
		t.Errorf("issue session part = %q, want 142", got)
	}
	pull := Subject{Kind: SubjectKindPullRequest, Repo: "a/b", Number: 57, Title: "T"}
	if got := pull.SessionPart(); got != "pr-57" {
		t.Errorf("pull session part = %q, want pr-57", got)
	}
	task := Subject{Kind: SubjectKindManual, Key: "3", Title: "T"}
	if got := task.About(); got != nil {
		t.Errorf("a task is about %+v, want nothing", got)
	}
	if got := (Subject{Kind: SubjectKindJira, Key: "PAY-9"}).About(); got == nil || got.Key != "PAY-9" {
		t.Errorf("ticket about = %+v", got)
	}
	if got := (Subject{Title: "Export", Body: "Add it."}).Content(); got != "Export\nAdd it." {
		t.Errorf("content = %q", got)
	}
}

// TestSubjectRoundTripKeepsOptionalFields checks a pull request's optional
// fields survive a round trip, and that an absent one stays nil.
func TestSubjectRoundTripKeepsOptionalFields(t *testing.T) {
	t.Parallel()
	subject := Subject{Kind: SubjectKindPullRequest, Repo: "acme/web", Number: 57, Title: "Move", Base: new("develop"),
		Occurrence: new("100"), Reviewer: new("me"), Blockers: []string{"#3"}, Labels: []string{"agent"}, IsOpen: true}
	data, err := jsonx.Marshal(subject)
	if err != nil {
		t.Fatal(err)
	}
	var back Subject
	testDecode(t, string(data), &back)
	if d := testDiffEmpty(subject, back); d != "" {
		t.Fatalf("round trip: %s", d)
	}
	if back.Parent != nil {
		t.Errorf("parent = %v, want nil", *back.Parent)
	}
}

// TestScheduledRefCountsRunes checks that a scheduled key's ref drops its first
// five runes, not bytes: the ref of "ü2026-09-29" is "-09-29".
// Keys are ASCII dates, so the count matches a character count.
func TestScheduledRefCountsRunes(t *testing.T) {
	t.Parallel()
	if got := (Subject{Kind: SubjectKindScheduled, Key: "ü2026-09-29"}).Ref(); got != "-09-29" {
		t.Errorf("ref = %q, want -09-29", got)
	}
	if got := (Subject{Kind: SubjectKindScheduled, Key: "2026"}).Ref(); got != "" {
		t.Errorf("short key ref = %q, want empty", got)
	}
}

// DefaultSubject is an open issue subject in repo with title and every other
// field empty, for tests to fill in.
func DefaultSubject(repo, title string) Subject {
	return Subject{Kind: SubjectKindIssue, Repo: repo, Title: title, IsOpen: true}
}
