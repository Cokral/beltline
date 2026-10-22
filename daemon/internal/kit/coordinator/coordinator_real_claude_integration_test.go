//go:build integration

// (BELTLINE_REAL_CLAUDE=1, model BELTLINE_REAL_CLAUDE_MODEL):
// one issue triaged by the signed-in Claude CLI, with the fake GitHub and a tiny local repository.

package coordinator

import (
	"os"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestRealClaudeTriagesAnIssueAndReportsAnOutcome triages one issue with the Claude CLI and checks its outcome.
func TestRealClaudeTriagesAnIssueAndReportsAnOutcome(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CLAUDE") != "1" {
		t.Skip("set BELTLINE_REAL_CLAUDE=1")
	}
	t.Parallel()
	backend := core.BackendClaude
	c, paths, run, attempt := realOneIssueTriage(t, &backend, os.Getenv("BELTLINE_REAL_CLAUDE_MODEL"))
	realLogTriage(t, "Claude", c, run, attempt)
	realExpectTriaged(t, paths, run, attempt)
}
