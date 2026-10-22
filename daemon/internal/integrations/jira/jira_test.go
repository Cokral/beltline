// Tests of the Jira parsing, and the opt-in live checks of read-only acli calls.

package jira

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// same reports whether got equals want, as the tests compare values.
func same(want, got any) bool { return testkit.Diff(want, got) == "" }

func TestJiraRichTextBecomesText(t *testing.T) {
	t.Parallel()
	const text = `{"type":"doc","version":1,"content":[
	  {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Refunds"}]},
	  {"type":"paragraph","content":[{"type":"text","text":"Call "},{"type":"text","text":"refund()","marks":[{"type":"code"}]},{"type":"hardBreak"},{"type":"text","text":"See ","marks":[]},{"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://x.test"}}]}]},
	  {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"partial"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"full"}]}]}]},
	  {"type":"codeBlock","content":[{"type":"text","text":"x = 1"}]}
	]}`
	var doc any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	want := "## Refunds\n\nCall `refund()`\nSee [docs](https://x.test)\n\n- partial\n- full\n\n```\nx = 1\n```"
	if got := ADFText(doc); got != want {
		t.Fatalf("ADFText = %q, want %q", got, want)
	}
	if got := ADFText(nil); got != "" {
		t.Fatalf("ADFText(nil) = %q", got)
	}
	if got := ADFText("plain"); got != "plain" {
		t.Fatalf("ADFText(plain) = %q", got)
	}
}

func TestAJiraTicketIsOpenUntilItsStatusIsDone(t *testing.T) {
	t.Parallel()
	object := func(category string) map[string]any {
		return map[string]any{"key": "KAN-9", "fields": map[string]any{
			"summary": "Simplify", "labels": []any{"agent"},
			"status": map[string]any{"name": "Closed", "statusCategory": map[string]any{"key": category}},
		}}
	}
	open, ok := AcliJiraTicket(object("indeterminate"), "acme.atlassian.net")
	want := core.Subject{
		Kind: core.SubjectKindJira, Repo: "", Key: "KAN-9", Title: "Simplify", Labels: []string{"agent"},
		URL: "https://acme.atlassian.net/browse/KAN-9", IsOpen: true, Assignees: []string{}, Blockers: []string{},
	}
	if !ok || !same(want, open) {
		t.Fatal(testkit.Diff(want, open))
	}
	if done, ok := AcliJiraTicket(object("done"), ""); !ok || done.IsOpen {
		t.Fatalf("done ticket open or unread: %+v", done)
	}
	if done, _ := AcliJiraTicket(object("done"), ""); done.URL != "" {
		t.Fatalf("url without a site: %q", done.URL)
	}
}

// acliStandIn answers like acli, whose search refuses the fields only view gives.
type acliStandIn struct {
	mu    sync.Mutex
	calls [][]string
}

func (a *acliStandIn) Run(_ context.Context, _ string, arguments []string, _ string, _ *string) shell.Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, arguments)
	prefix := arguments
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	switch {
	case reflect.DeepEqual(prefix, []string{"jira", "auth", "status"}):
		return shell.Result{Status: 0, Stdout: "Site: acme.atlassian.net\n"}
	case reflect.DeepEqual(prefix, []string{"jira", "workitem", "search"}):
		fields := ""
		for i, arg := range arguments {
			if arg == "--fields" && i+1 < len(arguments) {
				fields = arguments[i+1]
			}
		}
		if strings.Contains(fields, "parent") || strings.Contains(fields, "issuelinks") {
			return shell.Result{Status: 1, Stderr: "Error: fields 'parent, issuelinks' are not allowed"}
		}
		return shell.Result{Status: 0, Stdout: `[{"key":"KAN-23","fields":{"summary":"Marketplace"}}]`}
	case reflect.DeepEqual(prefix, []string{"jira", "workitem", "view"}):
		return shell.Result{Status: 0, Stdout: `{"key":"KAN-23","fields":{"summary":"Marketplace","labels":["biggame"],
		 "status":{"name":"To Do","statusCategory":{"key":"new"}},"parent":{"key":"KAN-10"},
		 "issuelinks":[{"type":{"inward":"is blocked by"},"inwardIssue":{"key":"KAN-5","fields":{"status":{"statusCategory":{"key":"new"}}}}}]}}`}
	}
	return shell.Result{Status: 1}
}

