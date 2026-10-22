package runner

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// codexBypass is the flag that stops Codex's approval prompts and its sandbox.
const codexBypass = "--dangerously-bypass-approvals-and-sandbox"

// codexHeadless are the flags of every headless run: a folder workspace need not
// be a git repository.
var codexHeadless = []string{codexBypass, "--skip-git-repo-check"}

// CodexAdapter starts a stage in Codex's interactive interface, with the brief
// sent as its first prompt; the agent hands the work back and the stage stays
// open until the daemon ends it. Headless `codex exec` attempts from before that
// are still resumed by their old argument lists, as are Continue and the
// assistant. It runs outside Codex's sandbox, which would keep it from committing
// in a worktree or pushing. Rollouts are used only to discover a checkout's
// resume id, never its outcome.
type CodexAdapter struct {
	adapterDefaults
	home string
}

var _ AgentAdapter = CodexAdapter{}

// NewCodexAdapter is CodexAdapter(home:). A nil home uses the user's home folder.
func NewCodexAdapter(home *string) CodexAdapter {
	if home == nil {
		return CodexAdapter{home: userHome()}
	}
	return CodexAdapter{home: *home}
}

// Backend is the Codex CLI.
func (CodexAdapter) Backend() core.Backend { return core.BackendCodex }

// RunningNote explains an older headless `codex exec` attempt, which draws nothing
// until it ends. Interactive stages draw as they run and have no note.
func (CodexAdapter) RunningNote() *string {
	note := core.BackendCodex.Label() + " runs headless: nothing is drawn until the attempt ends."
	return &note
}

// Arguments is a headless `codex exec` with the brief.
func (c CodexAdapter) Arguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := append([]string{"exec"}, codexHeadless...)
	args = append(args, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// ResumeArguments is `codex exec resume` on the rollout token, or a fresh exec when there is none.
func (c CodexAdapter) ResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	if token == nil {
		return c.Arguments(sessionID, name, model, thinking)
	}
	args := append([]string{"exec", "resume"}, codexHeadless...)
	args = append(args, c.options(model, thinking)...)
	return append(args, *token, "$PROMPT")
}

