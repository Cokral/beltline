package runner

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

func TestCodexHeadlessArgumentsAndPrompts(t *testing.T) {
	t.Parallel()
	adapter := NewCodexAdapter(nil)
	const bypass = "--dangerously-bypass-approvals-and-sandbox"
	mustEqual(t, []string{"exec", bypass, "--skip-git-repo-check", "$PROMPT"}, adapter.Arguments("id", "name", "", nil))
	mustEqual(t, []string{"exec", bypass, "--skip-git-repo-check", "-m", "gpt-test", "-c", "model_reasoning_effort=high", "$PROMPT"},
		adapter.Arguments("id", "name", " gpt-test ", new(core.ThinkingHigh)))
	token := "rollout-id"
	mustEqual(t, []string{"exec", "resume", bypass, "--skip-git-repo-check", "rollout-id", "$PROMPT"},
		adapter.ResumeArguments("id", "name", "", nil, &token))
	mustEqual(t, adapter.Arguments("id", "name", "", nil), adapter.ResumeArguments("id", "name", "", nil, nil))
	mustEqual(t, []string{"resume", "rollout-id", bypass}, adapter.ContinueArguments("id", "name", &token))
	mustEqual(t, []string{bypass}, adapter.ContinueArguments("id", "name", nil))
	mustEqual(t, []string{bypass, "-m", "gpt-test", "-c", "model_reasoning_effort=low", "$PROMPT"},
		adapter.ConversationArguments("id", "name", "gpt-test", new(core.ThinkingLow)))
}

func TestEffortPassesThroughUnchanged(t *testing.T) {
	t.Parallel()
	adapter := NewCodexAdapter(nil)
	token := "rollout-id"
	for _, thinking := range core.AllThinking {
		thinking := thinking
		expected := "model_reasoning_effort=" + string(thinking)
		fresh := adapter.Arguments("id", "name", " \n", &thinking)
		resumed := adapter.ResumeArguments("id", "name", "gpt-test", &thinking, &token)
		resumedWithoutToken := adapter.ResumeArguments("id", "name", "", &thinking, nil)
		conversation := adapter.ConversationArguments("id", "name", "gpt-test", &thinking)
		if slices.Contains(fresh, "-m") {
			t.Errorf("%s: blank model passed -m: %v", thinking, fresh)
		}
		mustEqual(t, adapter.Arguments("id", "name", "", &thinking), resumedWithoutToken)
		for _, args := range [][]string{fresh, resumed, resumedWithoutToken, conversation} {
			if !slices.Contains(args, expected) {
				t.Errorf("%s: %s missing: %v", thinking, expected, args)
			}
			for _, argument := range args {
				if strings.HasPrefix(argument, "model_reasoning_effort=") && argument != expected {
					t.Errorf("%s: unexpected %s", thinking, argument)
				}
			}
			if args[len(args)-1] != "$PROMPT" {
				t.Errorf("%s: last argument is %q", thinking, args[len(args)-1])
			}
		}
	}
	for _, args := range [][]string{
		adapter.Arguments("id", "name", "gpt-test", nil),
		adapter.ResumeArguments("id", "name", "gpt-test", nil, &token),
		adapter.ResumeArguments("id", "name", "gpt-test", nil, nil),
		adapter.ConversationArguments("id", "name", "gpt-test", nil),
	} {
		for _, argument := range args {
			if strings.Contains(argument, "model_reasoning_effort") {
				t.Errorf("unset level passed an effort: %v", args)
			}
		}
	}
}

