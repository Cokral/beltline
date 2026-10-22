package runner

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// SessionSpec is everything needed to start one agent session. Directory,
// AttemptFolder and Output are filesystem paths.
// NewSessionSpec defaults AgentName to Name.
type SessionSpec struct {
	Name      string
	SessionID string
	Backend   core.Backend
	Model     string
	// Thinking is nil for the runner's default.
	Thinking *core.Thinking
	Prompt   string
	// Directory is where the agent works.
	Directory string
	// AttemptFolder is where the brief, the wrapper script, the exit code and the log go.
	AttemptFolder string
	// Output is the file the agent writes; demo mode writes it itself.
	Output string
	// Outcomes are the stage's outcomes, for demo mode to pick from.
	Outcomes []string
	// AgentID is the library agent the stage uses, for demo mode to write its output.
	AgentID *string
	// Resume is whether this continues the agent's earlier session rather than starting one.
	Resume bool
	// AgentName is the name the agent knows its session by: Name, or a continued session's first name.
	AgentName string
	// LogMark is the assistant messages in the agent's log before this attempt started.
	LogMark   int
	UsageMark int
	// UsageCheckpoint is per-category baseline and native session identity. Nil for backends that only use UsageMark.
	UsageCheckpoint *core.UsageCheckpoint
	// ResumeToken is the rollout or chat id to resume. Codex refuses a fresh exec when this continuation has none.
	ResumeToken *string
	// Interactive is set for attempts launched in the runner's own interface, which stays open: they
	// finish on CompletionToken's handoff, or, for an older Copilot attempt without one, on a new task
	// event. Headless attempts leave it false and finish when the process exits.
	Interactive bool
	// CompletionToken is this launch's handoff token; empty on headless and older Copilot attempts.
	CompletionToken string
	// CopilotState overrides Copilot's session-state directory. Nil uses the default, or BELTLINE_COPILOT_STATE.
	CopilotState *string
}

// SessionSpecArgs are the parameters of NewSessionSpec. AgentName nil means Name.
type SessionSpecArgs struct {
	Name            string
	SessionID       string
	Backend         core.Backend
	Model           string
	Thinking        *core.Thinking
	Prompt          string
	Directory       string
	AttemptFolder   string
	Output          string
	Outcomes        []string
	AgentID         *string
	Resume          bool
	AgentName       *string
	LogMark         int
	UsageMark       int
	UsageCheckpoint *core.UsageCheckpoint
	ResumeToken     *string
	Interactive     bool
	CompletionToken string
	CopilotState    *string
}

// NewSessionSpec is SessionSpec(name:sessionId:...:agentName:...).
func NewSessionSpec(a SessionSpecArgs) SessionSpec {
	agentName := core.ValueOr(a.AgentName, a.Name)
	return SessionSpec{
		Name:            a.Name,
		SessionID:       a.SessionID,
		Backend:         a.Backend,
		Model:           a.Model,
		Thinking:        a.Thinking,
		Prompt:          a.Prompt,
		Directory:       a.Directory,
		AttemptFolder:   a.AttemptFolder,
		Output:          a.Output,
		Outcomes:        append([]string(nil), a.Outcomes...),
		AgentID:         a.AgentID,
		Resume:          a.Resume,
		AgentName:       agentName,
		LogMark:         a.LogMark,
		UsageMark:       a.UsageMark,
		UsageCheckpoint: a.UsageCheckpoint,
		ResumeToken:     a.ResumeToken,
		Interactive:     a.Interactive,
		CompletionToken: a.CompletionToken,
		CopilotState:    a.CopilotState,
	}
}

// SessionInterruptKind says whether an interrupt stopped the turn.
type SessionInterruptKind string

const (
	// SessionInterruptConfirmed is an interrupt that stopped the turn and left the conversation in place.
	SessionInterruptConfirmed SessionInterruptKind = "confirmed"
	// SessionInterruptFailed is an interrupt that did not stop the turn.
	SessionInterruptFailed SessionInterruptKind = "failed"
)