// InteractiveArguments starts the interactive `codex` with the brief as its first
// prompt, bypassing approvals and the sandbox. It has no `exec` subcommand and no
// `--skip-git-repo-check`, which only `exec` accepts.
func (c CodexAdapter) InteractiveArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{codexBypass}
	args = append(args, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// InteractiveResumeArguments resumes the checkout's own rollout in the interactive
// `codex` with the brief. Without a verified token it returns nil, never a fresh
// conversation: the caller must refuse to launch.
func (c CodexAdapter) InteractiveResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	if token == nil {
		return nil
	}
	args := []string{"resume", *token, codexBypass}
	args = append(args, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// ContinueArguments resumes the rollout in Codex's interactive interface.
func (CodexAdapter) ContinueArguments(sessionID, name string, token *string) []string {
	var args []string
	if token != nil {
		args = []string{"resume", *token}
	}
	return append(args, codexBypass)
}

// ConversationArguments is an interactive Codex conversation with the prompt.
func (c CodexAdapter) ConversationArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{codexBypass}
	args = append(args, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// options are the -m and -c model_reasoning_effort flags, when set.
func (CodexAdapter) options(model string, thinking *core.Thinking) []string {
	var args []string
	if m := strings.TrimSpace(model); m != "" {
		args = append(args, "-m", m)
	}
	if level := core.BackendCodex.Thinking(thinking); level != nil {
		args = append(args, "-c", "model_reasoning_effort="+string(*level))
	}
	return args
}

// Ending is nil: Codex's outcome comes from the exit code and the output file.
func (CodexAdapter) Ending(directory, sessionID string, after int) *LogEnding { return nil }

// Mark is zero: Codex keeps no message count.
func (CodexAdapter) Mark(directory, sessionID string) int { return 0 }

// Reply is nil: Codex's replies are not read from its rollout.
func (CodexAdapter) Reply(directory, sessionID string, after int) *string { return nil }

// Usage is nil: Codex's usage needs the checkpoint, see UsageWithCheckpoint.
func (CodexAdapter) Usage(directory, sessionID, name string, after, marked int) *core.Usage {
	return nil
}

// UsageWithCheckpoint is the usage the rollout records after the checkpoint's totals.
func (c CodexAdapter) UsageWithCheckpoint(directory, sessionID, name string, after, marked int, checkpoint *core.UsageCheckpoint) *core.Usage {
	file := c.rollout(directory, checkpoint)
	if file == nil {
		return nil
	}
	text, ok := readText(*file)
	if !ok {
		return nil
	}
	var baseline *core.TokenTotals
	if checkpoint != nil && checkpoint.Totals != nil {
		baseline = checkpoint.Totals
	} else if checkpoint != nil && checkpoint.Preexisting != nil && checkpoint.SessionID == nil {
		zero := core.TokenTotalsZero
		baseline = &zero
	}
	return CodexUsageOf(text, baseline, nil)
}

// UsageCheckpoint is the rollout's id and totals, or the rollouts that already exist before a fresh launch.
func (c CodexAdapter) UsageCheckpoint(directory, sessionID, name string, previous *core.UsageCheckpoint) *core.UsageCheckpoint {
	if previous != nil && previous.SessionID != nil {
		id := *previous.SessionID
		file := c.file(id, directory)
		if file == nil {
			return &core.UsageCheckpoint{SessionID: &id}
		}
		text, ok := readText(*file)
		if !ok {
			return &core.UsageCheckpoint{SessionID: &id}
		}
		return &core.UsageCheckpoint{SessionID: &id, Totals: CodexBaseline(text)}
	}
	if previous != nil {
		return &core.UsageCheckpoint{}
	}
	found := c.rollouts(directory)
	ids := make([]string, 0, len(found))
	for _, rollout := range found {
		ids = append(ids, rollout.ID)
	}
	return &core.UsageCheckpoint{Preexisting: ids}
}

// UsageIdentity is the checkpoint's rollout id, or the one fresh rollout since the checkpoint.
func (c CodexAdapter) UsageIdentity(directory string, checkpoint *core.UsageCheckpoint) *string {
	if checkpoint != nil && checkpoint.SessionID != nil {
		return checkpoint.SessionID
	}
	if fresh := c.freshRollout(directory, checkpoint); fresh != nil {
		id := fresh.ID
		return &id
	}
	return nil
}

// UsageMark is zero: Codex uses the checkpoint's totals instead.
func (CodexAdapter) UsageMark(directory, sessionID, name string) int { return 0 }

// SessionToken scans the rollouts, newest first, for the one whose session_meta
// names this checkout. Missing or malformed metadata degrades to a fresh session,
// never a rollout belonging to a different checkout.
func (c CodexAdapter) SessionToken(directory string) *string {
	checkout := codexCanonicalPath(directory)
	if checkout == nil {
		return nil
	}
	type candidate struct {
		path    string
		modTime int64
	}
	var candidates []candidate
	for _, file := range c.walkRollouts() {
		// Lstat: a symbolic link is not a regular file, as resourceValues says.
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, candidate{path: file, modTime: info.ModTime().UnixNano()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime == candidates[j].modTime {
			return candidates[i].path > candidates[j].path
		}
		return candidates[i].modTime > candidates[j].modTime
	})
	for _, candidate := range candidates {
		text, ok := readRolloutHead(candidate.path)
		if !ok {
			continue
		}
		cwd := CodexWorkingDirectory(text)
		if cwd == nil {
			continue
		}
		if canonical := codexCanonicalPath(*cwd); canonical == nil || *canonical != *checkout {
			continue
		}
		id := CodexRolloutID(text)
		if id == nil {
			continue
		}
		return id
	}
	return nil
}

// codexCanonicalPath is the resolved path of an absolute existing path, or nil.
func codexCanonicalPath(path string) *string {
	if path == "" || !strings.HasPrefix(path, "/") {
		return nil
	}
	resolved, ok := realPath(path)
	if !ok {
		return nil
	}
	return &resolved
}

// codexMetadata is the payload of a rollout's first line when it is session_meta.
func codexMetadata(text string) map[string]any {
	line := firstLine(text)
	object := jsonObject(line)
	if typ, _ := asString(object["type"]); typ != "session_meta" {
		return nil
	}
	payload, _ := asObject(object["payload"])
	return payload
}

// CodexRolloutID is a rollout's session id from its first line, or nil.
func CodexRolloutID(text string) *string {
	id, ok := asString(codexMetadata(text)["id"])
	if !ok || id == "" {
		return nil
	}
	return &id
}

// CodexWorkingDirectory is the working directory a rollout's first line records, or nil.
func CodexWorkingDirectory(text string) *string {
	cwd, ok := asString(codexMetadata(text)["cwd"])
	if !ok {
		return nil
	}
	return &cwd
}

// codexFound is a rollout file and the session id it holds.
type codexFound struct {
	ID  string
	URL string
}

// readRolloutHead reads the start of a rollout file, up to its first line break
// and at most 64 KB. False when it is not UTF-8.
func readRolloutHead(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }() // read-only: nothing is written, so a close error loses nothing
	buffer := make([]byte, 65_536)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", false
	}
	head := buffer[:n]
	if i := strings.IndexByte(string(head), '\n'); i >= 0 {
		head = head[:i]
	}
	if !utf8.Valid(head) {
		return "", false
	}
	return string(head), true
}

// walkRollouts are the rollout files under ~/.codex/sessions, in a stable order.
func (c CodexAdapter) walkRollouts() []string {
	root := filepath.Join(c.home, ".codex", "sessions")
	var files []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "rollout-") && filepath.Ext(entry.Name()) == ".jsonl" {
			files = append(files, path)
		}
		return nil
	})
	return files
}

