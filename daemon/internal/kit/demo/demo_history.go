// SeedHistory, the shipped runs over
// the last twelve weeks for the Overview's charts, and seedLongMarkdown.

package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// historyTitles are the titles of the shipped runs, picked by number.
var historyTitles = []string{
	"Add a billing contact field", "Fix the date picker on Safari", "Cache exchange rates", "Show the plan on the invoice",
	"Retry failed exports", "Sort customers by last invoice", "Warn before deleting a quote", "Faster invoice search",
	"Keep filters after reload", "Link receipts from emails", "Round totals the same way everywhere", "Log slow queries",
	"Show who approved a quote", "Remember the last export format", "Trim spaces in customer names", "Explain failed payments",
}

// seedHistory writes the runs shipped over the last twelve weeks, for the
// Overview's charts.
// Runs from the start of the shadow test alternate between live and the shadow;
// live's go back from Verify to Build every third run, and take longer.
func seedHistory(storage store.Storage, belts map[string]core.Belt, shadow *demoShadow, now time.Time) error {
	perWeek := []int{1, 1, 2, 2, 3, 2, 3, 4, 4, 5, 6, 7}
	number := 60
	belt, ok := belts["issues"]
	if !ok {
		return nil
	}
	repos := []string{"acme/billing", "acme/web", "acme/platform"}
	for week, count := range perWeek {
		for k := 0; k < count; k++ {
			number++
			shipped := now
			if week != 11 || k != 0 {
				// Whole seconds for these offsets.
				back := (11-week)*7*86400 + k*30000 + 7200
				shipped = now.Add(-time.Duration(back) * time.Second)
			}
			repo := repos[number%3]
			created := addSeconds(shipped, -11000)
			definition, arm := belt, core.ShadowArm("")
			if shadow != nil && !created.Before(shadow.test.StartedAt.Time) {
				// Pairs of runs alternate, so each version takes both routes.
				arm = core.ShadowArmLive
				if number%4 >= 2 {
					arm, definition = core.ShadowArmShadow, shadow.belt
				}
			}
			run := core.NewRun(core.RunArgs{
				Belt: definition,
				Subject: core.Subject{
					Kind: core.SubjectKindIssue, Repo: repo, Number: number, Title: historyTitles[number%len(historyTitles)],
					Labels: []string{"agent"}, IsOpen: false,
				},
				Now: addSeconds(shipped, -11000),
			})
			if arm != "" {
				run.Shadow = &core.RunShadow{Test: shadow.test.ID, Arm: arm}
			}
			clock := addSeconds(shipped, -11000)
			core.EngineStart(&run, clock)
			// Every other one took the Simple route.
			start := []string{"ready to implement"}
			if number%2 == 0 {
				start = []string{"ready to spec", "complete", "approved"}
			}
			build := []string{"done", "pass"}
			step := 1400.0
			if arm == core.ShadowArmLive && number%3 == 0 {
				build = []string{"done", "code mismatch", "done", "pass"}
			}
			if arm == core.ShadowArmShadow {
				step = 1100
			}
			outcomes := append(append(append([]string{}, start...), build...), "opened", "merged", "done")
			for _, outcome := range outcomes {
				clock = addSeconds(clock, step)
				if stage := run.Stage(); stage != nil && stage.Kind() == core.StageKindWatch {
					run.SetPullRequest(&core.PullRequest{
						Number: number + 40, URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, number+40), State: core.PullRequestStateMerged,
					})
				}
				if err := complete(&run, outcome, storage, clock); err != nil {
					return err
				}
			}
			if week == 11 && k == 0 && len(run.Attempts) > 0 {
				run.Attempts[len(run.Attempts)-1].EndedAt = &jsonx.Date{Time: now}
				run.UpdatedAt = jsonx.Date{Time: now}
			}
			if err := storage.SaveRun(run); err != nil {
				return err
			}
		}
	}
	return nil
}

// seedLongMarkdown, with BELTLINE_DEMO_LONG_MARKDOWN=1, replaces the default
// issue run's last saved agent output with long Markdown, so a snapshot can
// exercise wrapping.
func seedLongMarkdown(storage store.Storage, env func(string) string) error {
	if env("BELTLINE_DEMO_LONG_MARKDOWN") != "1" {
		return nil
	}
	var target *core.Run
	for _, run := range storage.LoadRuns() {
		if run.Subject.Ref() == "#139" && run.StageID == "approve" {
			run := run
			target = &run
			break
		}
	}
	if target == nil {
		return nil
	}
	var stage *core.Stage
	for i := range target.Stages {
		if target.Stages[i].ID == "spec" {
			stage = &target.Stages[i]
			break
		}
	}
	if stage == nil {
		return nil
	}
	attemptIndex := -1
	for i := range target.Attempts {
		if target.Attempts[i].StageID == stage.ID {
			attemptIndex = i
		}
	}
	if attemptIndex < 0 {
		return nil
	}
	file := filepath.Join(storage.Paths.AttemptFolder(*target, target.Attempts[attemptIndex]), stage.Writes())
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(seedLongMarkdownSample()), 0o644)
}
