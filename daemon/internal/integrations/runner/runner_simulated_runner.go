package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Output text the simulated sessions write, by agent and outcome. The
// text literals are in output(for:outcome:).
const (
	simulatedTriageNeedsInfoBody   = "# Triage\n\nThe issue doesn't say where it happens.\n\n## Open questions\n1. Which page shows the problem?\n2. Does it happen for new items only?"
	simulatedTriageBody            = "# Triage\n\nThe goal is clear and the code involved is small.\n\n## Notes for the spec\nThe change sits in one module and its tests."
	simulatedSpecBody              = "# Spec\n\n## Goal\nThe behaviour the issue asks for.\n\n## Behaviour\n1. The user can do what the issue describes.\n2. Nothing else changes.\n\n## Testing\n1 → a unit test for the new path.\n2 → the existing suite passes."
	simulatedBuildBody             = "# Evidence\n\nChanged the module and added a test.\n\n1: met — unit test\n2: met — full suite\n\n## Friction\nNone"
	simulatedVerifyMismatchBody    = "# Verdict\n\n## Mismatches\n- rule 1: the empty case returns an error — see the test output."
	simulatedVerifyBody            = "# Verdict\n\nEvery rule is met; the tests pass."
	simulatedDefaultOutputBody     = "# Output\n\nFinished."
	simulatedOutcomeFooterTemplate = "\n\nOutcome: %s\n"
)

// Literals of SimulatedAFKDecision: the message and the reason, by choice.
const (
	simulatedAFKAnswerMessage = "1. The export page, under Invoices.\n2. Yes, for every item. Assumed CSV with a header row."
	simulatedAFKRerunMessage  = "One more try: check what stopped the last attempt before anything else."
	simulatedAFKLeaveWhy      = "This needs knowledge only you have."
	simulatedAFKAnswerWhy     = "The issue and the code answer both questions; the assumptions are small and easy to change."
	simulatedAFKRerunWhy      = "Nothing in the run's history says this stop needs a person yet, and one more try is cheap."
	simulatedAFKDefaultWhy    = "The output covers what the issue asks for and the tests describe it, so this is the choice you would make."
)

// SimulatedPick chooses a session's outcome; nil means the stage's first outcome.
type SimulatedPick func(spec SessionSpec) *string

// SimulatedRunner runs sessions that finish on a clock and write an output with
// an outcome, for demo mode and tests. A mutex guards it: every
// method takes mu for its synchronous part, and the test hooks (BeforeStatus,
// BeforeStart, BeforeStop) run without it.
type SimulatedRunner struct {
	mu sync.Mutex

	started map[string]time.Time
	lines   map[string][]string
	// suspended are the sessions a global pause has frozen. Their clock stands
	// still, so Status keeps reporting the same attempt as running.
	suspended      map[string]struct{}
	endedContinues []string
	// trail records start and stop calls, in order.
	trail []string
	// prompts are the prompts each start wrote, in order, for continuation tests.
	prompts []string
	// interruptFailures are the sessions whose interrupt should fail.
	interruptFailures map[string]struct{}
	// failPrompt, when set, makes Start refuse a prompt that contains it.
	failPrompt *string

	// BeforeStatus is awaited at the start of Status, so a test can hold a completion.
	BeforeStatus func(spec SessionSpec)
	// BeforeStart is awaited at the start of Start, so a test can hold session allocation.
	BeforeStart func(spec SessionSpec)
	// BeforeStop is awaited from Stop and EnsureStopped, so a test can hold session shutdown.
	BeforeStop func(session string)
	// BeforePrepare is awaited at the start of PrepareSessionID, so a test can hold a launch while it prepares.
	BeforePrepare func(proposed string)
	// PrepareErr, when set, is what the next PrepareSessionID fails with, once.
	PrepareErr error
	// StopError, when set, is what EnsureStopped fails with, leaving the session running.
	StopError *string

	// duration is the session's length in seconds.
	duration func(spec SessionSpec) float64
	pick     SimulatedPick
	clock    func() time.Time
}

var _ AgentRunner = (*SimulatedRunner)(nil)

