package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/jira"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/kit/tools"
)

// Notifier delivers a notification's title and body; the daemon shows it, a
// headless caller does what it likes (a typealias of the
// coordinator's).
type Notifier = coordinator.Notifier

// LaunchSetupLive builds the live coordinator for the data folder at paths: the one
// over gh, acli, tmux and git.
// checkCommand is how the configuration assistant checks its changes; nil when
// there is none to run. The tools check runs tools.Run through the real shell.
// Lock: none; it runs before the coordinator is shared.
func LaunchSetupLive(paths store.Paths, notifier Notifier, checkCommand []string) (*coordinator.Coordinator, error) {
	sh := shell.ProcessRunner{}
	c, err := coordinator.New(coordinator.Services{
		Storage:    store.NewStorage(paths),
		GitHub:     github.NewGHCLI(sh),
		Jira:       jira.NewAcliJira(sh),
		Runner:     runner.NewTmuxRunner(sh),
		Workspaces: runner.NewGitWorkspaces(paths, sh),
		Shell:      sh,
		ToolCheck: func(ctx context.Context) []tools.Result {
			return tools.Run(ctx, sh, nil)
		},
		Notifier: notifier,
	})
	if err != nil {
		return nil, err
	}
	c.SetAssistantCheckCommand(checkCommand)
	return c, nil
}

// LaunchSetupDemo builds the demo coordinator for root: an empty folder is seeded,
// one that already holds a demo is reopened as it is. Its tools check returns the
// fixtures.
// Lock: none; it runs before the coordinator is shared.
func LaunchSetupDemo(root string, notifier Notifier, checkCommand []string) (*coordinator.Coordinator, error) {
	setup, err := demo.Prepare(root, time.Now(), os.Getenv)
	if err != nil {
		return nil, err
	}
	c, err := coordinator.New(coordinator.Services{
		Storage:        setup.Storage,
		GitHub:         setup.Services.GitHub(),
		Jira:           setup.Services.Jira(),
		Runner:         setup.Runner,
		Workspaces:     setup.Workspaces,
		LibraryURL:     setup.LibraryURL,
		ModelDiscovery: runner.FixtureModelCatalog{},
		ToolCheck: func(context.Context) []tools.Result {
			return tools.Fixtures()
		},
		Notifier: notifier,
	})
	if err != nil {
		return nil, err
	}
	c.SetAssistantCheckCommand(checkCommand)
	return c, nil
}

// LaunchSessionBuild builds a coordinator for a launch.
type LaunchSessionBuild func(root string, notifier Notifier, checkCommand []string) (*coordinator.Coordinator, error)

// LaunchSession is a launch that owns its data folder: the folder's lock, taken
// before anything is read or seeded, and the coordinator built while holding it.
// Keep it for as long as the coordinator runs, and Close it after the coordinator
// has stopped ticking.
type LaunchSession struct {
	// Root is the data folder.
	Root string
	// Demo is set for a demo folder.
	Demo bool
	// Coordinator is the coordinator the launch built.
	Coordinator *coordinator.Coordinator
	// Lock is the folder's lock, held until Close.
	Lock *OwnershipLock
	// AutomaticRoot is set for a demo folder this launch allocated itself, removed when
	// it closes. A folder the caller supplied stays.
	AutomaticRoot bool
	// Upgrade is what upgrading a live folder to the current format did, and
	// UpgradeError why it stopped; a failed upgrade does not stop the launch (a folder
	// in a newer format does: LaunchFailureNewerFormat),
	// since the files it left still read, and the next launch tries again.
	Upgrade      store.UpgradeReport
	UpgradeError error
}

// newLaunchSession builds a session around its parts.
// Lock: none.
func newLaunchSession(root string, demo bool, built *coordinator.Coordinator, lock *OwnershipLock, automaticRoot bool) *LaunchSession {
	return &LaunchSession{Root: root, Demo: demo, Coordinator: built, Lock: lock, AutomaticRoot: automaticRoot}
}

// launchCurrentPID is the launching process, which records itself in the lock.
func launchCurrentPID() int32 { return int32(os.Getpid()) }

// LaunchSessionLive owns the live data folder at paths and builds its coordinator
// with build (nil: LaunchSetupLive). A folder another launch owns is refused before
// anything is built.
// Lock: none; it takes the folder's lock before building.
func LaunchSessionLive(paths store.Paths, notifier Notifier, checkCommand []string, build LaunchSessionBuild) (*LaunchSession, error) {
	lock, err := OwnershipLockAcquire(paths.Root, launchCurrentPID())
	if err != nil {
		return nil, err
	}
	if build == nil {
		build = func(root string, notifier Notifier, checkCommand []string) (*coordinator.Coordinator, error) {
			return LaunchSetupLive(store.Paths{Root: root}, notifier, checkCommand)
		}
	}
	upgrade, upgradeErr := store.NewStorage(paths).Upgrade(time.Now())
	var newer store.NewerFormat
	if errors.As(upgradeErr, &newer) {
		lock.Release()
		return nil, LaunchFailure{Kind: LaunchFailureNewerFormat, Folder: paths.Root, Message: newer.Error()}
	}
	built, err := build(paths.Root, notifier, checkCommand)
	if err != nil {
		lock.Release()
		return nil, LaunchFailure{Kind: LaunchFailureIO, Folder: paths.Root,
			Message: "Could not open the data folder: " + err.Error()}
	}
	session := newLaunchSession(paths.Root, false, built, lock, false)
	session.Upgrade, session.UpgradeError = upgrade, upgradeErr
	return session, nil
}

// LaunchSessionDemo owns a demo folder and builds its coordinator: root as it is when
// given, otherwise a fresh folder under parent after the folders of demos that
// are gone are swept. build nil means LaunchSetupDemo
// (LaunchSession.demo(root:parent:notifier:checkCommand:build:)).
// Lock: none; it takes the folder's lock before building.
func LaunchSessionDemo(root *string, parent string, notifier Notifier, checkCommand []string, build LaunchSessionBuild) (*LaunchSession, error) {
	automatic := root == nil
	var folder string
	if root != nil {
		folder = *root
	} else {
		if parent == "" {
			parent = demo.DemoRootParent(os.Getenv, os.TempDir())
		}
		folder = demo.DemoRootAllocate(parent, os.Getpid(), syscall.Kill)
	}
	lock, err := OwnershipLockAcquire(folder, launchCurrentPID())
	if err != nil {
		if automatic {
			_ = os.RemoveAll(folder)
		}
		return nil, err
	}
	if build == nil {
		build = LaunchSetupDemo
	}
	built, err := build(folder, notifier, checkCommand)
	if err != nil {
		if automatic {
			_ = os.RemoveAll(folder)
		}
		lock.Release()
		return nil, LaunchFailure{Kind: LaunchFailureIO, Folder: folder,
			Message: fmt.Sprintf("Could not seed the demo: %s", err.Error())}
	}
	return newLaunchSession(folder, true, built, lock, automatic), nil
}

// Close ends the launch once its coordinator has stopped: it removes a demo folder
// it allocated, then lets the folder's lock go.
// Lock: none; the caller has stopped the coordinator's ticking.
func (s *LaunchSession) Close() {
	if !s.Lock.Held() {
		return
	}
	if s.AutomaticRoot {
		_ = os.RemoveAll(s.Root)
	}
	s.Lock.Release()
}
