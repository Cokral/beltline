package library

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// ghStub answers gh calls with a fixed URL and records them; git runs for real.
type ghStub struct {
	url   string
	calls [][]string
}

func (g *ghStub) Run(ctx context.Context, executable string, args []string, dir string, input *string) shell.Result {
	if executable == "gh" {
		g.calls = append(g.calls, args)
		return shell.Result{Stdout: g.url + "\n"}
	}
	return shell.ProcessRunner{}.Run(ctx, executable, args, dir, input)
}

// shellIn runs a command in dir for test setup and fails the test when it fails.
func shellIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRemote makes a bare repository whose main branch holds files, or none when files is nil.
func newRemote(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	shellIn(t, root, "init", "-q", "--bare", "-b", "main", remote)
	if files == nil {
		return remote
	}
	work := filepath.Join(root, "seed")
	shellIn(t, root, "clone", "-q", remote, work)
	writeAll(t, work, files)
	shellIn(t, work, "add", "-A")
	shellIn(t, work, "commit", "-q", "-m", "seed")
	shellIn(t, work, "push", "-q", "origin", "HEAD:main")
	return remote
}

// writeAll writes files under dir, creating their folders.
func writeAll(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o666); err != nil {
			t.Fatal(err)
		}
	}
}

// reposFor is a Repos whose clones live in a fresh folder and whose remote for
// repository is remote.
func reposFor(t *testing.T, repository, remote string, gh shell.Runner) Repos {
	return Repos{
		Root:   filepath.Join(t.TempDir(), "library-repos"),
		Runner: gh,
		URL: func(name string) string {
			if name != repository {
				t.Fatalf("unexpected repository %s", name)
			}
			return remote
		},
	}
}

func TestSyncClonesAndFollowsTheDefaultBranch(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	repos := reposFor(t, "owner/lib", remote, &ghStub{})
	clone, err := repos.Sync(context.Background(), "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	dir, commit := clone.Dir, clone.Commit
	if commit == "" || dir != repos.Dir("owner/lib") || clone.Branch != "main" {
		t.Fatalf("Sync gave %+v", clone)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "belts/a/v1.json")); string(data) != "one" {
		t.Fatalf("clone holds %q", data)
	}

	other := filepath.Join(t.TempDir(), "other")
	shellIn(t, filepath.Dir(other), "clone", "-q", remote, other)
	writeAll(t, other, map[string]string{"belts/a/v2.json": "two"})
	shellIn(t, other, "add", "-A")
	shellIn(t, other, "commit", "-q", "-m", "more")
	shellIn(t, other, "push", "-q", "origin", "HEAD:main")

	again, err := repos.Sync(context.Background(), "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	if next := again.Commit; next == commit {
		t.Fatalf("Sync did not follow the remote: still %s", next)
	}
	if _, err := os.Stat(filepath.Join(dir, "belts/a/v2.json")); err != nil {
		t.Fatalf("second Sync did not fetch the new file: %v", err)
	}
}

func TestCommitAndPushWritesTheDefaultBranch(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	repos := reposFor(t, "owner/lib", remote, &ghStub{})
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	dir := clone.Dir
	writeAll(t, dir, map[string]string{"belts/a/v2.json": "two"})
	commit, changed, err := repos.Commit(ctx, dir, "Back up Beltline library")
	if err != nil || !changed || commit == "" {
		t.Fatalf("Commit = %q, %v, %v", commit, changed, err)
	}
	if err := repos.Push(ctx, dir, clone.Branch); err != nil {
		t.Fatal(err)
	}
	if got := shellIn(t, remote, "show", "main:belts/a/v2.json"); got != "two" {
		t.Fatalf("remote holds %q", got)
	}

	if _, changed, err := repos.Commit(ctx, dir, "nothing new"); err != nil || changed {
		t.Fatalf("Commit with no change = changed %v, %v", changed, err)
	}
}

func TestShareBranchOpensAPullRequest(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	gh := &ghStub{url: "https://github.com/owner/lib/pull/7"}
	repos := reposFor(t, "owner/lib", remote, gh)
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	dir := clone.Dir
	branch := "beltline/share-b-v2"
	if err := repos.Branch(ctx, dir, branch); err != nil {
		t.Fatal(err)
	}
	writeAll(t, dir, map[string]string{"belts/b/v2.json": "shared"})
	if _, changed, err := repos.Commit(ctx, dir, "Share b v2"); err != nil || !changed {
		t.Fatalf("Commit = changed %v, %v", changed, err)
	}
	if err := repos.PushBranch(ctx, dir, branch); err != nil {
		t.Fatal(err)
	}
	if got := shellIn(t, remote, "show", branch+":belts/b/v2.json"); got != "shared" {
		t.Fatalf("branch holds %q", got)
	}
	url, err := repos.OpenPullRequest(ctx, "owner/lib", branch, clone.Branch, "Share b v2", "body")
	if err != nil {
		t.Fatal(err)
	}
	if url != gh.url {
		t.Fatalf("OpenPullRequest = %q", url)
	}
	if len(gh.calls) != 1 || !strings.Contains(strings.Join(gh.calls[0], " "), "--head "+branch) {
		t.Fatalf("gh calls = %v", gh.calls)
	}
}