// NewSimulatedRunner is SimulatedRunner(duration:clock:pick:). Nil
// arguments use 8 seconds, time.Now and no pick.
func NewSimulatedRunner(duration func(spec SessionSpec) float64, clock func() time.Time, pick SimulatedPick) *SimulatedRunner {
	if duration == nil {
		duration = func(SessionSpec) float64 { return 8 }
	}
	if clock == nil {
		clock = time.Now
	}
	if pick == nil {
		pick = func(SessionSpec) *string { return nil }
	}
	return &SimulatedRunner{
		started:           map[string]time.Time{},
		lines:             map[string][]string{},
		suspended:         map[string]struct{}{},
		interruptFailures: map[string]struct{}{},
		duration:          duration,
		pick:              pick,
		clock:             clock,
	}
}

// FailInterrupts sets the sessions whose interrupt fails.
func (r *SimulatedRunner) FailInterrupts(names map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interruptFailures = core.CopySet(names)
}

// RefusePrompt makes Start refuse a prompt containing text; nil clears it.
func (r *SimulatedRunner) RefusePrompt(text *string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if text == nil {
		r.failPrompt = nil
		return
	}
	value := *text
	r.failPrompt = &value
}

// Trail is the start and stop calls so far, in order.
func (r *SimulatedRunner) Trail() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.trail...)
}

// Prompts is the prompts the starts wrote, in order.
func (r *SimulatedRunner) Prompts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.prompts...)
}

// EndedContinues is the Continue sessions ended so far, in order.
func (r *SimulatedRunner) EndedContinues() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.endedContinues...)
}

// SuspendedSessions is the sessions a global pause has frozen.
func (r *SimulatedRunner) SuspendedSessions() map[string]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return core.CopySet(r.suspended)
}

// PrepareSessionID takes the proposed id: a simulated runner hands out no ids of its own.
func (r *SimulatedRunner) PrepareSessionID(ctx context.Context, backend core.Backend, proposed string, directory string) (string, error) {
	if r.BeforePrepare != nil {
		r.BeforePrepare(proposed)
	}
	if err := r.PrepareErr; err != nil {
		r.PrepareErr = nil
		return "", err
	}
	return proposed, nil
}

// Start writes the brief and a turns log, and begins the clock. It refuses a
// prompt that RefusePrompt named.
func (r *SimulatedRunner) Start(ctx context.Context, spec SessionSpec) error {
	r.mu.Lock()
	if r.failPrompt != nil && strings.Contains(spec.Prompt, *r.failPrompt) {
		r.mu.Unlock()
		return core.NewFailure("launch refused")
	}
	hook := r.BeforeStart
	r.mu.Unlock()
	if hook != nil {
		hook(spec)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trail = append(r.trail, "start "+spec.Name)
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(spec.AttemptFolder, "brief.md"), []byte(spec.Prompt), 0o644); err != nil {
		return err
	}
	r.prompts = append(r.prompts, spec.Prompt)
	turns := filepath.Join(spec.AttemptFolder, "turns.log")
	prior, _ := os.ReadFile(turns)
	separator := "\n---\n"
	if len(prior) == 0 {
		separator = ""
	}
	_ = os.WriteFile(turns, []byte(string(prior)+separator+spec.Prompt), 0o644)
	r.started[spec.Name] = r.clock()
	r.lines[spec.Name] = []string{"$ " + spec.Backend.Executable(os.Getenv) + " …", "Reading the issue and the code."}
	return nil
}

