// The
// tmux runner over a shell that reports sessions as they are launched, and the checkout the attempt
// continues in. The case itself is registered in servertest_contract_runs.go.
package servertest

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// scenarioContinueSession continues the held run's answerable attempt on a tmux runner whose shell
// reports no sessions until one is launched. It posts twice before the third, reused, post, which is
// the reply (run-continue-session.json).
func scenarioContinueSession(t testing.TB) (*Fixture, Reply) {
	g := newContinueFixture(t)
	run := findRun(t, g.Coordinator, "held with an answerable session", func(run core.Run) bool {
		return run.State == core.RunStateHeld && run.Answerable() != nil
	})
	attempt := *run.Answerable()
	makeContinueCheckouts(t, g, run, attempt)
	body := mustJSON(t, api.RunAttemptRequest{Attempt: attempt.ID})
	path := casePath("runContinueSession", run.ID, "")
	if first := postRunCase(g, path, body); first.Status != http.StatusOK {
		t.Fatalf("first continue: HTTP %d %s", first.Status, first.Text())
	}
	if second := postRunCase(g, path, body); second.Status != http.StatusOK && second.Status != http.StatusUnprocessableEntity {
		t.Fatalf("second continue: HTTP %d", second.Status)
	}
	return g, postRunCase(g, path, body)
}

// newContinueFixture is the contract fixture whose runner is a tmux runner over continueShell.
func newContinueFixture(t testing.TB) *Fixture {
	t.Helper()
	return Start(t, Options{Runner: runner.NewTmuxRunner(&continueShell{})})
}

// makeContinueCheckouts creates the checkout the attempt continues in: the directory the session
// spec names for the attempt (`paths.checkout(run, Sessions.checkout(run, stage), leg:)`).
func makeContinueCheckouts(t testing.TB, g *Fixture, run core.Run, attempt core.Attempt) {
	t.Helper()
	spec := g.Coordinator.Spec(run, attempt, "")
	if spec == nil {
		t.Fatal("the attempt has no session spec")
	}
	if err := os.MkdirAll(spec.Directory, 0o755); err != nil {
		t.Fatalf("checkout: %v", err)
	}
}

// continueShell is a shell that answers tmux without a server: a session exists once it has been
// created, and every command is recorded.
type continueShell struct {
	launched []string
	sessions map[string]bool
}

// Run answers the tmux calls Continue makes: has-session reports whether the named session was created,
// new-session records one.
func (s *continueShell) Run(_ context.Context, executable string, args []string, _ string, _ *string) shell.Result {
	if s.sessions == nil {
		s.sessions = map[string]bool{}
	}
	if executable != "tmux" {
		return shell.Result{Status: 0}
	}
	subcommand, target := tmuxSubcommand(args)
	switch subcommand {
	case "has-session":
		if s.sessions[strings.TrimPrefix(target, "=")] {
			return shell.Result{Status: 0}
		}
		return shell.Result{Status: 1}
	case "new-session":
		s.sessions[target] = true
		s.launched = append(s.launched, target)
	}
	return shell.Result{Status: 0}
}

// tmuxSubcommand is the tmux subcommand in args (after any -L socket) and the value named after -s
// (new-session) or -t (has-session) in it, if any.
func tmuxSubcommand(args []string) (string, string) {
	for i, arg := range args {
		if arg != "has-session" && arg != "new-session" {
			continue
		}
		for j := i + 1; j < len(args)-1; j++ {
			if (arg == "new-session" && args[j] == "-s") || (arg == "has-session" && args[j] == "-t") {
				return arg, args[j+1]
			}
		}
		return arg, ""
	}
	return "", ""
}