// SessionInterrupt is the outcome of an interrupt. Conversation is set when
// confirmed and the conversation can be resumed; Message when it failed.
type SessionInterrupt struct {
	Kind         SessionInterruptKind
	Conversation *string
	Message      string
}

// SessionStatusKind is how a session stands.
type SessionStatusKind string

const (
	// SessionStatusRunning is a session still going.
	SessionStatusRunning SessionStatusKind = "running"
	// SessionStatusFinished is a session that finished. ExitCode is nil when only its log said so.
	SessionStatusFinished SessionStatusKind = "finished"
	// SessionStatusFailed is a session that stopped on an error its log reports; Message says why.
	SessionStatusFailed SessionStatusKind = "failed"
	// SessionStatusGone is a session that is gone without saying how it ended.
	SessionStatusGone SessionStatusKind = "gone"
)

// SessionStatus is a session's state, with its exit code when finished and its message when failed.
type SessionStatus struct {
	Kind     SessionStatusKind
	ExitCode *int32
	Message  string
}

// AgentRunner starts, watches and stops agent sessions. TmuxRunner runs real
// agents; demo mode and tests use SimulatedRunner. Methods with defaults are
// documented on the implementations, which declare them.
type AgentRunner interface {
	// PrepareSessionID allocates an explicit chat identity before recording and launching it.
	PrepareSessionID(ctx context.Context, backend core.Backend, proposed string, directory string) (string, error)
	// Start launches the session.
	Start(ctx context.Context, spec SessionSpec) error
	// StartConversation launches an interactive conversation.
	StartConversation(ctx context.Context, spec ConversationSpec) error
	// ConversationAlive reports whether the conversation session is still running.
	ConversationAlive(ctx context.Context, session string) bool
	// Status reports how the session stands.
	Status(ctx context.Context, spec SessionSpec) SessionStatus
	// Finish saves the session's scrollback to session.log and ends it.
	Finish(ctx context.Context, spec SessionSpec)
	// LiveSessionNames is every session name running now, read in one query, so a pass
	// over many sessions asks once rather than once per session.
	LiveSessionNames(ctx context.Context) map[string]bool
	// EndContinuing saves Continue scrollback separately and ends only the interactive session.
	EndContinuing(ctx context.Context, spec SessionSpec)
	// Stop ends the session.
	Stop(ctx context.Context, session string)
	// Suspend stops the agent in session without ending it, so continuing resumes it intact.
	Suspend(ctx context.Context, session string)
	// Resume wakes a suspended session again.
	Resume(ctx context.Context, session string)
	// EnsureStopped stops session and reports a failure. Erasure uses this so a
	// session that will not stop leaves the run in place.
	EnsureStopped(ctx context.Context, session string) error
	// AttachCommand is the command that attaches a terminal to the session, if it is alive; nil otherwise.
	AttachCommand(ctx context.Context, session string, readOnly bool) []string
	// Reply is the agent's last reply in this attempt, from its own session log.
	Reply(ctx context.Context, spec SessionSpec) *string
	// Usage is the attempt's usage, or nil.
	Usage(ctx context.Context, spec SessionSpec) *core.Usage
	// UsageMark is the usage counter observed before launch.
	UsageMark(ctx context.Context, spec SessionSpec) int
	// UsageCheckpoint is the counters and identity observed before launch.
	UsageCheckpoint(ctx context.Context, spec SessionSpec) *core.UsageCheckpoint
	// UsageIdentity is the native session id to store, or nil when ambiguous.
	UsageIdentity(ctx context.Context, spec SessionSpec) *string
	// Interrupt cancels the current turn, as Escape does in Pi, and leaves the conversation resumable.
	Interrupt(ctx context.Context, spec SessionSpec) SessionInterrupt
}

