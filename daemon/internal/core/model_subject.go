package core

import (
	"fmt"
	"strconv"
	"strings"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// SubjectKind is the kind of thing a run is about.
type SubjectKind string

const (
	// SubjectKindIssue is a GitHub issue.
	SubjectKindIssue SubjectKind = "issue"
	// SubjectKindPullRequest is a pull request waiting for your review.
	SubjectKindPullRequest SubjectKind = "pullRequest"
	// SubjectKindJira is a Jira ticket.
	SubjectKindJira SubjectKind = "jira"
	// SubjectKindManual is a task you started.
	SubjectKindManual SubjectKind = "manual"
	// SubjectKindScheduled is a task a schedule fired.
	SubjectKindScheduled SubjectKind = "scheduled"
)

// AllSubjectKind lists every SubjectKind in declaration order (CaseIterable).
var AllSubjectKind = []SubjectKind{SubjectKindIssue, SubjectKindPullRequest, SubjectKindJira, SubjectKindManual, SubjectKindScheduled}

// Noun is what the app calls one, such as "issue" or "ticket".
func (k SubjectKind) Noun() string {
	switch k {
	case SubjectKindIssue:
		return "issue"
	case SubjectKindPullRequest:
		return "pull request"
	case SubjectKindJira:
		return "ticket"
	case SubjectKindManual, SubjectKindScheduled:
		return "task"
	}
	return ""
}

// ResourceRefKind is the case of a ResourceRef.
type ResourceRefKind string

const (
	// ResourceRefKindIssue is a GitHub issue: Repo and Number are set.
	ResourceRefKindIssue ResourceRefKind = "issue"
	// ResourceRefKindPullRequest is a GitHub pull request: Repo and Number are set.
	ResourceRefKindPullRequest ResourceRefKind = "pullRequest"
	// ResourceRefKindTicket is a Jira ticket: Key is set.
	ResourceRefKindTicket ResourceRefKind = "ticket"
)

// ResourceRef is something outside Beltline a run is about, which its agents work on.
//
// Encoding is the enum form:
// {"issue":{"repo":…,"number":…}}, {"pullRequest":{"repo":…,"number":…}} or
// {"ticket":{"key":…}}. MarshalJSON and UnmarshalJSON produce exactly that.
type ResourceRef struct {
	Kind ResourceRefKind
	// Repo and Number are the payload of an issue or pull request.
	Repo   string
	Number int
	// Key is the payload of a ticket.
	Key string
}

// MarshalJSON writes the ResourceRef in the enum form: one key, the kind, over its payload.
func (r ResourceRef) MarshalJSON() ([]byte, error) {
	switch r.Kind {
	case ResourceRefKindIssue, ResourceRefKindPullRequest:
		return jsonx.Marshal(map[string]map[string]any{string(r.Kind): {"repo": r.Repo, "number": r.Number}})
	case ResourceRefKindTicket:
		return jsonx.Marshal(map[string]map[string]any{string(r.Kind): {"key": r.Key}})
	}
	return nil, fmt.Errorf("core: resource ref: unknown kind %q", string(r.Kind))
}

// UnmarshalJSON reads the ResourceRef from the enum form, which has exactly one key.
func (r *ResourceRef) UnmarshalJSON(data []byte) error {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return fmt.Errorf("core: resource ref: expected one case object")
	}
	if len(outer) != 1 {
		return fmt.Errorf("core: resource ref: expected one case, found %d", len(outer))
	}
	for name, raw := range outer {
		kind := ResourceRefKind(name)
		switch kind {
		case ResourceRefKindIssue, ResourceRefKindPullRequest:
			var p struct {
				Repo   string `json:"repo"`
				Number int    `json:"number"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			*r = ResourceRef{Kind: kind, Repo: p.Repo, Number: p.Number}
		case ResourceRefKindTicket:
			var p struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			*r = ResourceRef{Kind: kind, Key: p.Key}
		default:
			return fmt.Errorf("core: resource ref: %q is not a case", name)
		}
	}
	return nil
}

// Subject is the thing a run works on: a GitHub issue, a pull request waiting
// for your review, a Jira ticket, or a task you started or a schedule fired.
type Subject struct {
	Kind SubjectKind `json:"kind"`
	// Repo is the repository the code work happens in.
	Repo string `json:"repo"`
	// Number is the issue or pull request number; 0 for the other kinds.
	Number int `json:"number"`
	// Key is the Jira key, or the id of a manual or scheduled task.
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	URL       string   `json:"url"`
	IsOpen    bool     `json:"isOpen"`
	Assignees []string `json:"assignees"`
	// Base is, for a pull request, the branch it merges into.
	Base *string `json:"base,omitzero"`
	// Occurrence is, for a pull request, the review request this run answers, so
	// asking again starts a new run.
	Occurrence *string `json:"occurrence,omitzero"`
	// Reviewer is the GitHub login whose review request this run answers.
	Reviewer *string `json:"reviewer,omitzero"`
	// Parent is the epic or parent issue it belongs to: a Jira key, or "#12" for
	// an issue in the same repository.
	Parent *string `json:"parent,omitzero"`
	// Blockers are the issues or tickets that block it and are still open, as their refs.
	Blockers []string `json:"blockers"`
}

// Subject's optional keys are omitted when nil. UnmarshalJSON defaults the
// kind to an issue and isOpen to true, and reads runs saved before there were
// other triggers, which only had issues.

// UnmarshalJSON reads a subject over those defaults.
func (s *Subject) UnmarshalJSON(data []byte) error {
	type plain Subject
	p := plain{Kind: SubjectKindIssue, IsOpen: true}
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*s = Subject(p)
	return nil
}

// Ref is how the app shows the subject: "#142", "KAN-9", "T3" or "09-29".
func (s Subject) Ref() string {
	switch s.Kind {
	case SubjectKindIssue, SubjectKindPullRequest:
		return "#" + strconv.Itoa(s.Number)
	case SubjectKindJira:
		return s.Key
	case SubjectKindManual:
		return "T" + s.Key
	case SubjectKindScheduled:
		// The first five characters are dropped by rune count;
		// the keys are ASCII dates, so this matches a character count.
		if runes := []rune(s.Key); len(runes) > 5 {
			return string(runes[5:])
		}
		return ""
	}
	return ""
}

// Slug is a name for branches and folders: "issue-142", "pr-57", "KAN-9",
// "task-3" or "2026-09-29".
func (s Subject) Slug() string {
	switch s.Kind {
	case SubjectKindIssue:
		return "issue-" + strconv.Itoa(s.Number)
	case SubjectKindPullRequest:
		return "pr-" + strconv.Itoa(s.Number)
	case SubjectKindJira:
		return s.Key
	case SubjectKindManual:
		return "task-" + s.Key
	case SubjectKindScheduled:
		return s.Key
	}
	return ""
}

// SessionPart is the subject's part of a tmux session name. Issues keep their
// bare number, as sessions were named before there were other kinds.
func (s Subject) SessionPart() string {
	if s.Kind == SubjectKindIssue {
		return strconv.Itoa(s.Number)
	}
	return s.Slug()
}

// About is the issue, pull request or ticket the run is about; nil for a task
// you started or a schedule fired, which exist only in Beltline.
func (s Subject) About() *ResourceRef {
	switch s.Kind {
	case SubjectKindIssue:
		return &ResourceRef{Kind: ResourceRefKindIssue, Repo: s.Repo, Number: s.Number}
	case SubjectKindPullRequest:
		return &ResourceRef{Kind: ResourceRefKindPullRequest, Repo: s.Repo, Number: s.Number}
	case SubjectKindJira:
		return &ResourceRef{Kind: ResourceRefKindTicket, Key: s.Key}
	case SubjectKindManual, SubjectKindScheduled:
		return nil
	}
	return nil
}

// Content is what an edit means for the run: the title and description.
func (s Subject) Content() string {
	return s.Title + "\n" + s.Body
}

// BaseHint is the branch the subject's `base:` label names, which its work
// starts from and its pull requests merge into, such as an epic's feature
// branch. The first such label with a value wins.
func (s Subject) BaseHint() *string {
	for _, label := range s.Labels {
		if !strings.HasPrefix(LowercasedFull(label), "base:") {
			continue
		}
		value := trimBlanks(label[len("base:"):])
		if value != "" {
			return &value
		}
	}
	return nil
}

// Clone is a deep copy of the subject, with its label, assignee and blocker lists.
func (s Subject) Clone() Subject {
	return cloneValue(s)
}
