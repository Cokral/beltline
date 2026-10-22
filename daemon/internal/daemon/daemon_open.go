package daemon

import (
	"context"
	"fmt"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
)

// DaemonOpen opens a browser on a daemon that has already published discovery.
// This command is deliberately read-only until every discovery and API check
// has passed; it never starts or repairs a daemon. The checks are
// DaemonDiscovery's, which the app's ownership window shares.
type DaemonOpen struct {
	Runner    shell.Runner
	Discovery launch.DaemonDiscovery
	// Platform says how to open a URL; the zero value is this system's.
	Platform launch.Platform
}

// Run verifies the daemon in folder and opens its sign-in URL in the browser
// (open on macOS, xdg-open on Linux). A failed verification or a failed launch is an error, and
// the browser is never launched for an unverified daemon.
// Lock: none. Suspends: the discovery probe and the open command, run without any lock.
func (o DaemonOpen) Run(ctx context.Context, folder string) error {
	verified, err := o.Discovery.Verify(ctx, folder)
	if err != nil {
		return err
	}
	command := o.Platform.Open(verified.SignInURL)
	result := o.Runner.Run(ctx, command.Executable, command.Arguments, "", nil)
	if !result.OK() {
		return launch.DaemonDiscoveryFailure{
			Folder: folder,
			Step:   "launch browser",
			Detail: fmt.Sprintf("command returned exit %d", result.Status),
		}
	}
	return nil
}

// DaemonOpenLiveFolder is the data folder `beltlined open` signs in to: BELTLINE_DATA_DIR
// when set, else Beltline in support.
// Lock: none.
func DaemonOpenLiveFolder(environment map[string]string, support string) string {
	return store.StandardPaths(func(key string) string { return environment[key] }, support).Root
}