func TestAJiraSearchReadsEachTicketForWhatTheSearchCannotGive(t *testing.T) {
	t.Parallel()
	shellStandIn := &acliStandIn{}
	tickets, err := NewAcliJira(shellStandIn).Search(context.Background(), "labels = biggame")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Key != "KAN-23" {
		t.Fatalf("tickets %+v", tickets)
	}
	first := tickets[0]
	if first.Parent == nil || *first.Parent != "KAN-10" {
		t.Fatalf("parent %v", first.Parent)
	}
	if !same([]string{"KAN-5"}, first.Blockers) {
		t.Fatalf("blockers %v", first.Blockers)
	}
	if first.URL != "https://acme.atlassian.net/browse/KAN-23" {
		t.Fatalf("url %q", first.URL)
	}
	found := slices.ContainsFunc(shellStandIn.calls, func(c []string) bool {
		return len(c) >= 4 && c[0] == "jira" && c[1] == "workitem" && c[2] == "view" && c[3] == "KAN-23"
	})
	if !found {
		t.Fatal("the ticket was not read on its own")
	}
}

// answerShell answers each acli command by its first three arguments with the stdout
// given for it; any other command fails.
type answerShell map[string]string

func (a answerShell) Run(_ context.Context, _ string, arguments []string, _ string, _ *string) shell.Result {
	prefix := arguments
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	if out, ok := a[strings.Join(prefix, " ")]; ok {
		return shell.Result{Status: 0, Stdout: out}
	}
	return shell.Result{Status: 1}
}

// TestAcliAnswersMustBeJSONObjectsOrArrays: the decoder allows no top-level fragments,
// allows no top-level fragments, so a scalar or text that is not JSON is the parse
// error, and an object or array of the wrong shape is the Failure the caller names.
func TestAcliAnswersMustBeJSONObjectsOrArrays(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const parseError = "The given data was not valid JSON."
	for _, text := range []string{"5", "null", `"x"`, "not json"} {
		_, err := NewAcliJira(answerShell{"jira workitem search": text}).Search(ctx, "x")
		if err == nil || err.Error() != parseError {
			t.Fatalf("search %q: %v", text, err)
		}
		_, err = NewAcliJira(answerShell{"jira workitem view": text}).Ticket(ctx, "KAN-1")
		if err == nil || err.Error() != parseError {
			t.Fatalf("ticket %q: %v", text, err)
		}
		_, err = NewAcliJira(answerShell{"jira workitem view": text}).TicketState(ctx, "KAN-1")
		if err == nil || err.Error() != parseError {
			t.Fatalf("ticket state %q: %v", text, err)
		}
	}
	_, err := NewAcliJira(answerShell{"jira workitem search": "{}"}).Search(ctx, "x")
	if err == nil || err.Error() != "Could not read acli's answer" {
		t.Fatalf("search of an object: %v", err)
	}
	_, err = NewAcliJira(answerShell{"jira workitem view": "[]"}).Ticket(ctx, "KAN-1")
	if err == nil || err.Error() != "Could not read KAN-1 from acli" {
		t.Fatalf("ticket of an array: %v", err)
	}
	_, err = NewAcliJira(answerShell{"jira workitem view": "[]"}).TicketState(ctx, "KAN-1")
	if err == nil || err.Error() != "Could not read KAN-1 from acli" {
		t.Fatalf("ticket state of an array: %v", err)
	}
}

// TestAnAcliChildListThatIsNotAListHasNoChildren: the tickets under a ticket are
// empty unless acli's search answers with a list of objects; that answer must still
// be JSON.
func TestAnAcliChildListThatIsNotAListHasNoChildren(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	view := `{"key":"KAN-1","fields":{"status":{"name":"To Do","statusCategory":{"key":"new"}}}}`
	for _, list := range []string{`{"key":"KAN-2"}`, `[1,2]`} {
		state, err := NewAcliJira(answerShell{"jira workitem view": view, "jira workitem search": list}).TicketState(ctx, "KAN-1")
		if err != nil {
			t.Fatalf("children %q: %v", list, err)
		}
		if state.Children == nil || len(state.Children) != 0 {
			t.Fatalf("children %q: %+v", list, state.Children)
		}
	}
	if _, err := NewAcliJira(answerShell{"jira workitem view": view, "jira workitem search": "5"}).TicketState(ctx, "KAN-1"); err == nil {
		t.Fatal("a scalar child list was accepted")
	}
}

func TestLiveJiraReadsTicketsThroughAcli(t *testing.T) {
	t.Parallel()
	jql := os.Getenv("BELTLINE_LIVE_JQL")
	if jql == "" {
		t.Skip("set BELTLINE_LIVE_JQL")
	}
	ctx := context.Background()
	jira := NewAcliJira(nil)
	tickets, err := jira.Search(ctx, jql)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) == 0 {
		return
	}
	first := tickets[0]
	if len(first.URL) < len("/browse/")+len(first.Key) || first.URL[len(first.URL)-len(first.Key)-len("/browse/"):] != "/browse/"+first.Key {
		t.Fatalf("url %q", first.URL)
	}
	read, err := jira.Ticket(ctx, first.Key)
	if err != nil || read.Title != first.Title {
		t.Fatalf("ticket %+v: %v", read, err)
	}
}
