package runner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// WorkspaceCheckoutKind is which checkout a session works in.
type WorkspaceCheckoutKind string

const (
	// WorkspaceCheckoutKindBase is a read-only checkout of the base branch.
	WorkspaceCheckoutKindBase WorkspaceCheckoutKind = "base"
	// WorkspaceCheckoutKindWorktree is a worktree on the run's branch, made from the
	// base the first time; also read later, read-only, by the stages after the one
	// that wrote it.
	WorkspaceCheckoutKindWorktree WorkspaceCheckoutKind = "worktree"
	// WorkspaceCheckoutKindPullRequest is the head of a pull request, read-only.
	WorkspaceCheckoutKindPullRequest WorkspaceCheckoutKind = "pullRequest"
)

// WorkspaceCheckout is the enum WorkspaceSpec.Checkout. Number is the pull
// request number for WorkspaceCheckoutKindPullRequest, and zero otherwise.
type WorkspaceCheckout struct {
	Kind   WorkspaceCheckoutKind
	Number int
}

// WorkspaceSpec is the checkout one agent session works in, without the run it is for.
type WorkspaceSpec struct {
	Repo string
	// Repos are every repository of the run; a single repository is [Repo].
	Repos []string
	// Base is the branch the checkout starts from.
	Base string
	// Branch is the worktree's branch.
	Branch   string
	Checkout WorkspaceCheckout
	// Folder is the folder a run's checkouts live in: work/<run folder>.
	Folder string
	// From is the ref the worktree's branch is created from. Nil starts from origin/<Base>.
	From *string
}

// WorkspaceSpecArgs are the parameters of NewWorkspaceSpec. Repos empty means [Repo].
type WorkspaceSpecArgs struct {
	Repo     string
	Base     string
	Branch   string
	Checkout WorkspaceCheckout
	Folder   string
	Repos    []string
	From     *string
}

// NewWorkspaceSpec builds a WorkspaceSpec from its arguments.
func NewWorkspaceSpec(a WorkspaceSpecArgs) WorkspaceSpec {
	repos := append([]string(nil), a.Repos...)
	if len(repos) == 0 {
		repos = []string{a.Repo}
	}
	return WorkspaceSpec{
		Repo:     a.Repo,
		Repos:    repos,
		Base:     a.Base,
		Branch:   a.Branch,
		Checkout: a.Checkout,
		Folder:   a.Folder,
		From:     a.From,
	}
}

// Directory is where the session works: readonly or worktree inside Folder.
func (s WorkspaceSpec) Directory() string { return WorkspaceSpecDirectory(s.Checkout, s.Folder) }

// CheckoutDirectory is the repository's own checkout inside Directory. A single
// repository's checkout is Directory itself.
func (s WorkspaceSpec) CheckoutDirectory() string {
	return WorkspaceSpecCheckout(s.Repo, s.Repos, s.Directory())
}

// WorkspaceSpecCheckout is the checkout of repo inside directory: the directory
// itself for one repository, else a folder named for the repository (its owner
// and name when two share a name).
func WorkspaceSpecCheckout(repo string, repos []string, directory string) string {
	if len(repos) <= 1 {
		return directory
	}
	name, _ := workspaceLastComponent(repo)
	duplicates := 0
	for _, other := range repos {
		if last, ok := workspaceLastComponent(other); ok && last == name {
			duplicates++
		}
	}
	component := name
	if duplicates > 1 {
		component = strings.ReplaceAll(repo, "/", "--")
	}
	return filepath.Join(directory, component)
}

// workspaceLastComponent is the last non-empty part of a repository name.
// ok is false when there is none.
func workspaceLastComponent(repo string) (string, bool) {
	var last string
	ok := false
	for _, part := range strings.Split(repo, "/") {
		if part != "" {
			last = part
			ok = true
		}
	}
	if !ok {
		return repo, false
	}
	return last, true
}

// WorkspaceCheckoutPath is the folder a run's checkout is in: WorkspaceSpecDirectory
// inside the run's work folder for leg.
// declared here because store cannot import this package (runner imports store).
func WorkspaceCheckoutPath(paths store.Paths, run core.Run, checkout WorkspaceCheckout, leg *string) string {
	return WorkspaceSpecDirectory(checkout, paths.WorkFolderLeg(run, leg))
}

// WorkspaceSpecDirectory is readonly or worktree inside folder, by checkout kind.
func WorkspaceSpecDirectory(checkout WorkspaceCheckout, folder string) string {
	if checkout.Kind == WorkspaceCheckoutKindWorktree {
		return filepath.Join(folder, "worktree")
	}
	return filepath.Join(folder, "readonly")
}

// MergeResultKind says what merging a run's leg branches came to.
type MergeResultKind string

