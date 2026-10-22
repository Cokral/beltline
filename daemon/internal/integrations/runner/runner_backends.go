package runner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// AgentAdapter is how one agent CLI is started and how its session log says it
// is done. A directory is a
// filesystem path.
type AgentAdapter interface {
	// Backend is the CLI this adapter starts.
	Backend() core.Backend
	// RunningNote is why a running headless stage has no live terminal output, or
	// nil. Interactive stages draw as they run.
	RunningNote() *string
	// Arguments is the command line that runs the brief in a directory. `$PROMPT`
	// in an argument is replaced by the prompt's text.
	Arguments(sessionID, name, model string, thinking *core.Thinking) []string
	// ResumeArguments is like Arguments, but continues the earlier session with
	// a new prompt. token is the rollout or chat id to resume, when there is one.
	ResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string
	// InteractiveArguments starts a stage in the runner's own interface with the
	// brief (`$PROMPT`) already sent, never stopping to ask permission. The
	// interface stays open after the turn; the agent hands the work back.
	InteractiveArguments(sessionID, name, model string, thinking *core.Thinking) []string
	// InteractiveResumeArguments is like InteractiveArguments, but continues the
	// earlier conversation with a new prompt. token is as for ResumeArguments.
	InteractiveResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string
	// ContinueArguments is the command line that continues the session interactively.
	ContinueArguments(sessionID, name string, token *string) []string
	// ConversationArguments is a persistent interactive conversation, never headless.
	ConversationArguments(sessionID, name, model string, thinking *core.Thinking) []string
	// SessionToken is a checkout-scoped session id, for backends that discover it on disk, or nil.
	SessionToken(directory string) *string
	// CreateSessionArguments is the command line that creates a session and prints
	// its id, for backends that hand out their own ids; nil when they take Beltline's.
	CreateSessionArguments() []string
	// Ending is what the agent's own session log says about how it ended, after
	// its first `after` assistant messages; nil when it says nothing.
	Ending(directory, sessionID string, after int) *LogEnding
	// Mark is how many assistant messages the session's log holds so far.
	Mark(directory, sessionID string) int
	// Reply is the text of the agent's last reply after its first `after`
	// assistant messages, when its log keeps one.
	Reply(directory, sessionID string, after int) *string
	// Usage is the usage of the attempt, without a checkpoint.
	Usage(directory, sessionID, name string, after, marked int) *core.Usage
	// UsageWithCheckpoint is Usage counted from a checkpoint.
	UsageWithCheckpoint(directory, sessionID, name string, after, marked int, checkpoint *core.UsageCheckpoint) *core.Usage
	// UsageMark is the usage counters observed before launch.
	UsageMark(directory, sessionID, name string) int
	// UsageCheckpoint is the counters and identity observed before launch.
	// previous is the attempt being resumed.
	UsageCheckpoint(directory, sessionID, name string, previous *core.UsageCheckpoint) *core.UsageCheckpoint
	// UsageIdentity is the native session id to store once the attempt's log can
	// be told apart; nil when it is ambiguous.
	UsageIdentity(directory string, checkpoint *core.UsageCheckpoint) *string
}

// adapterDefaults are the AgentAdapter protocol extension's defaults that call
// no other adapter method: nil for each. Every adapter embeds it and declares
// the methods it overrides.
type adapterDefaults struct{}

// RunningNote is nil: a runner with a live terminal has nothing to explain.
func (adapterDefaults) RunningNote() *string { return nil }

// SessionToken is nil: most backends do not discover a session id on disk.
func (adapterDefaults) SessionToken(directory string) *string { return nil }

// CreateSessionArguments is nil: the backend takes Beltline's session id.
func (adapterDefaults) CreateSessionArguments() []string { return nil }

// UsageCheckpoint is nil: the backend keeps no per-attempt counters.
func (adapterDefaults) UsageCheckpoint(directory, sessionID, name string, previous *core.UsageCheckpoint) *core.UsageCheckpoint {
	return nil
}

// UsageIdentity is nil: there is no native session id to store.
func (adapterDefaults) UsageIdentity(directory string, checkpoint *core.UsageCheckpoint) *string {
	return nil
}

