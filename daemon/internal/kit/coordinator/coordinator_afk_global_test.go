// Coordinator tests of the global AFK switch: changing it keeps every run's own mode and moves
// only the runs that follow it, and a failed save keeps the saved run mode.

package coordinator

import (
	"os"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// coordAllows reports whether the assistant may decide for run, through the lock.
func coordAllows(c *Coordinator, run core.Run) bool {
	c.Lock()
	defer c.EndTurn()
	return c.autopilot.allows(run)
}

// TestChangingGlobalAfkKeepsEveryRunsModeAndMovesOnlyTheOnesThatFollowIt
func TestChangingGlobalAfkKeepsEveryRunsModeAndMovesOnlyTheOnesThatFollowIt(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "true", false: "false"}[enabled], func(t *testing.T) {
			t.Parallel()
			var belts []core.Belt
			for _, mode := range core.AllAfkMode {
				belts = append(belts, afkBelt("belt-"+string(mode), true))
			}
			h := twoBeltWorld(t, belts, harnessOptions{})
			settings := core.DefaultAfkSettings()
			settings.Enabled = !enabled
			settings.AnswerQuestions = false
			settings.RerunFailures = false
			settings.Approve = false
			settings.ResolveHolds = false
			setAfk(h, func(afk *core.AfkSettings) { *afk = settings })
			for _, mode := range core.AllAfkMode {
				run := runOn(h, "belt-"+string(mode))
				if run == nil {
					t.Fatalf("no run on belt-%s", mode)
				}
				h.coord.SetRunAfk(bgCtx, run.ID, mode)
			}
			h.coord.SetAfk(bgCtx, enabled)
			restarted := h.RestartWith(durationRunner(h.Script, &coordDurations{}))
			if globalAfk(restarted) != enabled {
				t.Fatalf("restarted global AFK = %v, want %v", !enabled, enabled)
			}
			for _, mode := range core.AllAfkMode {
				run := runOn(h, "belt-"+string(mode))
				current := h.coord.Run(run.ID)
				restored := restarted.Run(run.ID)
				if current == nil || restored == nil {
					t.Fatalf("run %s gone after the restart", run.ID)
				}
				if current.Afk() != mode || restored.Afk() != mode {
					t.Fatalf("mode of %s = %s / %s, want %s", run.ID, current.Afk(), restored.Afk(), mode)
				}
				want := mode == core.AfkModeAlways || (mode == core.AfkModeGlobal && enabled)
				if got := coordAllows(h.coord, *run); got != want {
					t.Fatalf("allows(%s) = %v, want %v", mode, got, want)
				}
				if got := coordAllows(restarted, *restored); got != want {
					t.Fatalf("restarted allows(%s) = %v, want %v", mode, got, want)
				}
			}
		})
	}
}

// TestAFailedSaveKeepsTheSavedRunMode
func TestAFailedSaveKeepsTheSavedRunMode(t *testing.T) {
	t.Parallel()
	h := twoBeltWorld(t, []core.Belt{afkBelt("on", true)}, harnessOptions{})
	run := runOn(h, "on")
	if run == nil {
		t.Fatal("no run on the belt")
	}
	folder := h.coord.Paths().RunFolder(*run)
	if err := os.Chmod(folder, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(folder, 0o755) }()

	h.coord.SetRunAfk(bgCtx, run.ID, core.AfkModeAlways)
	if got := h.coord.Run(run.ID); got == nil || got.Afk() != core.AfkModeGlobal {
		t.Fatalf("run mode after a failed save = %+v, want global", got)
	}
	for _, saved := range h.storage.LoadRuns() {
		if saved.ID == run.ID && saved.AfkEnabled != nil {
			t.Fatalf("saved choice = %v, want none", *saved.AfkEnabled)
		}
	}
	if !hasNoticeContaining(h.coord.Notices(), "Could not save", run.Subject.Ref()) {
		t.Fatalf("notices = %+v, want a save failure for %s", h.coord.Notices(), run.Subject.Ref())
	}
}
