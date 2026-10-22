// The init's
// parameters, the clock and the UUIDs its parts make.

package coordinator

import (
	"context"
	"fmt"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/jira"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/library"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/tools"
	"os"
	"time"
)

// Clock is the coordinator's time.
// Tests pass testkit.Clock; the daemon passes RealClock.
type Clock interface {
	Now() time.Time
}

// RealClock reads the system clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// IDs makes the identities: UUIDs, and their lower-case text where a decision needs one:
// the notice ids, the session ids a launch proposes, the operation ids of
// pause and overrule, and the request ids of model discovery. The real one
// draws random v4 UUIDs; tests use SequenceIDs.
type IDs interface {
	// UUID returns an upper-case v4 UUID.
	// Callers that need lower case lowercase it themselves.
	UUID() string
}

// RealIDs draws random UUIDs from crypto/rand.
type RealIDs struct{}

// UUID returns a new random upper-case v4 UUID.
func (RealIDs) UUID() string { return core.NewUUID() }

// SequenceIDs is the deterministic IDs source for tests: the n-th call returns
// the UUID 00000000-0000-4000-8000-<n as twelve digits>, so runs and sessions
// get stable ids. Callers run under c.mu, so it needs no lock of its own.
type SequenceIDs struct {
	// Next is the number of UUIDs handed out so far.
	Next int
}

// UUID returns the next deterministic UUID and advances Next.
func (s *SequenceIDs) UUID() string {
	s.Next++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.Next)
}

// Notifier delivers a notification's title and body.
// The app shows it; a headless daemon
// writes it to its log and to macOS Notification Center.
type Notifier func(title, body string)

// ToolCheckFunc runs the tools check. LaunchSetup supplies
// it: the live one runs the real checks, demo mode returns the fixtures.
type ToolCheckFunc func(ctx context.Context) []tools.Result

// Services is every dependency a coordinator is given or constructs. Nil
// fields take their default, which LaunchSetup fills in for the
// rest. Tests pass the fakes, runner.SimulatedRunner, a testkit clock and a
// SequenceIDs.
type Services struct {
	// Storage is the data folder.
	Storage store.Storage
	// GitHub reads and writes GitHub through gh.
	GitHub github.GitHub
	// Jira reads and comments on tickets through acli.
	Jira jira.Jira
	// Runner starts, watches and stops agent sessions.
	Runner runner.AgentRunner
	// Workspaces makes checkouts, worktrees and merges.
	Workspaces runner.Workspaces
	// LibraryURL gives a library repository's git remote. Nil: gh clones it from GitHub.
	// Demo mode and tests point it at local repositories.
	LibraryURL func(repository string) string
	// ModelDiscovery asks each agent CLI for its models. Nil: runner.CLIModelCatalog.
	ModelDiscovery runner.ModelCatalogDiscovery
	// Shell is the command runner the tools check and model discovery use. The
	// coordinator itself runs no commands; LaunchSetup passes it to them.
	Shell shell.Runner
	// ToolCheck runs the tools check. Nil reports no rows.
	ToolCheck ToolCheckFunc
	// Clock is the time. Nil: RealClock.
	Clock Clock
	// IDs makes the UUIDs. Nil: RealIDs.
	IDs IDs
	// Notifier delivers notifications. Nil: none.
	Notifier Notifier
	// Env reads the process environment for the agent executables and the model
	// settings. Nil: os.Getenv.
	Env func(string) string
}

// New builds a coordinator over services: it loads the settings, the library, the runs and the
// marketplace state, wires the parts with their closures, records the factory origin's signature when
// it has none, admits runs that lack an admission, and spawns the assistant's
// reconciliation.
// Lock: not held; it runs before any other caller exists. Spawns: the assistant
// reconciliation, through c.Spawn. The coordinator's context is Turns.Start's.
func New(services Services) (*Coordinator, error) {
	c := &Coordinator{services: services}
	if err := c.bindServices(); err != nil {
		return nil, err
	}
	loaded := c.loadSettings()
	c.buildModels()
	c.buildLibrary()
	c.buildRunBook()
	c.buildWorkers(loaded.Paused)
	c.admitStoredRuns()
	c.startTurns()
	return c, nil
}

// bindServices copies the services onto the coordinator, takes the default for each nil
// one, checks the three parts that have no default are present, and makes the maps every
// part writes to.
// Lock: not held; it runs in New, before any other caller exists.
func (c *Coordinator) bindServices() error {
	services := c.services
	c.clock = services.Clock
	if c.clock == nil {
		c.clock = RealClock{}
	}
	c.ids = services.IDs
	if c.ids == nil {
		c.ids = RealIDs{}
	}
	c.env = services.Env
	if c.env == nil {
		c.env = os.Getenv
	}
	c.notifier = services.Notifier
	c.toolCheck = services.ToolCheck
	c.storage = services.Storage
	c.paths = services.Storage.Paths
	c.github = services.GitHub
	c.jira = services.Jira
	c.runner = services.Runner
	c.workspaces = services.Workspaces
	// A missing part would only fail later, in a spawned goroutine.
	if c.runner == nil || c.workspaces == nil || c.github == nil || c.jira == nil {
		return fmt.Errorf("coordinator: Services needs Runner, Workspaces, GitHub and Jira")
	}
	// Git runs through the process runner even when Shell is nil: a local remote needs no gh.
	libraryShell := services.Shell
	if libraryShell == nil {
		libraryShell = shell.ProcessRunner{}
	}
	c.libraryRepos = library.Repos{Root: c.paths.LibraryRepos(), Runner: libraryShell, URL: services.LibraryURL}
	c.initMaps()
	return nil
}

