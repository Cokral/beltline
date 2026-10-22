// Complete, pauseSeed, coverSpend and
// afkDecision: how a seeded run is taken through its stages and what each one
// leaves behind.

package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// complete finishes the run's current stage with outcome, writing an agent's
// output.
func complete(run *core.Run, outcome string, storage store.Storage, now time.Time) error {
	stage := run.Stage()
	if stage == nil {
		return nil
	}
	switch stage.Kind() {
	case core.StageKindAgent:
		if stage.Workspace == core.WorkspaceWorktree {
			branch := core.RunBranch(*run)
			run.Branch = &branch
		}
		folder := core.EngineAttemptFolder(*run, *stage)
		agentID := ""
		if stage.Agent != nil {
			agentID = stage.Agent.ID
		}
		core.EngineAdvance(run, core.Event{
			Kind: core.EventKindSessionStarted, Session: "bl-demo-" + run.Subject.SessionPart() + "-" + stage.ID,
			SessionID: core.NewUUID(), Folder: folder,
		}, addSeconds(now, -120))
		attemptFolder := filepath.Join(storage.Paths.RunFolder(*run), folder)
		if err := os.MkdirAll(attemptFolder, 0o755); err != nil {
			return err
		}
		text := runner.SimulatedRunnerOutput(runner.SessionSpec{AgentID: &agentID}, outcome)
		if err := os.WriteFile(filepath.Join(attemptFolder, stage.Writes()), []byte(text), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(attemptFolder, "session.log"), []byte("Seeded by demo mode.\n"), 0o644); err != nil {
			return err
		}
		core.EngineAdvance(run, core.Event{Kind: core.EventKindSessionFinished, Output: &text}, now)
		if last := len(run.Attempts) - 1; last >= 0 {
			usage := core.NewUsage(core.UsageArgs{
				CostNanos: new(110_000_000), Input: new(401), Output: new(93), CacheRead: new(50560), TotalTokens: new(51054),
			})
			run.Attempts[last].Usage = &usage
		}
	case core.StageKindGate:
		core.EngineAdvance(run, core.Event{Kind: core.EventKindDecided, Outcome: outcome}, now)
	case core.StageKindWatch:
		event := core.WatchEventClosed
		if o := stage.Outcome(outcome); o != nil && isWatchEvent(o.When) {
			event = core.WatchEvent(o.When)
		}
		var detail *string
		if pull := run.PullRequest(); pull != nil {
			text := fmt.Sprintf("#%d", pull.Number)
			detail = &text
		}
		core.EngineAdvance(run, core.Event{Kind: core.EventKindWatched, Watch: event, Detail: detail}, now)
	}
	return nil
}

// isWatchEvent reports whether raw is one of the watch events.
func isWatchEvent(raw string) bool {
	return slices.ContainsFunc(core.AllWatchEvent, func(event core.WatchEvent) bool {
		return string(event) == raw
	})
}

