// Tests that the acli commands run under their fixed limit, and that a lookup which
// hits the limit is neither parsed nor cached.

package jira

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// limitOf is the limit a call's deadline was set to, to the minute.
func limitOf(c shell.Call) time.Duration {
	return time.Until(c.Deadline).Round(time.Minute)
}

func TestAcliCommandsRunUnderTheCommandLimit(t *testing.T) {
	t.Parallel()
	rec := &shell.Recorder{Reply: func(c shell.Call) shell.Result {
		if c.Args[0] == "jira" && c.Args[1] == "auth" {
			return shell.Result{Stdout: "Site: acme.atlassian.net\n"}
		}
		return shell.Result{Stdout: "[]"}
	}}
	j := NewAcliJira(rec)
	ctx := context.Background()
	if _, err := j.Search(ctx, "project = KAN"); err != nil {
		t.Fatal(err)
	}
	if name := j.siteName(ctx); name != "acme.atlassian.net" {
		t.Fatalf("site %q", name)
	}
	calls := rec.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want the search and the auth status", len(calls))
	}
	for _, call := range calls {
		if call.Deadline.IsZero() {
			t.Fatalf("%s has no deadline", call.Line())
		}
		if got := limitOf(call); got != shell.CommandLimit {
			t.Errorf("%s: limit %s, want %s", call.Line(), got, shell.CommandLimit)
		}
	}
}

func TestAJiraSearchThatHitsItsLimitFailsRatherThanParsingItsOutput(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// acli printed a partial answer before it stalled; the search must not read it.
	stalled := shell.Stalled{Reply: shell.Result{Stdout: `[{"key":"KAN-1"`}}
	tickets, err := NewAcliJira(stalled).Search(ctx, "project = KAN")
	if err == nil || tickets != nil {
		t.Fatalf("tickets %v, err %v; want the timeout as the error", tickets, err)
	}
	if got := err.Error(); got != "acli jira workitem search: "+shell.TimedOutReason {
		t.Fatalf("err = %q, want the command named as timed out", got)
	}
}

// authShell stalls its first call, the site lookup that times out, and answers later
// ones with a site.
type authShell struct {
	mu    sync.Mutex
	calls int
}

func (a *authShell) Run(ctx context.Context, _ string, _ []string, _ string, _ *string) shell.Result {
	a.mu.Lock()
	a.calls++
	first := a.calls == 1
	a.mu.Unlock()
	if first {
		<-ctx.Done()
		return shell.Result{Stdout: "Site: partial.example\n"}
	}
	return shell.Result{Stdout: "Site: acme.atlassian.net\n"}
}

// TestATimedOutSiteLookupIsNeitherParsedNorCached: the lookup that hit its limit gives
// no site, and the next lookup asks acli again rather than reusing the failure.
func TestATimedOutSiteLookupIsNeitherParsedNorCached(t *testing.T) {
	t.Parallel()
	sh := &authShell{}
	j := NewAcliJira(sh)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if name := j.siteName(ctx); name != "" {
		t.Fatalf("a timed-out lookup gave the site %q", name)
	}
	if name := j.siteName(context.Background()); name != "acme.atlassian.net" {
		t.Fatalf("the next lookup gave %q; a timed-out one must not be cached", name)
	}
	if sh.calls != 2 {
		t.Fatalf("auth status ran %d times, want 2", sh.calls)
	}
}