// loadSettings reads the saved settings and sets the global pause flag from them. It returns the
// loaded settings, which the caller reads for the pause state.
// Lock: not held; it runs in New.
func (c *Coordinator) loadSettings() store.Settings {
	loaded := c.storage.LoadSettings()
	c.settings = loaded
	c.settingsRevision++
	c.setGloballyPausedFlag(loaded.Paused)
	return loaded
}

// buildModels makes the model catalogs, with the default for a nil model discovery.
// Lock: not held; it runs in New, after the settings are loaded.
func (c *Coordinator) buildModels() {
	discovery := c.services.ModelDiscovery
	if discovery == nil {
		discovery = runner.NewCLIModelCatalog(c.services.Shell, nil)
	}
	c.modelCatalogs = newModelCatalogs(discovery, 30*time.Second)
}

// buildLibrary makes the library, which reads the belts and agents, and gives it the model
// admission and the model labels its checks use.
// Library.init reads the belts and agents and then runs load(), which gives belts saved
// before watches saw reviews their outcomes once.
// Lock: not held; it runs in New.
func (c *Coordinator) buildLibrary() {
	c.library = newLibrary(c, c.storage)
	c.library.modelAdmission = func() core.ModelAdmission { return c.currentModelAdmission() }
	c.library.modelLabel = func(model string, backend core.Backend) *string {
		snapshot, ok := c.settings.ModelCatalogs[string(backend)]
		if !ok {
			return nil
		}
		for _, choice := range snapshot.Models {
			if choice.ID == model {
				label := choice.DisplayLabel()
				return &label
			}
		}
		return nil
	}
}

// buildRunBook makes the run book, whose effects the coordinator carries out, and the outputs
// that read the runs' attempts.
// Lock: not held; it runs in New.
func (c *Coordinator) buildRunBook() {
	c.book = newRunBook(c, c.storage, c.clock)
	c.book.carryOut = func(effects []core.Effect, run core.Run, session *string) {
		c.perform(effects, run, session)
	}
	c.outputs = newOutputs(c, c.paths, func(stage core.Stage, run core.Run) *string { return c.library.KeyIn(stage, run) })
}

// buildWorkers makes the parts that act on runs: sessions, watches, intake, AFK autopilot and the
// configuration assistant. paused is the saved global pause, which sessions and intake start with.
// Lock: not held; it runs in New.
func (c *Coordinator) buildWorkers(paused bool) {
	c.sessions = newSessions(c, &c.book, &c.library, c.runner, c.workspaces, c.github, c.paths, c.clock)
	c.sessions.globallyPaused = paused
	c.watches = newWatches(c, &c.book, c.github, c.clock)
	c.ticketWatches = newTicketWatches(c, &c.book, c.github, c.jira, c.clock)
	c.intake = newIntake(c, &c.book, &c.library, c.github, c.jira, c.clock)
	c.intake.globallyPaused = paused
	c.intake.admission = func() core.ModelAdmission { return c.currentModelAdmission() }
	c.intake.currentModelWarning = func(settings core.SessionSettings, name string) *string {
		backend := core.ValueOr(settings.Runner, core.BackendPi)
		return c.modelWarning(settings.Model, backend, name)
	}
	c.autopilot = newAutopilot(c, &c.book, c.runner, c.storage, c.clock)
	c.autopilot.settings = func() core.AfkSettings { return c.settings.Afk }
	c.autopilot.notify = func(title, body string) {
		if c.settings.Notifications && c.notifier != nil {
			c.notifier(title, body)
		}
	}
	c.autopilot.output = func(run core.Run, attempt *core.Attempt) *string {
		if attempt != nil {
			return c.outputs.output(run, *attempt)
		}
		return c.outputs.latest(run, nil)
	}
	c.autopilot.currentAdmission = func() core.ModelAdmission { return c.currentModelAdmission() }
	c.assistant = newLibraryAssistant(c, c.paths, c.runner)
}

// admitStoredRuns admits each saved run that has no admission yet, and notes the reason of each
// one it cannot admit.
// Lock: not held; it runs in New.
func (c *Coordinator) admitStoredRuns() {
	for _, run := range c.book.runs {
		if run.ModelAdmission != nil {
			continue
		}
		if _, err := c.sessions.admittedRun(run.ID); err != nil {
			id := run.ID
			c.book.note(err.Error(), &id)
		}
	}
}

// startTurns makes the projection the turns compare, starts the turns and spawns the assistant's
// startup probe.
// Lock: not held; it runs in New. Spawns: the assistant reconciliation, through c.Spawn.
func (c *Coordinator) startTurns() {
	// Each turn compares every resource projection with its baseline (Turns.compare).
	c.Project = c.projection
	c.Start(context.Background())
	// The startup probe runs once the turns are live, so it takes the lock like any other part.
	// turns are live, so it takes the lock like any other main-actor part.
	c.Spawn(func(ctx context.Context) {
		c.Lock()
		defer c.EndTurn()
		c.assistant.Reconcile(ctx)
	})
}