// rollouts are the rollouts under ~/.codex/sessions whose working directory is this checkout.
func (c CodexAdapter) rollouts(directory string) []codexFound {
	checkout := codexCanonicalPath(directory)
	if checkout == nil {
		return nil
	}
	var found []codexFound
	for _, file := range c.walkRollouts() {
		text, ok := readRolloutHead(file)
		if !ok {
			continue
		}
		cwd := CodexWorkingDirectory(text)
		if cwd == nil {
			continue
		}
		canonical := codexCanonicalPath(*cwd)
		if canonical == nil || *canonical != *checkout {
			continue
		}
		id := CodexRolloutID(text)
		if id == nil {
			continue
		}
		found = append(found, codexFound{ID: *id, URL: file})
	}
	return found
}

// file is the rollout file with this session id for the checkout, or nil.
func (c CodexAdapter) file(id, directory string) *string {
	for _, rollout := range c.rollouts(directory) {
		if rollout.ID == id {
			url := rollout.URL
			return &url
		}
	}
	return nil
}

// freshRollout is the one rollout created after the checkpoint's preexisting
// ids were saved. Zero or several is ambiguous, so nil.
func (c CodexAdapter) freshRollout(directory string, checkpoint *core.UsageCheckpoint) *codexFound {
	if checkpoint == nil || checkpoint.Preexisting == nil || checkpoint.SessionID != nil {
		return nil
	}
	var fresh []codexFound
	for _, rollout := range c.rollouts(directory) {
		if !slices.Contains(checkpoint.Preexisting, rollout.ID) {
			fresh = append(fresh, rollout)
		}
	}
	if len(fresh) != 1 {
		return nil
	}
	return &fresh[0]
}

// rollout is the rollout file for the checkpoint's session id, or the fresh one, or nil.
func (c CodexAdapter) rollout(directory string, checkpoint *core.UsageCheckpoint) *string {
	if checkpoint != nil && checkpoint.SessionID != nil {
		return c.file(*checkpoint.SessionID, directory)
	}
	if fresh := c.freshRollout(directory, checkpoint); fresh != nil {
		url := fresh.URL
		return &url
	}
	return nil
}
