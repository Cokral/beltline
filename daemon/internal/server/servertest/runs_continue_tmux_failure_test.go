// Contract test of a continue-session launch that fails in tmux: a 422 with the tmux message and no
// problems.

package servertest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// refusingNewSessionShell is the continue shell, except that tmux refuses to create a session.
type refusingNewSessionShell struct {
	continueShell
}

// Run refuses new-session with tmux's stderr and answers the other calls as continueShell does.
func (s *refusingNewSessionShell) Run(ctx context.Context, executable string, args []string, dir string, env *string) shell.Result {
	if executable == "tmux" {
		if subcommand, _ := tmuxSubcommand(args); subcommand == "new-session" {
			return shell.Result{Status: 1, Stderr: "duplicate session: bl-continue-1\n"}
		}
	}
	return s.continueShell.Run(ctx, executable, args, dir, env)
}

// TestContinueSessionTmuxFailureIs422 checks that a continue refused by tmux answers
// 422 with "tmux: <message>" and no problems, as the Failure does.
func TestContinueSessionTmuxFailureIs422(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{Runner: runner.NewTmuxRunner(&refusingNewSessionShell{})})
	run := findRun(t, f.Coordinator, "held with an answerable session", func(run core.Run) bool {
		return run.State == core.RunStateHeld && run.Answerable() != nil
	})
	attempt := *run.Answerable()
	makeContinueCheckouts(t, f, run, attempt)
	body := mustJSON(t, api.RunAttemptRequest{Attempt: attempt.ID})
	reply := postRunCase(f, casePath("runContinueSession", run.ID, ""), body)
	if reply.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", reply.Status, reply.Text())
	}
	if !strings.Contains(reply.Text(), `"message":"tmux: duplicate session: bl-continue-1"`) {
		t.Fatalf("message missing from %s", reply.Text())
	}
	if !strings.Contains(reply.Text(), `"problems":[]`) {
		t.Fatalf("problems should be empty: %s", reply.Text())
	}
}