const (
	// MergeResultNothing means at most one branch had commits the base did not have.
	MergeResultNothing MergeResultKind = "nothing"
	// MergeResultMerged means the branches named in Names went into the run's branch.
	MergeResultMerged MergeResultKind = "merged"
	// MergeResultConflict means the files named in Names could not be merged.
	MergeResultConflict MergeResultKind = "conflict"
)

// MergeResult is what merging a run's leg branches into its own branch came to.
// Names are the merged branches or the conflicted files, by Kind.
type MergeResult struct {
	Kind  MergeResultKind
	Names []string
}

// Workspaces provides where each stage's agent works.
type Workspaces interface {
	// Prepare creates or refreshes the checkout spec describes, and returns its directory.
	Prepare(ctx context.Context, spec WorkspaceSpec) (string, error)
	// Fingerprint is a fingerprint of the folder's state, to check a read-only
	// session left it alone.
	Fingerprint(ctx context.Context, directory string) string
	// Restore puts a read-only folder back the way it was.
	Restore(ctx context.Context, directory string)
	// CleanUp removes the checkouts in folder. The pushed branch stays.
	CleanUp(ctx context.Context, repos []string, folder string)
	// MergeLegs merges the leg branches into the run's own branch, in the checkout spec describes.
	MergeLegs(ctx context.Context, branches []string, into WorkspaceSpec) MergeResult
	// Remove removes the checkouts in folder, including git worktree registration.
	// Missing folders are success. A failure leaves them in place and is reported.
	Remove(ctx context.Context, repos []string, folder string) error
}

// GitWorkspaces keeps the app's own clone of each repository, with a read-only
// checkout and a worktree per run beside it: repos/<owner>/<repo> and
// work/<run folder>/{readonly,worktree}. A pull request's checkout is its head
// instead of the base.
type GitWorkspaces struct {
	paths store.Paths
	shell shell.Runner
}

var _ Workspaces = GitWorkspaces{}

// NewGitWorkspaces is GitWorkspaces(paths:shell:). A nil shell uses shell.ProcessRunner.
func NewGitWorkspaces(paths store.Paths, sh shell.Runner) GitWorkspaces {
	if sh == nil {
		sh = shell.ProcessRunner{}
	}
	return GitWorkspaces{paths: paths, shell: sh}
}

// git runs git in directory and returns its stdout, or an error that names the subcommand.
func (g GitWorkspaces) git(ctx context.Context, arguments []string, directory string) (string, error) {
	result := shell.Bounded(ctx, g.shell, "git", arguments, directory, nil)
	if !result.OK() {
		subcommand := ""
		if len(arguments) > 0 {
			subcommand = arguments[0]
		}
		return "", core.NewFailure("git " + subcommand + ": " + result.Message())
	}
	return result.Stdout, nil
}

// clone clones the repository the first time and fetches it after that. It returns the clone's path.
func (g GitWorkspaces) clone(ctx context.Context, repo string) (string, error) {
	clone := g.paths.Clone(repo)
	if runnerPathExists(filepath.Join(clone, ".git")) {
		if _, err := g.git(ctx, []string{"fetch", "--prune", "origin"}, clone); err != nil {
			return "", err
		}
		return clone, nil
	}
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		return "", err
	}
	result := shell.Bounded(ctx, g.shell, "gh", []string{"repo", "clone", repo, clone}, "", nil)
	if !result.OK() {
		return "", core.NewFailure("gh repo clone " + repo + ": " + result.Message())
	}
	return clone, nil
}

// Prepare clones or fetches, then creates or refreshes the checkout: a detached
// read-only checkout of the base or the pull request head, or a worktree on the
// run's branch made from the base the first time.
func (g GitWorkspaces) Prepare(ctx context.Context, spec WorkspaceSpec) (string, error) {
	clone, err := g.clone(ctx, spec.Repo)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(spec.Folder, 0o755); err != nil {
		return "", err
	}
	folder := spec.CheckoutDirectory()
	if err := os.MkdirAll(filepath.Dir(folder), 0o755); err != nil {
		return "", err
	}
	exists := runnerPathExists(filepath.Join(folder, ".git"))
	switch spec.Checkout.Kind {
	case WorkspaceCheckoutKindBase, WorkspaceCheckoutKindPullRequest:
		ref := "origin/" + spec.Base
		if spec.Checkout.Kind == WorkspaceCheckoutKindPullRequest {
			// `pull/<n>/head` exists for pull requests from forks too.
			ref = "refs/remotes/origin/beltline-pr/" + strconv.Itoa(spec.Checkout.Number)
			if _, err := g.git(ctx, []string{"fetch", "--force", "origin", "+refs/pull/" + strconv.Itoa(spec.Checkout.Number) + "/head:" + ref}, clone); err != nil {
				return "", err
			}
		}
		if exists {
			if _, err := g.git(ctx, []string{"checkout", "--detach", "--force", ref}, folder); err != nil {
				return "", err
			}
			if _, err := g.git(ctx, []string{"clean", "-fdq"}, folder); err != nil {
				return "", err
			}
		} else {
			if _, err := g.git(ctx, []string{"worktree", "prune"}, clone); err != nil {
				return "", err
			}
			if _, err := g.git(ctx, []string{"worktree", "add", "--detach", folder, ref}, clone); err != nil {
				return "", err
			}
		}
	case WorkspaceCheckoutKindWorktree:
		if !exists {
			if _, err := g.git(ctx, []string{"worktree", "prune"}, clone); err != nil {
				return "", err
			}
			start := core.ValueOr(spec.From, "origin/"+spec.Base)
			if _, err := g.git(ctx, []string{"worktree", "add", "-B", spec.Branch, folder, start}, clone); err != nil {
				return "", err
			}
		}
	}
	return spec.Directory(), nil
}

