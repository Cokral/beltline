// What a ticket watch and intake read of an issue beyond the issue itself, and what a branch has to merge.

package github

import (
	"context"
	"strconv"
	"strings"
	"unicode"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// linkedIssueJSON is an issue as the dependency and sub-issue endpoints give it.
type linkedIssueJSON struct {
	Number        int        `json:"number"`
	Title         *string    `json:"title,omitzero"`
	State         *string    `json:"state,omitzero"`
	Labels        []nameJSON `json:"labels,omitzero"`
	RepositoryURL *string    `json:"repository_url,omitzero"`
}

// isOpen reports whether the issue is open.
func (j linkedIssueJSON) isOpen() bool {
	return strings.ToLower(core.ValueOr(j.State, "open")) == "open"
}

// ref is "#n" in repo, or "owner/repo#n" in another one.
func (j linkedIssueJSON) ref(repo string) string {
	other := repo
	if j.RepositoryURL != nil {
		var parts []string
		for _, p := range strings.Split(*j.RepositoryURL, "/") {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) > 2 {
			parts = parts[len(parts)-2:]
		}
		other = strings.Join(parts, "/")
	}
	if strings.EqualFold(other, repo) {
		return "#" + strconv.Itoa(j.Number)
	}
	return other + "#" + strconv.Itoa(j.Number)
}

// issueStateJSON is an issue's state and comments as gh writes them.
type issueStateJSON struct {
	State    *string `json:"state,omitzero"`
	Comments []struct {
		ID     *string `json:"id,omitzero"`
		Author *struct {
			Login *string `json:"login,omitzero"`
		} `json:"author,omitzero"`
		Body *string `json:"body,omitzero"`
	} `json:"comments,omitzero"`
}

// optional answers an API the repository or host doesn't have, such as issue
// dependencies on an older GitHub, with empty.
func optional[T any](empty T, call func() (T, error)) (T, error) {
	value, err := call()
	if err == nil {
		return value, nil
	}
	// A run that was stopped is no answer, even when its command line names a 404-like ref.
	if f, ok := failure(err); ok && !stoppedFailure(f.Message) && (strings.Contains(f.Message, "404") || strings.Contains(f.Message, "Not Found")) {
		return empty, nil
	}
	var zero T
	return zero, err
}

// stoppedFailure reports whether a failure's message says its command was stopped at
// its limit or cancelled, rather than that it answered (see shell.Result.MessageFor).
func stoppedFailure(message string) bool {
	return strings.HasSuffix(message, shell.TimedOutReason) || strings.HasSuffix(message, shell.CancelledReason)
}

// Blockers are the open issues that block number, as refs.
func (g GHCLI) Blockers(ctx context.Context, repo string, number int) ([]string, error) {
	return optional([]string{}, func() ([]string, error) {
		text, err := g.gh(ctx, []string{"api", "repos/" + repo + "/issues/" + strconv.Itoa(number) + "/dependencies/blocked_by?per_page=100"}, nil)
		if err != nil {
			return nil, err
		}
		var list []linkedIssueJSON
		if err := decodeGH(text, &list); err != nil {
			return nil, err
		}
		out := []string{}
		for _, j := range list {
			if j.isOpen() {
				out = append(out, j.ref(repo))
			}
		}
		return out, nil
	})
}

// IssueState reads an issue's status, comments and sub-issues.
func (g GHCLI) IssueState(ctx context.Context, repo string, number int) (core.TicketState, error) {
	text, err := g.gh(ctx, []string{"issue", "view", strconv.Itoa(number), "-R", repo, "--json", "state,comments"}, nil)
	if err != nil {
		return core.TicketState{}, err
	}
	var issue issueStateJSON
	if err := decodeGH(text, &issue); err != nil {
		return core.TicketState{}, err
	}
	children, err := optional([]linkedIssueJSON{}, func() ([]linkedIssueJSON, error) {
		text, err := g.gh(ctx, []string{"api", "repos/" + repo + "/issues/" + strconv.Itoa(number) + "/sub_issues?per_page=100"}, nil)
		if err != nil {
			return nil, err
		}
		var list []linkedIssueJSON
		err = decodeGH(text, &list)
		return list, err
	})
	if err != nil {
		return core.TicketState{}, err
	}
	open := strings.ToUpper(core.ValueOr(issue.State, "OPEN")) == "OPEN"
	status := "closed"
	if open {
		status = "open"
	}
	comments := []core.TicketStateComment{}
	for index, c := range issue.Comments {
		id := core.ValueOr(c.ID, strconv.Itoa(index))
		author := "someone"
		if c.Author != nil && c.Author.Login != nil {
			author = *c.Author.Login
		}
		comments = append(comments, core.TicketStateComment{ID: id, Author: author, Body: core.ValueOf(c.Body)})
	}
	kids := []core.TicketStateChild{}
	for _, c := range children {
		childStatus := "closed"
		if c.isOpen() {
			childStatus = "open"
		}
		title := core.ValueOr(c.Title, "#"+strconv.Itoa(c.Number))
		kids = append(kids, core.TicketStateChild{
			Ref: c.ref(repo), Title: title, Status: childStatus, IsOpen: c.isOpen(), Labels: compactNames(c.Labels, true),
		})
	}
	return core.TicketState{Status: status, IsOpen: open, Comments: comments, Children: kids}, nil
}

// CommitsAhead is how many commits head has that base doesn't. A branch that was
// never pushed has nothing to merge, so it is 0.
func (g GHCLI) CommitsAhead(ctx context.Context, repo, base, head string) (*int, error) {
	text, err := optional("0", func() (string, error) {
		return g.gh(ctx, []string{"api", "repos/" + repo + "/compare/" + base + "..." + head, "-q", ".ahead_by"}, nil)
	})
	if err != nil {
		return nil, err
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(text))
	if convErr != nil {
		return nil, nil
	}
	return &count, nil
}

// BranchExists reports whether branch is on the repository; nil when the name
// cannot be encoded for a URL.
func (g GHCLI) BranchExists(ctx context.Context, repo, branch string) (*bool, error) {
	encoded, ok := percentEncodeRef(branch)
	if !ok {
		return nil, nil
	}
	name, err := optional("", func() (string, error) {
		return g.gh(ctx, []string{"api", "repos/" + repo + "/branches/" + encoded, "-q", ".name"}, nil)
	})
	if err != nil {
		return nil, err
	}
	exists := strings.TrimSpace(name) != ""
	return &exists, nil
}

// percentEncodeRef percent-encodes a ref path the way Foundation's
// addingPercentEncoding(withAllowedCharacters:) does with alphanumerics and
// "-._~/": letters and digits (Unicode, as CharacterSet.alphanumerics), those
// punctuation marks, and nothing else.
func percentEncodeRef(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		if unicode.In(r, unicode.L, unicode.M, unicode.N) || strings.ContainsRune("-._~/", r) {
			b.WriteRune(r)
			continue
		}
		for _, c := range []byte(string(r)) {
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)|0x100, 16)[1:]))
		}
	}
	return b.String(), true
}