// LogEndingKind says whether a session log reports a finish or a failure.
type LogEndingKind string

const (
	// LogEndingFinished is a session that completed its task.
	LogEndingFinished LogEndingKind = "finished"
	// LogEndingFailed is a session that stopped on an error.
	LogEndingFailed LogEndingKind = "failed"
)

// LogEnding is what an agent's own session log says about how it ended.
// Message is set for LogEndingFailed: the agent stopped on an error
// its log reports, such as its provider refusing the request.
type LogEnding struct {
	Kind    LogEndingKind
	Message string
}

// AdapterFor is the adapter that starts a backend.
func AdapterFor(backend core.Backend) AgentAdapter {
	switch backend {
	case core.BackendCopilot:
		return NewCopilotAdapter(nil)
	case core.BackendPi:
		return PiAdapter{}
	case core.BackendClaude:
		return ClaudeAdapter{}
	case core.BackendCodex:
		return NewCodexAdapter(nil)
	case core.BackendCursor:
		return CursorAdapter{}
	}
	panic("runner: no adapter for backend " + string(backend))
}

// CopilotAdapter starts Copilot stages interactively and runs the brief in
// autopilot; the session stays at the prompt after `session.task_complete`.
// Continue stays interactive without autopilot. The configuration assistant
// uses ConversationArguments.
type CopilotAdapter struct {
	adapterDefaults
	// StateRoot is the folder Copilot keeps its session state in.
	StateRoot string
}

var _ AgentAdapter = CopilotAdapter{}

// CopilotLongContext is the CLI's long-context tier, and the suffix that picks
// it in a model id (`claude-opus-4.6[long_context]`), as `[1m]` does for Claude.
const CopilotLongContext = "long_context"

// NewCopilotAdapter is CopilotAdapter(stateRoot:). A nil stateRoot uses
// CopilotDefaultStateRoot.
func NewCopilotAdapter(stateRoot *string) CopilotAdapter {
	if stateRoot == nil {
		return CopilotAdapter{StateRoot: CopilotDefaultStateRoot()}
	}
	return CopilotAdapter{StateRoot: *stateRoot}
}

// CopilotDefaultStateRoot is BELTLINE_COPILOT_STATE when it is set and not
// empty, otherwise ~/.copilot/session-state.
func CopilotDefaultStateRoot() string {
	if path := os.Getenv("BELTLINE_COPILOT_STATE"); path != "" {
		return path
	}
	return filepath.Join(userHome(), ".copilot", "session-state")
}

// CopilotLongContextID is `<model>[long_context]`, the id that selects the long tier.
func CopilotLongContextID(model string) string { return model + "[" + CopilotLongContext + "]" }

// CopilotModelArguments is `--model`, plus `--context long_context` for a
// long-context id. Empty for a blank model.
func CopilotModelArguments(model string) []string {
	model = trimWhitespaces(model)
	if model == "" {
		return nil
	}
	suffix := "[" + CopilotLongContext + "]"
	if !strings.HasSuffix(model, suffix) {
		return []string{"--model", model}
	}
	return []string{"--model", strings.TrimSuffix(model, suffix), "--context", CopilotLongContext}
}

// CopilotIsCompletion reports whether a line of Copilot's `events.jsonl` says a
// task finished (`session.task_complete`).
func CopilotIsCompletion(line string) bool {
	object := jsonObject(line)
	typ, _ := asString(object["type"])
	return typ == "session.task_complete"
}

// Backend is Copilot.
func (CopilotAdapter) Backend() core.Backend { return core.BackendCopilot }

// Arguments starts a managed, autopilot session with the brief as its prompt.
func (c CopilotAdapter) Arguments(sessionID, name, model string, thinking *core.Thinking) []string {
	return append(c.managed(model, thinking), "--name="+core.PrefixCharacters(name, 100))
}

// ResumeArguments resumes the named session with a new prompt.
func (c CopilotAdapter) ResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	return append(c.managed(model, thinking), "--resume="+core.PrefixCharacters(name, 100))
}

// InteractiveArguments is Arguments: Copilot stages already run interactively.
func (c CopilotAdapter) InteractiveArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	return c.Arguments(sessionID, name, model, thinking)
}