// pauseSeed leaves a run paused mid-triage, with its log still readable. An
// incomplete pause names the session that did not confirm.
func pauseSeed(run *core.Run, storage store.Storage, now time.Time, incomplete bool) error {
	stage := run.Stage()
	if stage == nil {
		return nil
	}
	folder := core.EngineAttemptFolder(*run, *stage)
	session := "bl-demo-" + run.Subject.SessionPart() + "-paused"
	conversation := "paused-" + run.Subject.SessionPart()
	core.EngineAdvance(run, core.Event{Kind: core.EventKindSessionStarted, Session: session, SessionID: conversation, Folder: folder}, addSeconds(now, -40))
	attemptFolder := filepath.Join(storage.Paths.RunFolder(*run), folder)
	if err := os.MkdirAll(attemptFolder, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(attemptFolder, "session.log"), []byte("Reading the issue.\n-- interrupted --\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(attemptFolder, stage.Writes()), []byte("Partial notes, still open.\n"), 0o644); err != nil {
		return err
	}
	core.EngineAdvance(run, core.Event{Kind: core.EventKindPause, Operation: "demo-pause"}, addSeconds(now, -20))
	leg := "main"
	if incomplete {
		core.EngineAdvance(run, core.Event{Kind: core.EventKindPauseFailed, Operation: "demo-pause", Leg: &leg, Reason: "did not stop"}, addSeconds(now, -10))
	} else {
		core.EngineAdvance(run, core.Event{Kind: core.EventKindPauseConfirmed, Operation: "demo-pause", Leg: &leg, Conversation: &conversation}, addSeconds(now, -10))
	}
	return nil
}

// coverSpend gives #139 the three coverage states: Pi reported, Claude estimated,
// Cursor untracked.
func coverSpend(run *core.Run) {
	if len(run.Attempts) < 2 {
		return
	}
	reported := core.NewUsage(core.UsageArgs{
		CostNanos: new(340_000_000), Input: new(401), Output: new(93), CacheRead: new(50_560), CacheWrite: new(0),
		Reasoning: new(15), TotalTokens: new(51_054),
	})
	run.Attempts[0].Usage = &reported
	estimated := core.CostProvenanceEstimated
	coverage := core.UsageCoverageComplete
	claude := core.NewUsage(core.UsageArgs{
		CostNanos: new(120_000_000), Input: new(1_000), Output: new(200), CacheRead: new(4_000), TotalTokens: new(5_200),
		Provenance: &estimated, Coverage: &coverage,
		Rate: &core.RateProvenance{Revision: claudeRateRevision, Source: claudeRateSource, Models: []string{"claude-sonnet-4-6"}},
	})
	run.Attempts[1].Usage = &claude
	started := run.Attempts[1].StartedAt.Time
	if run.Attempts[1].EndedAt != nil {
		started = run.Attempts[1].EndedAt.Time
	}
	cursor := core.NewAttempt(core.AttemptArgs{StageID: "build", Number: 1, StartedAt: jsonx.Date{Time: started}, Folder: "build-untracked"})
	cursor.EndedAt = &jsonx.Date{Time: started.Add(90 * time.Second)}
	cursor.Session = new("bl-demo-139-cursor")
	cursor.SessionID = new("cursor-session")
	cursor.Outcome = new("done")
	good := core.ToneGood
	cursor.Tone = &good
	runner := core.BackendCursor
	cursor.Settings = &core.SessionSettings{Runner: &runner, Model: "cursor"}
	run.Attempts = append(run.Attempts, cursor)
}

// afkDecision is what the AFK assistant decided overnight, for the AFK page: it
// approved #142's spec, and left #151's questions for you.
func afkDecision(run *core.Run, now time.Time) (core.AfkDecision, bool) {
	switch run.Subject.Ref() {
	case "#142":
		index := -1
		for i, attempt := range run.Attempts {
			if attempt.StageID == "approve" {
				index = i
				break
			}
		}
		if index < 0 || run.Attempts[index].EndedAt == nil {
			return core.AfkDecision{}, false
		}
		ended := run.Attempts[index].EndedAt.Time
		waiting := *run
		waiting.StageID = "approve"
		waiting.State = core.RunStateWaiting
		situation := core.AfkSituation{Kind: core.AfkSituationKindApproval, Key: fmt.Sprintf("%s|approve|waiting|%d", run.ID, index+1)}
		decision := core.NewAfkDecision(core.AfkDecisionArgs{
			ID: "demo-afk-142", Run: waiting, Situation: situation, StartedAt: jsonx.Date{Time: ended.Add(-90 * time.Second)},
		})
		decision.Status = core.AfkDecisionStatusApplied
		decision.DecidedAt = &jsonx.Date{Time: ended}
		decision.Choice = new("decide approved")
		decision.Action = new("Chose “approved” at Approve")
		decision.Why = new("The spec covers the export the issue asks for, every rule has a test, and it changes one module. Nothing in it needs knowledge only you have.")
		run.Attempts[index].AfkDecision = new(decision.ID)
		return decision, true
	case "#151":
		settings := core.DefaultAfkSettings()
		settings.Enabled = true
		situation := core.AfkSituationOf(*run, settings)
		if situation == nil {
			return core.AfkDecision{}, false
		}
		decision := core.NewAfkDecision(core.AfkDecisionArgs{
			ID: "demo-afk-151", Run: *run, Situation: *situation, StartedAt: jsonx.Date{Time: addSeconds(now, -1500)},
		})
		decision.Status = core.AfkDecisionStatusLeftForYou
		decision.DecidedAt = &jsonx.Date{Time: addSeconds(now, -1400)}
		decision.Choice = new("leave")
		decision.Action = new("Left it for you")
		decision.Why = new("Triage asks which page shows the problem. Neither the issue nor the code says, and guessing would send Spec the wrong way.")
		return decision, true
	default:
		return core.AfkDecision{}, false
	}
}
