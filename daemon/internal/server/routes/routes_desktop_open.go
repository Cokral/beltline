// The POST desktop
// commands that open a run file, the worktree in VS Code, the attempt in Ghostty
// and a link target in the browser.
package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// desktopOpenFileRoute serves POST api/v1/desktop/open-file: it opens an
// attempt's output file.
func desktopOpenFileRoute(c *coordinator.Coordinator, desktop desktopLauncher) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		run, attemptID, err := desktopRunCommand(c, r, "attemptId")
		if err != nil {
			return err
		}
		target, err := desktopOutputTarget(attemptID, run, c)
		if err != nil {
			return err
		}
		paths := c.Paths()
		directory := false
		resolved, ok := store.ArtifactsConfinedURL(target, paths.Root, &directory)
		if !ok {
			return api.MissingResource{}
		}
		return desktop.launch(w, r, desktop.platform.Open(resolved))
	})
}

// desktopVSCodeRoute serves POST api/v1/desktop/vscode: it opens a run's
// worktree in VS Code.
func desktopVSCodeRoute(c *coordinator.Coordinator, desktop desktopLauncher, applicationLookup DesktopApplicationLookup) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		run, targetID, err := desktopRunCommand(c, r, "targetId")
		if err != nil {
			return err
		}
		paths := c.Paths()
		targets := runner.WorktreeTargetsFor(run, paths.WorkFolder(run))
		target, found := desktopWorktreeTarget(targets, targetID)
		if !found {
			return core.NewFailure(launch.DesktopActionsVSCodeUnavailable)
		}
		folder := target.Folder
		rechecked := runner.WorktreeTargetsFor(run, paths.WorkFolder(run))
		recheckedTarget, rechecksFound := desktopWorktreeTarget(rechecked, targetID)
		directory := true
		var safeFolder string
		safeOK := false
		if rechecksFound {
			safeFolder, safeOK = store.ArtifactsConfinedURL(recheckedTarget.Folder, paths.Root, &directory)
		}
		installed := applicationLookup(launch.DesktopActionsVSCodeBundleID)
		checkoutExists := safeOK && rechecksFound && folder == recheckedTarget.Folder
		if reason := launch.DesktopActionsVSCodeError(checkoutExists, installed); reason != nil {
			return core.NewFailure(*reason)
		}
		command := desktop.platform.VSCode(safeFolder)
		result := desktop.runner.Run(r.Context(), command.Executable, command.Arguments, "", nil)
		if !result.OK() {
			message := result.Message()
			return core.NewFailure(launch.DesktopActionsVSCodeLaunchError(&message))
		}
		JSONResponse(w, DesktopSuccess{OK: true}, http.StatusOK)
		return nil
	})
}

// desktopGhosttyRoute serves POST api/v1/desktop/ghostty: it attaches Ghostty
// to an attempt's session.
func desktopGhosttyRoute(c *coordinator.Coordinator, desktop desktopLauncher, applicationLookup DesktopApplicationLookup, sessionLookup DesktopSessionLookup) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		run, attemptID, err := desktopRunCommand(c, r, "attemptId")
		if err != nil {
			return err
		}
		attempt, found := desktopAttemptOf(run, attemptID)
		if !found {
			return api.MissingResource{}
		}
		// The same refusal the sidebar's Ghostty offer shows for a paused run.
		if run.Pause != nil && run.Pause.BlocksProgress() {
			return core.NewFailure(core.DesktopActionsPausedReason)
		}
		if attempt.Session == nil || *attempt.Session == "" {
			return core.NewFailure("This attempt has no available session.")
		}
		attemptSession := *attempt.Session
		continueSession := runner.TmuxContinueName(attemptSession)
		continueAvailable := false
		if !attempt.IsOpen() {
			continueAvailable = desktopSessionAvailable(r.Context(), continueSession, c, sessionLookup)
		}
		selected := launch.DesktopActionsGhosttySession(&attemptSession, continueAvailable, attempt.IsOpen())
		if selected == nil || !desktopSessionAvailable(r.Context(), *selected, c, sessionLookup) {
			return core.NewFailure("This attempt has no available session.")
		}
		if !applicationLookup(launch.DesktopActionsGhosttyBundleID) {
			return core.NewFailure("Ghostty is not installed. Install it and try again.")
		}
		command := launch.DesktopActionsGhosttyCommand(runner.TmuxSocket(), *selected)
		return desktop.launch(w, r, desktop.platform.Ghostty(command))
	})
}

// desktopOpenURLRoute serves POST api/v1/desktop/open-url: it opens a run's
// link target in the browser.
func desktopOpenURLRoute(c *coordinator.Coordinator, desktop desktopLauncher) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		run, targetID, err := desktopRunCommand(c, r, "targetId")
		if err != nil {
			return err
		}
		targets := core.RunLinksTargets(run.Subject, run.PullRequests, c.ReadRepoLabelAmbiguousNames())
		var target *core.RunLinksTarget
		for i := range targets {
			if targets[i].ID == targetID {
				target = &targets[i]
				break
			}
		}
		if target == nil {
			return core.NewFailure("Unknown link target.")
		}
		if !launch.DesktopActionsValidWebURL(target.URL) {
			return core.NewFailure("Only absolute HTTP and HTTPS links can be opened.")
		}
		return desktop.launch(w, r, desktop.platform.Open(target.URL))
	})
}