// checkouts are the git checkouts inside a session's working folder.
func (g GitWorkspaces) checkouts(directory string) []string {
	if runnerPathExists(filepath.Join(directory, ".git")) {
		return []string{directory}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if runnerPathExists(filepath.Join(path, ".git")) {
			found = append(found, path)
		}
	}
	sort.Strings(found)
	return found
}

// Fingerprint is HEAD and the tracked-file status of every checkout. Untracked
// files do not count: running the tests leaves caches behind.
func (g GitWorkspaces) Fingerprint(ctx context.Context, directory string) string {
	var result strings.Builder
	for _, checkout := range g.checkouts(directory) {
		head, _ := g.git(ctx, []string{"rev-parse", "HEAD"}, checkout)
		status, _ := g.git(ctx, []string{"status", "--porcelain", "--untracked-files=no"}, checkout)
		result.WriteString(checkout + head + status)
	}
	return result.String()
}

// Restore resets every checkout in directory to its HEAD, discarding changes.
func (g GitWorkspaces) Restore(ctx context.Context, directory string) {
	for _, checkout := range g.checkouts(directory) {
		_, _ = g.git(ctx, []string{"reset", "--hard", "-q"}, checkout)
	}
}

// CleanUp removes each repository's worktrees in folder, then the folder.
func (g GitWorkspaces) CleanUp(ctx context.Context, repos []string, folder string) {
	for _, repo := range repos {
		clone := g.paths.Clone(repo)
		for _, checkout := range repoCheckouts(repo, repos, folder) {
			_ = shell.Bounded(ctx, g.shell, "git", []string{"worktree", "remove", "--force", checkout}, clone, nil)
		}
	}
	_ = os.RemoveAll(folder)
}

// failureText is why a git command failed, for a message. A command that was stopped
// is named by its tool and subcommand, so the timeout or cancellation reads as theirs;
// an ordinary failure keeps the text its output gives.
func failureText(tool string, arguments []string, result shell.Result) string {
	return result.MessageFor(tool + " " + shell.GitSubcommand(arguments))
}

// repoCheckouts are the checkouts of repo in a run's work folder, and in the
// folders of its legs under it, which work routes side by side.
func repoCheckouts(repo string, repos []string, folder string) []string {
	var legs []string
	if entries, err := os.ReadDir(folder); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if name == "readonly" || name == "worktree" {
				continue
			}
			if runnerIsRealDirectory(filepath.Join(folder, name)) {
				legs = append(legs, filepath.Join(folder, name))
			}
		}
	}
	sort.Strings(legs)
	var found []string
	for _, base := range append([]string{folder}, legs...) {
		for _, name := range []string{"readonly", "worktree"} {
			checkout := WorkspaceSpecCheckout(repo, repos, filepath.Join(base, name))
			if runnerPathExists(checkout) {
				found = append(found, checkout)
			}
		}
	}
	return found
}

