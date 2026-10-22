// The POST desktop
// commands that reveal files on this Mac, and the registration of every desktop
// command (the open and editor commands are in routes_desktop_open.go).
package routes

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// DesktopApplicationLookup reports whether a desktop application is installed. It
// is read on the coordinator's turn.
type DesktopApplicationLookup func(name string) bool

// DesktopSessionLookup reports whether a tmux session is available to attach or
// open.
type DesktopSessionLookup func(ctx context.Context, name string) bool

// desktopBodyLimit is the read limit of the desktop commands' bodies (8 KiB).
const desktopBodyLimit = 8_192

// AddDesktop is the add desktop:
// POST api/v1/desktop/reveal, reveal-data, vscode, ghostty, open-file and
// open-url. Each runs one command through runner after the coordinator has
// checked the request.
// Lock: the handlers call coordinator reads and checks, each of which takes the
// lock for its own turn; the launch runs without it.
func AddDesktop(mux *http.ServeMux, c *coordinator.Coordinator, shellRunner shell.Runner, platform launch.Platform,
	applicationLookup DesktopApplicationLookup, sessionLookup DesktopSessionLookup) {
	desktop := desktopLauncher{runner: shellRunner, platform: platform}
	mux.Handle("POST /api/v1/desktop/reveal-data", ErrorRoute(desktopRevealDataRoute(c, desktop)))
	mux.Handle("POST /api/v1/desktop/reveal", ErrorRoute(desktopRevealRoute(c, desktop)))
	mux.Handle("POST /api/v1/desktop/open-file", ErrorRoute(desktopOpenFileRoute(c, desktop)))
	mux.Handle("POST /api/v1/desktop/vscode", ErrorRoute(desktopVSCodeRoute(c, desktop, applicationLookup)))
	mux.Handle("POST /api/v1/desktop/ghostty", ErrorRoute(desktopGhosttyRoute(c, desktop, applicationLookup, sessionLookup)))
	mux.Handle("POST /api/v1/desktop/open-url", ErrorRoute(desktopOpenURLRoute(c, desktop)))
}

// desktopRevealDataRoute serves POST api/v1/desktop/reveal-data: it reveals the
// data folder in Finder.
func desktopRevealDataRoute(c *coordinator.Coordinator, desktop desktopLauncher) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		if err := DecodeEmptyObjectOrNoBody(r, DefaultBodyLimit); err != nil {
			return err
		}
		paths := c.Paths()
		return desktop.launch(w, r, desktop.platform.Reveal(filepath.Clean(paths.Root), true))
	})
}

// desktopRevealRoute serves POST api/v1/desktop/reveal: it reveals a run's,
// attempt's, worktree's, AFK or agent's folder, or an artifact, in Finder.
func desktopRevealRoute(c *coordinator.Coordinator, desktop desktopLauncher) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		first, err := desktopBody(r, []string{"kind"}, []string{"runId", "attemptId", "name", "targetId", "agentId"})
		if err != nil {
			return err
		}
		kind, ok := first["kind"]
		if !ok {
			return core.NewFailure("Expected runId and kind.")
		}
		var target string
		var directory bool
		if kind == "agent" {
			// An agent's folder belongs to the library, not to a run.
			if !desktopKeysAre(first, "kind", "agentId") {
				return core.NewFailure("The agent selector requires agentId.")
			}
			agentID, ok := first["agentId"]
			if !ok {
				return core.NewFailure("The agent selector requires agentId.")
			}
			if c.ReadAgent(agentID) == nil {
				return api.MissingResource{}
			}
			target = filepath.Join(c.Paths().Agents(), agentID)
			directory = true
		} else {
			runID, hasRunID := first["runId"]
			var run *core.Run
			if hasRunID {
				run = c.ReadRun(runID)
			}
			if run == nil {
				if !hasRunID {
					return core.NewFailure("Expected runId and kind.")
				}
				return api.MissingResource{}
			}
			target, err = desktopRevealTarget(kind, first, *run, c)
			if err != nil {
				return err
			}
			directory = kind == "run" || kind == "attempt" || kind == "worktree" || kind == "afk"
		}
		paths := c.Paths()
		resolved, ok := store.ArtifactsConfinedURL(target, paths.Root, &directory)
		if !ok {
			return api.MissingResource{}
		}
		return desktop.launch(w, r, desktop.platform.Reveal(resolved, directory))
	})
}