// Status is running until the duration has passed, then finished with exit code
// zero. The first finish writes the output file with the picked outcome.
func (r *SimulatedRunner) Status(ctx context.Context, spec SessionSpec) SessionStatus {
	if r.BeforeStatus != nil {
		r.BeforeStatus(spec)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	start, ok := r.started[spec.Name]
	if !ok {
		return SessionStatus{Kind: SessionStatusGone}
	}
	if _, frozen := r.suspended[spec.Name]; frozen {
		return SessionStatus{Kind: SessionStatusRunning}
	}
	if r.clock().Sub(start).Seconds() < r.duration(spec) {
		return SessionStatus{Kind: SessionStatusRunning}
	}
	if !runnerPathExists(spec.Output) {
		outcome := "done"
		if picked := r.pick(spec); picked != nil {
			outcome = *picked
		} else if len(spec.Outcomes) > 0 {
			outcome = spec.Outcomes[0]
		}
		_ = os.WriteFile(spec.Output, []byte(SimulatedRunnerOutput(spec, outcome)), 0o644)
	}
	zero := int32(0)
	return SessionStatus{Kind: SessionStatusFinished, ExitCode: &zero}
}

// Finish writes the session log and forgets the session's clock.
func (r *SimulatedRunner) Finish(ctx context.Context, spec SessionSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	log := append(append([]string(nil), r.lines[spec.Name]...), "Wrote "+filepath.Base(spec.Output)+".", "Done.")
	url := filepath.Join(spec.AttemptFolder, "session.log")
	piece := strings.Join(log, "\n")
	prior, err := os.ReadFile(url)
	if spec.Prompt == core.RunPauseContinuePrompt && err == nil && len(prior) > 0 {
		_ = os.WriteFile(url, []byte(string(prior)+"\n"+piece), 0o644)
	} else {
		_ = os.WriteFile(url, []byte(piece), 0o644)
	}
	delete(r.started, spec.Name)
}

// Interrupt stops a running session, writing its log and an `interrupted` marker.
// A session that is not running but has the marker is confirmed.
func (r *SimulatedRunner) Interrupt(ctx context.Context, spec SessionSpec) SessionInterrupt {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, failing := r.interruptFailures[spec.Name]; failing {
		return SessionInterrupt{Kind: SessionInterruptFailed, Message: "interrupt failed"}
	}
	if _, running := r.started[spec.Name]; !running {
		if runnerPathExists(filepath.Join(spec.AttemptFolder, "interrupted")) {
			if spec.ResumeToken != nil {
				return SessionInterrupt{Kind: SessionInterruptConfirmed, Conversation: spec.ResumeToken}
			}
			id := spec.SessionID
			return SessionInterrupt{Kind: SessionInterruptConfirmed, Conversation: &id}
		}
		return SessionInterrupt{Kind: SessionInterruptFailed, Message: spec.Name + " is not running"}
	}
	url := filepath.Join(spec.AttemptFolder, "session.log")
	prior, _ := os.ReadFile(url)
	segment := strings.Join(r.lines[spec.Name], "\n")
	_ = os.WriteFile(url, []byte(string(prior)+segment+"\n-- interrupted --\n"), 0o644)
	_ = os.WriteFile(filepath.Join(spec.AttemptFolder, "interrupted"), []byte("interrupted\n"), 0o644)
	delete(r.started, spec.Name)
	delete(r.suspended, spec.Name)
	if spec.Backend == core.BackendCodex {
		return SessionInterrupt{Kind: SessionInterruptConfirmed, Conversation: spec.ResumeToken}
	}
	id := spec.SessionID
	return SessionInterrupt{Kind: SessionInterruptConfirmed, Conversation: &id}
}

// StartConversation writes the conversation's prompt file and begins its clock.
func (r *SimulatedRunner) StartConversation(ctx context.Context, spec ConversationSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(spec.PromptFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(spec.PromptFile, []byte(spec.Prompt), 0o644); err != nil {
		return err
	}
	r.started[spec.Name] = r.clock()
	return nil
}

// ConversationAlive reports whether the session has a clock running.
func (r *SimulatedRunner) ConversationAlive(ctx context.Context, session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.started[session]
	return ok
}

// LiveSessionNames is every session with a clock running, as ConversationAlive sees them.
func (r *SimulatedRunner) LiveSessionNames(ctx context.Context) map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make(map[string]bool, len(r.started))
	for name := range r.started {
		names[name] = true
	}
	return names
}

// EndContinuing writes the Continue log, records the session ended, and stops it.
func (r *SimulatedRunner) EndContinuing(ctx context.Context, spec SessionSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := TmuxContinueName(spec.Name)
	if _, ok := r.started[name]; !ok {
		return
	}
	log := append(append([]string(nil), r.lines[name]...), "Continue session ended.")
	_ = os.WriteFile(filepath.Join(spec.AttemptFolder, "continue.log"), []byte(strings.Join(log, "\n")), 0o644)
	r.endedContinues = append(r.endedContinues, name)
	r.stopLocked(name)
}

// Stop forgets the session, and records the stop in the trail.
func (r *SimulatedRunner) Stop(ctx context.Context, session string) {
	if r.BeforeStop != nil {
		r.BeforeStop(session)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked(session)
}

// stopLocked is Stop for a caller that holds mu.
func (r *SimulatedRunner) stopLocked(session string) {
	r.trail = append(r.trail, "stop "+session)
	delete(r.started, session)
	delete(r.suspended, session)
}

// Suspend freezes a session's clock, the way stopping its process would.
func (r *SimulatedRunner) Suspend(ctx context.Context, session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.started[session]; !ok {
		return
	}
	r.suspended[session] = struct{}{}
}

// Resume wakes a frozen session.
func (r *SimulatedRunner) Resume(ctx context.Context, session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.suspended, session)
}

// IsSuspended reports whether a global pause has frozen the session.
func (r *SimulatedRunner) IsSuspended(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.suspended[session]
	return ok
}

// EnsureStopped runs BeforeStop, fails with StopError when it is set, and stops the session.
func (r *SimulatedRunner) EnsureStopped(ctx context.Context, session string) error {
	if r.BeforeStop != nil {
		r.BeforeStop(session)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.StopError != nil {
		return core.NewFailure(*r.StopError)
	}
	r.stopLocked(session)
	return nil
}

// AttachCommand is nil: a simulated session has no terminal to attach.
func (r *SimulatedRunner) AttachCommand(ctx context.Context, session string, readOnly bool) []string {
	return nil
}

// IsRunning reports whether a session is still going, for the session sidebar in demo mode.
func (r *SimulatedRunner) IsRunning(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.started[session]
	return ok
}

// Usage is nil: a simulated session reports no usage (the protocol default).
func (r *SimulatedRunner) Usage(ctx context.Context, spec SessionSpec) *core.Usage {
	return nil
}

// UsageMark is zero (the protocol default).
func (r *SimulatedRunner) UsageMark(ctx context.Context, spec SessionSpec) int { return 0 }

// UsageCheckpoint is nil (the protocol default).
func (r *SimulatedRunner) UsageCheckpoint(ctx context.Context, spec SessionSpec) *core.UsageCheckpoint {
	return nil
}

// UsageIdentity is nil (the protocol default).
func (r *SimulatedRunner) UsageIdentity(ctx context.Context, spec SessionSpec) *string {
	return nil
}

// Reply is nil (the protocol default): a simulated session has no log to read.
func (r *SimulatedRunner) Reply(ctx context.Context, spec SessionSpec) *string { return nil }

// SimulatedRunnerOutput is the file a simulated session writes for an outcome:
// the agent's body, then the outcome footer. The AFK agent gets its decision text.
func SimulatedRunnerOutput(spec SessionSpec, outcome string) string {
	agentID := core.ValueOf(spec.AgentID)
	if agentID == "afk" {
		return SimulatedAFKDecision(outcome)
	}
	var body string
	switch {
	case agentID == "triage" && outcome == "needs info":
		body = simulatedTriageNeedsInfoBody
	case agentID == "triage":
		body = simulatedTriageBody
	case agentID == "spec":
		body = simulatedSpecBody
	case agentID == "build":
		body = simulatedBuildBody
	case agentID == "verify" && outcome == "code mismatch":
		body = simulatedVerifyMismatchBody
	case agentID == "verify":
		body = simulatedVerifyBody
	default:
		body = simulatedDefaultOutputBody
	}
	return body + "\n\nOutcome: " + outcome + "\n"
}

// SimulatedAFKDecision is a decision by the AFK assistant in demo mode: its
// message, its reason and the `Decision:` line.
func SimulatedAFKDecision(choice string) string {
	var message string
	switch choice {
	case "answer":
		message = simulatedAFKAnswerMessage
	case "rerun":
		message = simulatedAFKRerunMessage
	default:
		message = ""
	}
	var why string
	switch choice {
	case "leave":
		why = simulatedAFKLeaveWhy
	case "answer":
		why = simulatedAFKAnswerWhy
	case "rerun":
		why = simulatedAFKRerunWhy
	default:
		why = simulatedAFKDefaultWhy
	}
	return "## Message\n\n" + message + "\n\n## Why\n\n" + why + "\n\nDecision: " + choice + "\n"
}

// PretendRunning records a session this runner did not start, still reported as running from at on
// . Tests use it to model a session another process started.
func (r *SimulatedRunner) PretendRunning(session string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started[session] = at
}

// RunningCount is how many sessions the runner tracks as started.
func (r *SimulatedRunner) RunningCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started)
}