func TestSyncOfAnEmptyRepositoryPushesItsFirstCommit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, nil)
	repos := reposFor(t, "owner/lib", remote, &ghStub{})
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	dir := clone.Dir
	if clone.Commit != "" {
		t.Fatalf("an empty repository has commit %q", clone.Commit)
	}
	writeAll(t, dir, map[string]string{"agents/x/v1.md": "---\n"})
	if _, changed, err := repos.Commit(ctx, dir, "Back up Beltline library"); err != nil || !changed {
		t.Fatalf("Commit = changed %v, %v", changed, err)
	}
	if err := repos.Push(ctx, dir, clone.Branch); err != nil {
		t.Fatal(err)
	}
	if got := shellIn(t, remote, "show", "main:agents/x/v1.md"); got != "---" {
		t.Fatalf("remote holds %q", got)
	}
}

// ghPulls answers gh's pull request calls: create fails as gh does when a pull request from the
// branch is already open, and view answers that pull request's URL.
type ghPulls struct {
	url    string
	exists bool
	calls  [][]string
}

func (g *ghPulls) Run(ctx context.Context, executable string, args []string, dir string, input *string) shell.Result {
	if executable != "gh" {
		return shell.ProcessRunner{}.Run(ctx, executable, args, dir, input)
	}
	g.calls = append(g.calls, args)
	if args[0] == "pr" && args[1] == "view" {
		return shell.Result{Stdout: g.url + "\n"}
	}
	if g.exists {
		return shell.Result{Status: 1, Stderr: `a pull request for branch "x" into branch "main" already exists:` + "\n" + g.url + "\n"}
	}
	return shell.Result{Stdout: g.url + "\n"}
}

func TestResharingForcePushesOverTheOpenShareBranch(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	repos := reposFor(t, "owner/lib", remote, &ghStub{})
	ctx := context.Background()
	branch := "beltline/share-b-v2"
	for _, text := range []string{"first share", "second share"} {
		clone, err := repos.Sync(ctx, "owner/lib")
		if err != nil {
			t.Fatal(err)
		}
		if err := repos.Branch(ctx, clone.Dir, branch); err != nil {
			t.Fatal(err)
		}
		writeAll(t, clone.Dir, map[string]string{"belts/b/v2.json": text})
		if _, changed, err := repos.Commit(ctx, clone.Dir, "Share b v2"); err != nil || !changed {
			t.Fatalf("%s: Commit = changed %v, %v", text, changed, err)
		}
		if err := repos.PushBranch(ctx, clone.Dir, branch); err != nil {
			t.Fatalf("%s: push over the open branch: %v", text, err)
		}
	}
	if got := shellIn(t, remote, "show", branch+":belts/b/v2.json"); got != "second share" {
		t.Fatalf("branch holds %q after the second share", got)
	}
}

func TestOpenPullRequestReturnsTheOpenOneWhenThereIs(t *testing.T) {
	t.Parallel()
	gh := &ghPulls{url: "https://github.com/owner/lib/pull/9", exists: true}
	repos := Repos{Root: t.TempDir(), Runner: gh}
	url, err := repos.OpenPullRequest(context.Background(), "owner/lib", "beltline/share-b-v2", "main", "Share b v2", "body")
	if err != nil {
		t.Fatal(err)
	}
	if url != gh.url {
		t.Fatalf("OpenPullRequest = %q, want the open pull request %q", url, gh.url)
	}
	last := gh.calls[len(gh.calls)-1]
	if strings.Join(last, " ") != "pr view beltline/share-b-v2 --repo owner/lib --json url -q .url" {
		t.Fatalf("last gh call = %v", last)
	}
}

func TestRefreshAfterAPullRequestShareListsTheDefaultBranch(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	gh := &ghStub{url: "https://github.com/owner/lib/pull/7"}
	repos := reposFor(t, "owner/lib", remote, gh)
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Branch(ctx, clone.Dir, "beltline/share-b-v2"); err != nil {
		t.Fatal(err)
	}
	writeAll(t, clone.Dir, map[string]string{"belts/b/v2.json": "shared"})
	if _, _, err := repos.Commit(ctx, clone.Dir, "Share b v2"); err != nil {
		t.Fatal(err)
	}
	if err := repos.PushBranch(ctx, clone.Dir, "beltline/share-b-v2"); err != nil {
		t.Fatal(err)
	}
	// The clone sits on the share branch now; a refresh goes back to the default branch's content.
	again, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	if again.Branch != "main" {
		t.Fatalf("refresh left the clone on %q", again.Branch)
	}
	if _, err := os.Stat(filepath.Join(again.Dir, "belts/b/v2.json")); err == nil {
		t.Fatal("refresh lists the share branch's file, not the default branch's")
	}
	if _, err := os.Stat(filepath.Join(again.Dir, "belts/a/v1.json")); err != nil {
		t.Fatalf("refresh lost the default branch's file: %v", err)
	}
}