// TmuxRunner runs each session in its own tmux session on a private tmux server
// (`tmux -L beltline`, or BELTLINE_TMUX_SOCKET when that is set), so your own
// tmux sessions and these never meet.
type TmuxRunner struct {
	shell shell.Runner
	// socket is the tmux server name this runner uses. NewTmuxRunner sets it to
	// TmuxSocket(); tests give each one its own.
	socket string
}

var _ AgentRunner = TmuxRunner{}

const tmuxSocketEnv = "BELTLINE_TMUX_SOCKET"

var (
	tmuxSocketOnce  sync.Once
	tmuxSocketValue string
)

// runnerUUIDPattern is what UUID(uuidString:) accepts: 8-4-4-4-12 hex digits.
var runnerUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// TmuxSocket is the server name, read once. Tests set BELTLINE_TMUX_SOCKET; the app leaves it unset and uses `beltline`.
func TmuxSocket() string {
	tmuxSocketOnce.Do(func() {
		env := map[string]string{}
		if value, ok := os.LookupEnv(tmuxSocketEnv); ok {
			env[tmuxSocketEnv] = value
		}
		tmuxSocketValue = tmuxSocketName(env)
	})
	return tmuxSocketValue
}

// tmuxSocketName is BELTLINE_TMUX_SOCKET when set, else `beltline`.
func tmuxSocketName(environment map[string]string) string {
	if value, ok := environment[tmuxSocketEnv]; ok {
		return value
	}
	return "beltline"
}

// NewTmuxRunner is TmuxRunner(shell:). A nil shell uses shell.ProcessRunner.
func NewTmuxRunner(sh shell.Runner) TmuxRunner {
	return NewTmuxRunnerOnSocket(sh, TmuxSocket())
}

// NewTmuxRunnerOnSocket is NewTmuxRunner on the tmux server named socket instead of the process's
// (tests give each one its own server, so they can run in parallel). A nil shell uses shell.ProcessRunner.
func NewTmuxRunnerOnSocket(sh shell.Runner, socket string) TmuxRunner {
	if sh == nil {
		sh = shell.ProcessRunner{}
	}
	return TmuxRunner{shell: sh, socket: socket}
}

// tmux runs tmux on the private server with arguments.
func (r TmuxRunner) tmux(ctx context.Context, arguments []string) shell.Result {
	return r.shell.Run(ctx, "tmux", append([]string{"-L", r.socket}, arguments...), "", nil)
}

