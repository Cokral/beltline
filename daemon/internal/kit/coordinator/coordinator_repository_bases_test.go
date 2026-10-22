package coordinator

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
)

// repositoryBasesTestRun is basesTestRun with the belt's shared base, the
// per-repository overrides and the subject's labels set on the run as admitted.
func repositoryBasesTestRun(base string, overrides map[string]string, subject core.Subject) core.Run {
	run := basesTestRun(base, subject)
	run.RepositoryBases = overrides
	return run
}

// TestEachRepositoryUsesItsOwnOverride: two overrides each apply in their own
// repository, and an unset one uses the shared base where that branch exists.
func TestEachRepositoryUsesItsOwnOverride(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	h.github.SetBranch("acme/billing", "release/billing", true)
	h.github.SetBranch("acme/web", "release/web", true)
	bases := basesTestBases(h)
	both := repositoryBasesTestRun("develop", map[string]string{"acme/billing": "release/billing", "acme/web": "release/web"}, basesTestTicket)
	onlyBilling := repositoryBasesTestRun("develop", map[string]string{"acme/billing": "release/billing"}, basesTestTicket)
	h.Turn(func() {
		ctx := context.Background()
		assertBase(t, bases, ctx, both, "acme/billing", "release/billing")
		assertBase(t, bases, ctx, both, "acme/web", "release/web")
		h.github.SetBranch("acme/web", "develop", true)
		assertBase(t, bases, ctx, onlyBilling, "acme/billing", "release/billing")
		assertBase(t, bases, ctx, onlyBilling, "acme/web", "develop")
	})
}

// TestALabelWinsOverTheOverrides: a base: label is requested in every repository,
// and a repository missing it uses its default, not its override.
func TestALabelWinsOverTheOverrides(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	h.github.SetBranch("acme/billing", "feature/epic", true)
	h.github.SetBranch("acme/web", "feature/epic", false)
	h.github.SetBranch("acme/web", "release/web", true)
	bases := basesTestBases(h)
	subject := basesTestEpicTicket
	subject.Labels = []string{"agent", "base:feature/epic"}
	run := repositoryBasesTestRun("develop", map[string]string{"acme/billing": "release/billing", "acme/web": "release/web"}, subject)
	h.Turn(func() {
		ctx := context.Background()
		assertBase(t, bases, ctx, run, "acme/billing", "feature/epic")
		assertBase(t, bases, ctx, run, "acme/web", "main")
	})
}

// basesLookupDouble is the fake GitHub with branch lookups that fail or answer unknown.
type basesLookupDouble struct {
	*github.FakeGitHub
	exists   *bool
	err      error
	defaultE error
}

func (d basesLookupDouble) BranchExists(context.Context, string, string) (*bool, error) {
	return d.exists, d.err
}

func (d basesLookupDouble) DefaultBranch(context.Context, string) (string, error) {
	if d.defaultE != nil {
		return "", d.defaultE
	}
	return "main", nil
}

// TestALookupThatFailsOrIsUnknownKeepsTheRequestedBranch: as before, a branch
// lookup that cannot answer keeps the requested branch, while a failing default
// lookup is an error.
func TestALookupThatFailsOrIsUnknownKeepsTheRequestedBranch(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	run := repositoryBasesTestRun("develop", map[string]string{"acme/web": "release/web"}, basesTestTicket)
	unknown := &Bases{c: h.Coord(), github: basesLookupDouble{FakeGitHub: h.github}, defaults: map[string]string{}, found: map[string]struct{}{}}
	failing := &Bases{c: h.Coord(), github: basesLookupDouble{FakeGitHub: h.github, err: errors.New("gh: unavailable")}, defaults: map[string]string{}, found: map[string]struct{}{}}
	noDefault := &Bases{c: h.Coord(), github: basesLookupDouble{FakeGitHub: h.github, exists: new(false), defaultE: errors.New("no default")}, defaults: map[string]string{}, found: map[string]struct{}{}}
	h.Turn(func() {
		ctx := context.Background()
		assertBase(t, unknown, ctx, run, "acme/web", "release/web")
		assertBase(t, failing, ctx, run, "acme/web", "release/web")
		if _, err := noDefault.base(ctx, run, "acme/web"); err == nil {
			t.Fatal("a failed default-branch lookup returned no error")
		}
	})
}

// basesRecordingDouble is the fake GitHub that records the base each commit
// count is asked against: the fake ignores that argument.
type basesRecordingDouble struct {
	*github.FakeGitHub
	asked  map[string]string
	ahead  map[string]int
	behind string
}

func (d *basesRecordingDouble) CommitsAhead(_ context.Context, repo, base, head string) (*int, error) {
	d.asked[repo] = base
	count := d.ahead[repo]
	return &count, nil
}

func (d *basesRecordingDouble) CommitsBehind(_ context.Context, repo, base, head string) (int, error) {
	d.behind = base
	return 0, nil
}

// TestMergeParticipationAndBehindFollowEachRepositoryBase: a repository merges
// its run branch when that branch has commits its own base lacks, measured
// against the base requested there; a behind check compares against the pull
// request's own base even when an override differs.
func TestMergeParticipationAndBehindFollowEachRepositoryBase(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	double := &basesRecordingDouble{FakeGitHub: h.github, asked: map[string]string{}, ahead: map[string]int{"acme/billing": 2, "acme/web": 0}}
	bases := &Bases{c: h.Coord(), github: double, defaults: map[string]string{}, found: map[string]struct{}{}}
	watches := &Watches{c: h.Coord(), github: double, bases: bases, observations: map[string]WatchesObservation{}, lastCheck: map[string]time.Time{}}
	run := repositoryBasesTestRun("develop", map[string]string{"acme/billing": "release/billing", "acme/web": "release/web"}, basesTestTicket)
	h.Turn(func() {
		ctx := context.Background()
		merging := watches.toMerge(ctx, []string{"acme/billing", "acme/web"}, run, "beltline/PAY-31")
		if !reflect.DeepEqual(merging, []string{"acme/billing"}) {
			t.Fatalf("repositories to merge = %v, want only acme/billing", merging)
		}
		if double.asked["acme/billing"] != "release/billing" || double.asked["acme/web"] != "release/web" {
			t.Fatalf("commit counts asked against %v, want each repository's own base", double.asked)
		}
		pull := core.PullRequest{Repo: "acme/billing", Number: 7, BaseRef: new("main"), HeadSha: new("abc")}
		watches.behind(ctx, pull, run)
		if double.behind != "main" {
			t.Fatalf("behind compared against %q, want the pull request's base main", double.behind)
		}
	})
}
