package runner

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// ClaudeAdapter runs a Claude stage in Claude Code's own interactive interface,
// where you can answer it; the stage's handoff command ends the turn and the
// daemon ends the session. Attempts started before that ran headless with `-p`,
// which exits when it is done, and Continue and the assistant still use the
// interactive forms of the same arguments.
type ClaudeAdapter struct {
	adapterDefaults
}

var _ AgentAdapter = ClaudeAdapter{}

// Backend is Claude Code.
func (ClaudeAdapter) Backend() core.Backend { return core.BackendClaude }

// RunningNote says a headless Claude attempt draws nothing until it ends. Only
// attempts started before interactive stages have that; interactive ones draw
// their own terminal.
func (ClaudeAdapter) RunningNote() *string {
	note := core.BackendClaude.Label() + " runs headless: nothing is drawn until the attempt ends."
	return &note
}

// options are the permission-bypass flags, the --model and the --effort, when set.
func (ClaudeAdapter) options(model string, thinking *core.Thinking) []string {
	args := []string{"--allow-dangerously-skip-permissions", "--permission-mode", "bypassPermissions"}
	if m := strings.TrimSpace(model); m != "" {
		args = append(args, "--model", m)
	}
	return append(args, reasoningFlag("--effort", core.BackendClaude, thinking)...)
}

// Arguments is a headless `-p` turn with a new session id.
func (c ClaudeAdapter) Arguments(sessionID, name, model string, thinking *core.Thinking) []string {
	return append([]string{"-p", "--output-format", "text"}, c.interactive(sessionID, name, model, thinking)...)
}

// ResumeArguments is a headless `-p` turn that resumes the session.
func (c ClaudeAdapter) ResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	args := []string{"-p", "--output-format", "text", "--resume", sessionID}
	args = append(args, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// interactive is the session id, name and options, with the prompt.
func (c ClaudeAdapter) interactive(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{"--session-id", sessionID, "-n", name}
	args = append(args, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// InteractiveArguments starts the session in Claude Code's interactive interface
// with the brief as its first message, and no `-p`.
func (c ClaudeAdapter) InteractiveArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	return c.interactive(sessionID, name, model, thinking)
}

// InteractiveResumeArguments resumes the session in Claude Code's interactive
// interface with the brief as its next message, and no `-p`.
func (c ClaudeAdapter) InteractiveResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	args := append([]string{"--resume", sessionID}, c.options(model, thinking)...)
	return append(args, "$PROMPT")
}

// ContinueArguments resumes the session in Claude's interactive interface.
func (c ClaudeAdapter) ContinueArguments(sessionID, name string, token *string) []string {
	return append([]string{"--resume", sessionID}, c.options("", nil)...)
}

// ConversationArguments is an interactive conversation with a new session id.
func (c ClaudeAdapter) ConversationArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	return c.interactive(sessionID, name, model, thinking)
}

// Ending is what the session's transcript says about a failed turn, after the
// first `after` assistant messages. The exit code says when it finished.
func (c ClaudeAdapter) Ending(directory, sessionID string, after int) *LogEnding {
	text := c.log(directory, sessionID)
	if text == nil {
		return nil
	}
	return ClaudeEndingOf(*text, after)
}

// Mark is the number of assistant messages in the session's transcript.
func (c ClaudeAdapter) Mark(directory, sessionID string) int {
	text := c.log(directory, sessionID)
	if text == nil {
		return 0
	}
	return ClaudeMarkOf(*text, 0)
}

// Reply is the last assistant text after the first `after` assistant messages.
func (c ClaudeAdapter) Reply(directory, sessionID string, after int) *string {
	text := c.log(directory, sessionID)
	if text == nil {
		return nil
	}
	return ClaudeReplyOf(*text, after)
}

// Usage is nil: Claude's usage needs the checkpoint, see UsageWithCheckpoint.
func (ClaudeAdapter) Usage(directory, sessionID, name string, after, marked int) *core.Usage {
	return nil
}

// UsageWithCheckpoint is the usage the transcript records after the checkpoint's line count.
func (c ClaudeAdapter) UsageWithCheckpoint(directory, sessionID, name string, after, marked int, checkpoint *core.UsageCheckpoint) *core.Usage {
	text := c.log(directory, sessionID)
	if text == nil {
		return nil
	}
	var lines *int
	if checkpoint != nil {
		lines = checkpoint.Lines
	}
	return ClaudeUsageUsage(*text, lines, sessionID, nil)
}