// InteractiveResumeArguments is ResumeArguments: Copilot stages already run interactively.
func (c CopilotAdapter) InteractiveResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	return c.ResumeArguments(sessionID, name, model, thinking, token)
}

// ContinueArguments continues the named session interactively, without autopilot.
func (CopilotAdapter) ContinueArguments(sessionID, name string, token *string) []string {
	return []string{"--resume=" + core.PrefixCharacters(name, 100), "--allow-all"}
}

// ConversationArguments is an interactive conversation with the prompt.
func (CopilotAdapter) ConversationArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{"--name=" + core.PrefixCharacters(name, 100), "--interactive", "$PROMPT"}
	args = append(args, CopilotModelArguments(model)...)
	return append(args, reasoningFlag("--reasoning-effort", core.BackendCopilot, thinking)...)
}

// Ending is nil: Copilot's completion is read from its session log elsewhere.
func (CopilotAdapter) Ending(directory, sessionID string, after int) *LogEnding { return nil }

// Mark is zero: Copilot's marks come from its usage events.
func (CopilotAdapter) Mark(directory, sessionID string) int { return 0 }

// Reply is nil: Copilot's replies are not read from its log.
func (CopilotAdapter) Reply(directory, sessionID string, after int) *string { return nil }

// UsageWithCheckpoint is Usage: the protocol default ignores the checkpoint.
func (c CopilotAdapter) UsageWithCheckpoint(directory, sessionID, name string, after, marked int, checkpoint *core.UsageCheckpoint) *core.Usage {
	return c.Usage(directory, sessionID, name, after, marked)
}

// managed is the autopilot command line: the brief, then the interactive prompt,
// with the model and reasoning effort.
func (c CopilotAdapter) managed(model string, thinking *core.Thinking) []string {
	args := []string{"--interactive", "$PROMPT", "--allow-all", "--no-ask-user", "--autopilot"}
	args = append(args, CopilotModelArguments(model)...)
	return append(args, reasoningFlag("--reasoning-effort", core.BackendCopilot, thinking)...)
}

// PiAdapter starts Pi stages in Pi's fullscreen interface with the brief as the
// prompt and `--approve`, so an unattended stage never stops to ask; the session
// stays open after the turn until the agent's handoff ends it. Arguments and
// ResumeArguments are the headless `--print` shapes, kept for attempts started
// before stages launched interactively. Its session log supplies replies and
// usage; Continue and the assistant stay interactive.
type PiAdapter struct {
	adapterDefaults
}

var _ AgentAdapter = PiAdapter{}

// Backend is Pi.
func (PiAdapter) Backend() core.Backend { return core.BackendPi }

// RunningNote says a headless Pi attempt draws nothing until it ends. Only older
// headless attempts have this note: interactive stages draw as they run.
func (PiAdapter) RunningNote() *string {
	note := core.BackendPi.Label() + " runs headless: nothing is drawn until the attempt ends."
	return &note
}

// options are the --model and --thinking flags, when set.
func (PiAdapter) options(model string, thinking *core.Thinking) []string {
	var args []string
	if m := trimWhitespaces(model); m != "" {
		args = append(args, "--model", m)
	}
	return append(args, reasoningFlag("--thinking", core.BackendPi, thinking)...)
}