// writeRollout writes one rollout file for a checkout and sets its modification time.
func writeRollout(t *testing.T, home, folder, cwd, id string, modified int64, body string) {
	t.Helper()
	file := filepath.Join(home, ".codex", "sessions", folder, "rollout-"+id+".jsonl")
	testkit.WriteFile(t, file, body)
	when := time.Unix(modified, 0)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestRolloutsAreCheckoutScopedAndNewestWins(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	checkout := filepath.Join(home, "checkout")
	other := filepath.Join(home, "other")
	for _, dir := range []string{checkout, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	meta := func(id, cwd string) string {
		return `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `"}}`
	}
	adapter := NewCodexAdapter(&home)
	if got := adapter.SessionToken(checkout); got != nil {
		t.Fatalf("token before rollouts = %v", *got)
	}
	if got := adapter.SessionToken(filepath.Join(home, "missing")); got != nil {
		t.Fatalf("token for a missing checkout = %v", *got)
	}
	writeRollout(t, home, "2025/01/01", checkout, "old", 1, meta("old", checkout))
	writeRollout(t, home, "2025/01/02", checkout, "new", 2, meta("new", checkout))
	writeRollout(t, home, "2025/01/03", other, "wrong", 3, meta("wrong", other))
	if got := adapter.SessionToken(checkout); got == nil || *got != "new" {
		t.Fatalf("token = %v, want new", got)
	}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(checkout, alias); err != nil {
		t.Fatal(err)
	}
	if got := adapter.SessionToken(alias); got == nil || *got != "new" {
		t.Fatalf("token through an alias = %v, want new", got)
	}
	if got := adapter.Ending(checkout, "new", 0); got != nil {
		t.Fatalf("ending = %v", got)
	}
	if got := adapter.Mark(checkout, "new"); got != 0 {
		t.Fatalf("mark = %d", got)
	}
	if got := adapter.Reply(checkout, "new", 0); got != nil {
		t.Fatalf("reply = %v", *got)
	}
	if CodexRolloutID("invalid") != nil {
		t.Fatalf("rollout id of invalid text")
	}
	if CodexWorkingDirectory(`{"type":"message","payload":{"cwd":"/wrong"}}`) != nil {
		t.Fatalf("working directory of a non-session line")
	}
}

func TestUsageFollowsOneRolloutAndIgnoresTheRest(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	checkout := filepath.Join(home, "checkout")
	other := filepath.Join(home, "other")
	for _, dir := range []string{checkout, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := func(id, cwd string, tokens int) string {
		return `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","model":"gpt-5.3-codex"}}` + "\n" +
			`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":` + strconv.Itoa(tokens) +
			`,"cached_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"total_tokens":` + strconv.Itoa(tokens) + `}}}}`
	}
	writeRollout(t, home, "2026/10/01", checkout, "old", 1, body("old", checkout, 10))
	writeRollout(t, home, "2026/10/02", checkout, "new", 2, body("new", checkout, 40))
	writeRollout(t, home, "2026/10/03", other, "elsewhere", 3, body("elsewhere", other, 999))
	adapter := NewCodexAdapter(&home)
	fresh := &core.UsageCheckpoint{Preexisting: []string{"old"}}
	selected := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, fresh)
	if selected == nil || selected.TotalTokens == nil || *selected.TotalTokens != 40 {
		t.Fatalf("selected = %+v", selected)
	}
	if id := adapter.UsageIdentity(checkout, fresh); id == nil || *id != "new" {
		t.Fatalf("identity = %v", id)
	}
	zero := core.TokenTotalsZero
	if got := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, &core.UsageCheckpoint{SessionID: new("old"), Totals: &zero}); got == nil || *got.TotalTokens != 10 {
		t.Fatalf("old rollout = %+v", got)
	}
	if got := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, &core.UsageCheckpoint{SessionID: new("old")}); got != nil {
		t.Fatalf("old rollout without totals = %+v", got)
	}
	if got := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, &core.UsageCheckpoint{Preexisting: []string{"old", "new"}}); got != nil {
		t.Fatalf("every rollout preexisting = %+v", got)
	}
	if got := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, &core.UsageCheckpoint{SessionID: new("missing")}); got != nil {
		t.Fatalf("missing rollout = %+v", got)
	}
	if got := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, nil); got != nil {
		t.Fatalf("no checkpoint = %+v", got)
	}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(checkout, alias); err != nil {
		t.Fatal(err)
	}
	if got := adapter.UsageCheckpoint(alias, "beltline", "name", &core.UsageCheckpoint{SessionID: new("new")}); got == nil || got.Totals == nil || got.Totals.Total != 40 {
		t.Fatalf("checkpoint through alias = %+v", got)
	}
	if got := adapter.UsageWithCheckpoint(alias, "beltline", "name", 0, 0, &core.UsageCheckpoint{SessionID: new("new"), Totals: &zero}); got == nil || *got.TotalTokens != 40 {
		t.Fatalf("usage through alias = %+v", got)
	}
	if got := adapter.UsageWithCheckpoint(other, "beltline", "name", 0, 0, &core.UsageCheckpoint{SessionID: new("new"), Totals: &zero}); got != nil {
		t.Fatalf("usage from another checkout = %+v", got)
	}
	resumed := adapter.UsageCheckpoint(checkout, "beltline", "name", &core.UsageCheckpoint{SessionID: new("new")})
	if resumed == nil || resumed.Totals == nil || resumed.Totals.Total != 40 {
		t.Fatalf("resumed checkpoint = %+v", resumed)
	}
	if got := adapter.UsageWithCheckpoint(checkout, "beltline", "name", 0, 0, resumed); got == nil || got.CostNanos == nil || *got.CostNanos != 0 {
		t.Fatalf("resumed usage = %+v", got)
	}
}

// A rollout that is a symbolic link is not a rollout file, however new its target,
// as resourceValues(forKeys: [.isRegularFileKey]) reports it.
func TestASymlinkedRolloutIsNotARollout(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	checkout := filepath.Join(home, "checkout")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := func(id string) string {
		return `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + checkout + `"}}`
	}
	writeRollout(t, home, "2025/01/01", checkout, "real", 1, meta("real"))
	target := filepath.Join(home, "elsewhere", "rollout-target.jsonl")
	testkit.WriteFile(t, target, meta("linked"))
	when := time.Unix(9, 0)
	if err := os.Chtimes(target, when, when); err != nil {
		t.Fatal(err)
	}
	linkFolder := filepath.Join(home, ".codex", "sessions", "2025", "01", "02")
	if err := os.MkdirAll(linkFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(linkFolder, "rollout-linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexAdapter(&home)
	if got := adapter.SessionToken(checkout); got == nil || *got != "real" {
		t.Fatalf("token = %v, want real", got)
	}
}