// UsageCheckpoint is the transcript's line count before launch.
func (c ClaudeAdapter) UsageCheckpoint(directory, sessionID, name string, previous *core.UsageCheckpoint) *core.UsageCheckpoint {
	text := ""
	if found := c.log(directory, sessionID); found != nil {
		text = *found
	}
	lines := len(ClaudeUsageLines(text))
	return &core.UsageCheckpoint{Lines: &lines}
}

// UsageMark is zero: Claude's usage uses the line checkpoint instead.
func (ClaudeAdapter) UsageMark(directory, sessionID, name string) int { return 0 }

// ClaudeSessionDirectory is ~/.claude/projects/<cwd>/, where Claude writes the
// real path with every character other than a letter or digit as a dash.
func ClaudeSessionDirectory(directory string) string {
	path, ok := realPath(directory)
	if !ok {
		path = directory
	}
	// One dash per Character: a letter or digit kept only when the Character is a
	// single ASCII one.
	var name strings.Builder
	for _, ch := range core.Characters(path) {
		if r, size := utf8.DecodeRuneInString(ch); size == len(ch) && r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			name.WriteString(ch)
		} else {
			name.WriteByte('-')
		}
	}
	return filepath.Join(claudeProjects(), name.String())
}

// claudeProjects is ~/.claude/projects, where Claude keeps its transcripts.
func claudeProjects() string {
	return filepath.Join(userHome(), ".claude", "projects")
}

// log is the session's transcript text, found in its folder or else in any project folder.
func (ClaudeAdapter) log(directory, sessionID string) *string {
	file := sessionID + ".jsonl"
	if text, ok := readText(filepath.Join(ClaudeSessionDirectory(directory), file)); ok {
		return &text
	}
	// Claude shortens long folder names, so look for the session's own file.
	entries, err := os.ReadDir(claudeProjects())
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if text, ok := readText(filepath.Join(claudeProjects(), entry.Name(), file)); ok {
			return &text
		}
	}
	return nil
}

// claudeMessages are the main-thread assistant messages of a transcript.
func claudeMessages(log string) []map[string]any {
	var messages []map[string]any
	for _, line := range splitLines(log, true) {
		object := jsonObject(line)
		if typ, _ := asString(object["type"]); typ != "assistant" {
			continue
		}
		if sidechain, _ := object["isSidechain"].(bool); sidechain {
			continue
		}
		message, ok := asObject(object["message"])
		if !ok {
			continue
		}
		if role, _ := asString(message["role"]); role != "assistant" {
			continue
		}
		messages = append(messages, message)
	}
	return messages
}

// claudeText is the joined text blocks of an assistant message.
func claudeText(message map[string]any) string {
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
	return strings.Join(texts, "\n")
}

// ClaudeMarkOf is the number of assistant messages after the first `after`.
func ClaudeMarkOf(log string, after int) int {
	count := len(claudeMessages(log)) - after
	if count < 0 {
		return 0
	}
	return count
}

// ClaudeReplyOf is the last non-empty assistant reply after the first `after` messages.
func ClaudeReplyOf(log string, after int) *string {
	messages := claudeMessages(log)
	after = max(after, 0)
	if after >= len(messages) {
		return nil
	}
	reply := claudeText(messages[len(messages)-1])
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	return &reply
}

// ClaudeEndingOf is a failure when the last assistant turn after the first `after`
// messages is a synthetic stop that was not undone by a later user prompt.
func ClaudeEndingOf(log string, after int) *LogEnding {
	seen := 0
	var failure *string
	for _, line := range splitLines(log, true) {
		object := jsonObject(line)
		if object == nil {
			continue
		}
		if sidechain, _ := object["isSidechain"].(bool); sidechain {
			continue
		}
		typ, _ := asString(object["type"])
		if typ == "user" {
			failure = nil
			continue
		}
		if typ != "assistant" {
			continue
		}
		message, ok := asObject(object["message"])
		if !ok {
			continue
		}
		if role, _ := asString(message["role"]); role != "assistant" {
			continue
		}
		seen++
		if seen <= after {
			continue
		}
		body := strings.TrimSpace(claudeText(message))
		model, _ := asString(message["model"])
		reason, _ := asString(message["stop_reason"])
		if model == "<synthetic>" && reason == "stop_sequence" && body != "No response requested." {
			text := core.PrefixCharacters(body, 300)
			failure = &text
		} else {
			failure = nil
		}
	}
	if failure == nil {
		return nil
	}
	return &LogEnding{Kind: LogEndingFailed, Message: *failure}
}