// Arguments is a headless --print turn with the session id and name.
func (p PiAdapter) Arguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{"--print", "--approve", "--session-id", sessionID, "--name", name}
	args = append(args, p.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// ResumeArguments selects the existing session, rather than creating one or opening a picker.
func (p PiAdapter) ResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	args := []string{"--print", "--approve", "--session", sessionID}
	args = append(args, p.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// InteractiveArguments opens a new session in Pi's fullscreen interface with the
// brief as its prompt, and `--approve` so it never stops to ask permission.
func (p PiAdapter) InteractiveArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{"--tui-mode", "fullscreen", "--approve", "--session-id", sessionID, "--name", name}
	args = append(args, p.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// InteractiveResumeArguments continues the existing session in Pi's fullscreen
// interface with a new prompt, and `--approve` as InteractiveArguments does.
func (p PiAdapter) InteractiveResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	args := []string{"--tui-mode", "fullscreen", "--approve", "--session", sessionID}
	args = append(args, p.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// ContinueArguments opens the session in Pi's fullscreen interface.
func (PiAdapter) ContinueArguments(sessionID, name string, token *string) []string {
	return []string{"--tui-mode", "fullscreen", "--session", sessionID}
}

// ConversationArguments opens a new session in Pi's fullscreen interface.
func (p PiAdapter) ConversationArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{"--tui-mode", "fullscreen", "--session-id", sessionID, "--name", name}
	args = append(args, p.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// Ending is nil: only the wrapper's exit code says how a headless turn ended.
func (PiAdapter) Ending(directory, sessionID string, after int) *LogEnding { return nil }

// Mark is the number of assistant messages in the session's log.
func (PiAdapter) Mark(directory, sessionID string) int {
	text := piLog(directory, sessionID)
	if text == nil {
		return 0
	}
	count := 0
	for _, line := range splitLines(*text, true) {
		if piAssistantMessage(line) != nil {
			count++
		}
	}
	return count
}

// Reply is the last assistant text after the first `after` assistant messages.
func (PiAdapter) Reply(directory, sessionID string, after int) *string {
	text := piLog(directory, sessionID)
	if text == nil {
		return nil
	}
	return PiReplyOf(*text, after)
}

// UsageWithCheckpoint is Usage: the protocol default ignores the checkpoint.
func (c PiAdapter) UsageWithCheckpoint(directory, sessionID, name string, after, marked int, checkpoint *core.UsageCheckpoint) *core.Usage {
	return c.Usage(directory, sessionID, name, after, marked)
}

// PiReplyOf is the text of the last assistant message with any, after the first
// `after` assistant messages.
func PiReplyOf(log string, after int) *string {
	var reply *string
	seen := 0
	for _, line := range splitLines(log, true) {
		message := piAssistantMessage(line)
		if message == nil {
			continue
		}
		seen++
		if seen <= after {
			continue
		}
		parts, _ := asObjects(message["content"])
		var texts []string
		for _, part := range parts {
			if kind, _ := asString(part["type"]); kind != "text" {
				continue
			}
			if text, ok := asString(part["text"]); ok {
				texts = append(texts, text)
			}
		}
		text := strings.Join(texts, "\n")
		if strings.TrimSpace(text) != "" {
			reply = &text
		}
	}
	return reply
}

// piLog is the session log's text, or nil when it is missing.
func piLog(directory, sessionID string) *string {
	path := piSessionLog(directory, sessionID)
	if path == nil {
		return nil
	}
	text, ok := readText(*path)
	if !ok {
		return nil
	}
	return &text
}

// piAssistantMessage is the message object of an assistant line, or nil for any other line.
func piAssistantMessage(line string) map[string]any {
	object := jsonObject(line)
	if typ, _ := asString(object["type"]); typ != "message" {
		return nil
	}
	message, ok := asObject(object["message"])
	if !ok {
		return nil
	}
	if role, _ := asString(message["role"]); role != "assistant" {
		return nil
	}
	return message
}

// PiSessionDirectory is ~/.pi/agent/sessions/--<path with dashes>--/, as Pi names it.
func PiSessionDirectory(directory string) string {
	path, ok := realPath(directory)
	if !ok {
		path = directory
	}
	name := "--" + strings.ReplaceAll(strings.ReplaceAll(strings.Trim(path, "/"), "/", "-"), ":", "-") + "--"
	return filepath.Join(userHome(), ".pi", "agent", "sessions", name)
}

// piSessionLog is the session's .jsonl file in its folder, or nil when there is none.
func piSessionLog(directory, sessionID string) *string {
	folder := PiSessionDirectory(directory)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) == ".jsonl" && strings.Contains(name, sessionID) {
			path := filepath.Join(folder, name)
			return &path
		}
	}
	return nil
}

// reasoningFlag is the flag and level for a thinking setting the backend takes,
// or nothing when it takes none or the setting is unset.
func reasoningFlag(flag string, backend core.Backend, thinking *core.Thinking) []string {
	level := backend.Thinking(thinking)
	if level == nil {
		return nil
	}
	return []string{flag, string(*level)}
}