// statusOf is the clone's branch and whether its folder is clean.
func statusOf(t *testing.T, dir string) (string, string) {
	t.Helper()
	return shellIn(t, dir, "rev-parse", "--abbrev-ref", "HEAD"), shellIn(t, dir, "status", "--porcelain")
}

func TestResetAfterAPullRequestShareLeavesTheDefaultBranch(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	gh := &ghStub{url: "https://github.com/owner/lib/pull/7"}
	repos := reposFor(t, "owner/lib", remote, gh)
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	branch := "beltline/share-belt-b-v2"
	if err := repos.Branch(ctx, clone.Dir, branch); err != nil {
		t.Fatal(err)
	}
	writeAll(t, clone.Dir, map[string]string{"belts/b/v2.json": "shared"})
	if _, _, err := repos.Commit(ctx, clone.Dir, "Share b v2"); err != nil {
		t.Fatal(err)
	}
	if err := repos.PushBranch(ctx, clone.Dir, branch); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.OpenPullRequest(ctx, "owner/lib", branch, clone.Branch, "Share b v2", "body"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Reset(ctx, clone.Dir, clone.Branch); err != nil {
		t.Fatal(err)
	}
	if head, status := statusOf(t, clone.Dir); head != "main" || status != "" {
		t.Fatalf("after the share the clone is on %q with %q", head, status)
	}
	if _, err := os.Stat(filepath.Join(clone.Dir, "belts/b/v2.json")); err == nil {
		t.Fatal("the clone still holds the share, so the Library page would list it as in the repository")
	}
	if _, err := os.Stat(filepath.Join(clone.Dir, "belts/a/v1.json")); err != nil {
		t.Fatalf("the default branch's file is gone: %v", err)
	}
}

func TestResetTakesBackACommitThatDidNotPush(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, map[string]string{"belts/a/v1.json": "one"})
	repos := reposFor(t, "owner/lib", remote, &ghStub{})
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, clone.Dir, map[string]string{"belts/c/v1.json": "unpushed"})
	if _, changed, err := repos.Commit(ctx, clone.Dir, "Back up Beltline library"); err != nil || !changed {
		t.Fatalf("Commit = changed %v, %v", changed, err)
	}
	// The remote goes away, so the push fails and the commit stays only in the clone.
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	if err := repos.Push(ctx, clone.Dir, clone.Branch); err == nil {
		t.Fatal("push to a missing remote succeeded")
	}
	if err := repos.Reset(ctx, clone.Dir, clone.Branch); err != nil {
		t.Fatal(err)
	}
	if head, status := statusOf(t, clone.Dir); head != "main" || status != "" {
		t.Fatalf("after the failed push the clone is on %q with %q", head, status)
	}
	if got := shellIn(t, clone.Dir, "rev-parse", "HEAD"); got != clone.Commit {
		t.Fatalf("HEAD is %s, want the remote's commit %s", got, clone.Commit)
	}
	if _, err := os.Stat(filepath.Join(clone.Dir, "belts/c/v1.json")); err == nil {
		t.Fatal("the unpushed file is still in the clone")
	}
}

func TestResetOfAnEmptyRepositoryTakesBackItsUnpushedCommit(t *testing.T) {
	t.Parallel()
	remote := newRemote(t, nil)
	repos := reposFor(t, "owner/lib", remote, &ghStub{})
	ctx := context.Background()
	clone, err := repos.Sync(ctx, "owner/lib")
	if err != nil {
		t.Fatal(err)
	}
	writeAll(t, clone.Dir, map[string]string{"agents/x/v1.md": "---\n"})
	if _, changed, err := repos.Commit(ctx, clone.Dir, "Share agent x v1"); err != nil || !changed {
		t.Fatalf("Commit = changed %v, %v", changed, err)
	}
	if err := repos.Reset(ctx, clone.Dir, clone.Branch); err != nil {
		t.Fatal(err)
	}
	if head := repos.run(ctx, clone.Dir, "rev-parse", "--verify", "-q", "HEAD"); head.OK() {
		t.Fatalf("the clone still has a commit: %s", head.Stdout)
	}
	if status := shellIn(t, clone.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("the clone is not clean: %q", status)
	}
	if _, err := os.Stat(filepath.Join(clone.Dir, "agents/x/v1.md")); err == nil {
		t.Fatal("the unpushed file is still in the clone")
	}
}

func TestGitFailureNamesTheSubcommandAfterItsOptions(t *testing.T) {
	t.Parallel()
	repos := Repos{Root: t.TempDir(), Runner: failingRunner{}}
	err := repos.git(context.Background(), t.TempDir(), "-c", "user.name=Beltline", "-c", "user.email=b@localhost", "commit", "-q", "-m", "x")
	if err == nil || !strings.HasPrefix(err.Error(), "git commit failed") {
		t.Fatalf("err = %v, want the commit named", err)
	}
}

// failingRunner fails every command with a message.
type failingRunner struct{}

func (failingRunner) Run(context.Context, string, []string, string, *string) shell.Result {
	return shell.Result{Status: 1, Stderr: "refused"}
}