// MergeLegs merges the branches the legs worked on into the run's own branch, and
// says which ones had commits, or which files conflicted. Nothing is pushed: the
// agent at the stage where the routes meet does that.
func (g GitWorkspaces) MergeLegs(ctx context.Context, branches []string, into WorkspaceSpec) MergeResult {
	if len(branches) == 0 {
		return MergeResult{Kind: MergeResultNothing}
	}
	if _, err := g.Prepare(ctx, into); err != nil {
		return MergeResult{Kind: MergeResultConflict, Names: []string{err.Error()}}
	}
	folder := into.CheckoutDirectory()
	// A leg that changed nothing contributes nothing.
	var ahead []string
	for _, branch := range branches {
		args := []string{"rev-list", "--count", "origin/" + into.Base + ".." + branch}
		result := shell.Bounded(ctx, g.shell, "git", args, folder, nil)
		if result.IsStopped() {
			// A leg that did not finish counting, stopped or cancelled, is not one that changed nothing.
			return MergeResult{Kind: MergeResultConflict, Names: []string{failureText("git", args, result)}}
		}
		if !result.OK() {
			continue
		}
		if count, err := strconv.Atoi(strings.TrimSpace(result.Stdout)); err == nil && count > 0 {
			ahead = append(ahead, branch)
		}
	}
	if len(ahead) == 0 {
		return MergeResult{Kind: MergeResultNothing}
	}
	for _, branch := range ahead {
		args := []string{"merge", "--no-edit", "--no-ff", branch}
		result := shell.Bounded(ctx, g.shell, "git", args, folder, nil)
		if result.OK() {
			continue
		}
		// The abort is bound to the caller like every other command, so a caller that has gone starts none.
		// The folder then keeps the unfinished merge until the next merge into it reports and aborts it.
		if result.IsStopped() {
			// A merge stopped part way is aborted like a conflicted one, and its stop is what is named.
			_ = shell.Bounded(ctx, g.shell, "git", []string{"merge", "--abort"}, folder, nil)
			return MergeResult{Kind: MergeResultConflict, Names: []string{failureText("git", args, result)}}
		}
		diff := []string{"diff", "--name-only", "--diff-filter=U"}
		files := shell.Bounded(ctx, g.shell, "git", diff, folder, nil)
		// Splitting at each newline, a CRLF is a separator too.
		names := strings.FieldsFunc(files.Stdout, core.IsNewline)
		_ = shell.Bounded(ctx, g.shell, "git", []string{"merge", "--abort"}, folder, nil)
		if files.IsStopped() {
			return MergeResult{Kind: MergeResultConflict, Names: []string{failureText("git", diff, files)}}
		}
		if len(names) == 0 {
			names = []string{branch}
		}
		return MergeResult{Kind: MergeResultConflict, Names: names}
	}
	return MergeResult{Kind: MergeResultMerged, Names: ahead}
}

// Remove removes each repository's worktrees in folder, then the folder. Failures
// are collected into one error, and the folder stays when they happen.
func (g GitWorkspaces) Remove(ctx context.Context, repos []string, folder string) error {
	if !runnerPathExists(folder) {
		return nil
	}
	var failures []string
	for _, repo := range repos {
		clone := g.paths.Clone(repo)
		for _, checkout := range repoCheckouts(repo, repos, folder) {
			args := []string{"worktree", "remove", "--force", checkout}
			result := shell.Bounded(ctx, g.shell, "git", args, clone, nil)
			if !result.OK() {
				failures = append(failures, repo+" "+filepath.Base(filepath.Dir(checkout))+": "+failureText("git", args, result))
			}
		}
	}
	if len(failures) > 0 {
		return core.NewFailure("Could not remove local checkouts (" + strings.Join(failures, "; ") + "). The run is still here; try erasing it again.")
	}
	if err := os.RemoveAll(folder); err != nil && runnerPathExists(folder) {
		return core.NewFailure("Could not remove the run's work folder: " + err.Error() + ". The run is still here; try erasing it again.")
	}
	return nil
}

// FolderWorkspaces are plain folders, for demo mode and tests.
type FolderWorkspaces struct{}

var _ Workspaces = FolderWorkspaces{}

// Prepare creates the checkout folder and returns its directory.
func (FolderWorkspaces) Prepare(ctx context.Context, spec WorkspaceSpec) (string, error) {
	if err := os.MkdirAll(spec.CheckoutDirectory(), 0o755); err != nil {
		return "", err
	}
	return spec.Directory(), nil
}

// Fingerprint is the sorted names in the folder.
func (FolderWorkspaces) Fingerprint(ctx context.Context, directory string) string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

// Restore is a no-op: plain folders have nothing to put back.
func (FolderWorkspaces) Restore(ctx context.Context, directory string) {}

// CleanUp removes the folder.
func (FolderWorkspaces) CleanUp(ctx context.Context, repos []string, folder string) {
	_ = os.RemoveAll(folder)
}

// MergeLegs is MergeResultNothing: plain folders have no branches to merge.
func (FolderWorkspaces) MergeLegs(ctx context.Context, branches []string, into WorkspaceSpec) MergeResult {
	return MergeResult{Kind: MergeResultNothing}
}

// Remove removes the folder. A folder that is still there after the failure is reported.
func (FolderWorkspaces) Remove(ctx context.Context, repos []string, folder string) error {
	if !runnerPathExists(folder) {
		return nil
	}
	if err := os.RemoveAll(folder); err != nil && runnerPathExists(folder) {
		return core.NewFailure("Could not remove the run's work folder: " + err.Error() + ". The run is still here; try erasing it again.")
	}
	return nil
}