// TmuxSessionName is a tmux-safe session name: `bl-` and the parts, with
// characters other than letters, digits, `-` and `_` replaced by `-`.
func TmuxSessionName(parts []string) string {
	raw := strings.Join(append([]string{"bl"}, parts...), "-")
	// Per character: a character is kept when it is `-` or `_`, or
	// its first scalar is a letter or a number.
	var b strings.Builder
	for _, ch := range core.Characters(raw) {
		first, _ := utf8.DecodeRuneInString(ch)
		if ch == "-" || ch == "_" || unicode.IsLetter(first) || unicode.IsNumber(first) {
			b.WriteString(ch)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// Stop kills the tmux session, best-effort. A pane that has already finished is
// not an error. EnsureStopped reports failures.
func (r TmuxRunner) Stop(ctx context.Context, session string) {
	_ = r.tmux(ctx, []string{"kill-session", "-t", tmuxExact(session)})
}

// EnsureStopped kills the session. It succeeds when the session is already gone,
// and fails, leaving the session in place, when tmux refuses to stop it.
func (r TmuxRunner) EnsureStopped(ctx context.Context, session string) error {
	killed := r.tmux(ctx, []string{"kill-session", "-t", tmuxExact(session)})
	if killed.OK() || tmuxSessionIsGone(killed) {
		return nil
	}
	present := r.tmux(ctx, []string{"has-session", "-t", tmuxExact(session)})
	if !present.OK() && tmuxSessionIsGone(present) {
		return nil
	}
	return core.NewFailure("tmux: " + killed.Message())
}

// tmuxSessionIsGone is true when tmux's answer says the session, or the server that would hold it, is gone.
func tmuxSessionIsGone(result shell.Result) bool {
	text := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	return strings.Contains(text, "can't find session") ||
		strings.Contains(text, "session not found") ||
		strings.Contains(text, "no server running") ||
		strings.Contains(text, "error connecting")
}

// AttachCommand is the command that attaches to the session, read-only when asked; nil when it is not running.
func (r TmuxRunner) AttachCommand(ctx context.Context, session string, readOnly bool) []string {
	exact := tmuxExact(session)
	if !r.tmux(ctx, []string{"has-session", "-t", exact}).OK() {
		return nil
	}
	command := []string{"tmux", "-L", r.socket, "attach-session"}
	if readOnly {
		command = append(command, "-r")
	}
	return append(command, "-t", exact)
}

// StartConversation starts an interactive conversation in its own tmux session,
// unless one is already alive. Refuses a conversation with no explicit model.
func (r TmuxRunner) StartConversation(ctx context.Context, spec ConversationSpec) error {
	if strings.TrimSpace(spec.Model) == "" {
		return core.NewFailure("Choose an explicit allowed model before launching " + spec.Backend.Label() + ".")
	}
	if r.ConversationAlive(ctx, spec.Name) {
		return nil
	}
	if err := os.MkdirAll(spec.Directory, 0o755); err != nil {
		return err
	}
	sessionID, err := r.PrepareSessionID(ctx, spec.Backend, spec.SessionID, spec.Directory)
	if err != nil {
		return err
	}
	args := trusting(spec.Backend, AdapterFor(spec.Backend).ConversationArguments(sessionID, spec.Name, spec.Model, spec.Thinking), spec.Directory)
	if err := os.MkdirAll(filepath.Dir(spec.PromptFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(spec.PromptFile, []byte(spec.Prompt), 0o644); err != nil {
		return err
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "$PROMPT" {
			quoted = append(quoted, `"$(cat `+tmuxQuote(spec.PromptFile)+`)"`)
		} else {
			quoted = append(quoted, tmuxQuote(arg))
		}
	}
	command := "exec " + tmuxQuote(spec.Backend.Executable(os.Getenv)) + " " + strings.Join(quoted, " ")
	result := r.tmux(ctx, []string{
		"new-session", "-d", "-s", spec.Name, "-x", "200", "-y", "50", "-c", spec.Directory,
		"-e", "PATH=" + shell.Path(), command,
	})
	if !result.OK() {
		return core.NewFailure("tmux: " + result.Message())
	}
	_ = r.tmux(ctx, []string{"set-option", "-t", tmuxExactPane(spec.Name), "remain-on-exit", "off"})
	return nil
}

// ConversationAlive reports whether an attach command exists for the session.
func (r TmuxRunner) ConversationAlive(ctx context.Context, session string) bool {
	return r.AttachCommand(ctx, session, false) != nil
}

// TmuxContinueName is the tmux session ContinueSession opens for session.
func TmuxContinueName(session string) string { return session + tmuxContinueSuffix }

// TmuxContinuedSession is the session a Continue session named name was opened for, as
// TmuxContinueName names it, and whether name is one.
func TmuxContinuedSession(name string) (string, bool) {
	return strings.CutSuffix(name, tmuxContinueSuffix)
}

// tmuxContinueSuffix ends the name of every Continue session.
const tmuxContinueSuffix = "-continue"

// tmuxExact is an exact session target: without the `=`, tmux matches by prefix.
func tmuxExact(session string) string { return "=" + session }

// tmuxExactPane is the current pane of exactly that session, for commands that take a window or pane.
func tmuxExactPane(session string) string { return "=" + session + ":" }

// checkpointLines is the transcript lines a usage checkpoint already counted, or zero.
func checkpointLines(checkpoint *core.UsageCheckpoint) int {
	if checkpoint == nil || checkpoint.Lines == nil {
		return 0
	}
	return *checkpoint.Lines
}
