package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexTrustsTheCheckoutForTheLaunchBeforeThePrompt(t *testing.T) {
	t.Parallel()
	checkout, _ := realPath(t.TempDir())
	adapter := NewCodexAdapter(&checkout)
	const bypass = "--dangerously-bypass-approvals-and-sandbox"
	override := "projects={\"" + checkout + "\"={trust_level=\"trusted\"}}"
	mustEqual(t, []string{bypass, "-c", override, "-m", "gpt-test", "$PROMPT"},
		CodexTrusting(adapter.InteractiveArguments("id", "name", "gpt-test", nil), checkout))
	token := "rollout-id"
	mustEqual(t, []string{"resume", "rollout-id", bypass, "-c", override, "$PROMPT"},
		CodexTrusting(adapter.InteractiveResumeArguments("id", "name", "", nil, &token), checkout))
	if CodexTrusting(adapter.InteractiveResumeArguments("id", "name", "", nil, nil), checkout) != nil {
		t.Error("a refused resume gained arguments")
	}
	mustEqual(t, []string{"--other"}, CodexTrusting([]string{"--other"}, checkout))
	mustEqual(t, []string{bypass}, CodexTrusting([]string{bypass}, "relative"))
}

func TestCodexTrustsAWorktreesRepositoryToo(t *testing.T) {
	t.Parallel()
	root, _ := realPath(t.TempDir())
	repository := filepath.Join(root, "repos", "acme.co", "app")
	gitdir := filepath.Join(repository, ".git", "worktrees", "worktree1")
	worktree := filepath.Join(root, "work", "1", "worktree")
	for _, folder := range []string{gitdir, worktree} {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, []string{worktree, repository}, codexTrustedProjects(worktree))
	mustEqual(t, []string{repository}, codexTrustedProjects(repository))
}

func TestTOMLBasicStringsEscapeQuotesBackslashesAndControls(t *testing.T) {
	t.Parallel()
	mustEqual(t, `"/a \"b\"\\c\u0009"`, tomlBasicString("/a \"b\"\\c\t"))
}
