package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"encoding/json"
)

// daemonDiscoveryHTTPOK is the status the authenticated version request must answer.
const daemonDiscoveryHTTPOK = 200

// DaemonDiscoveryReadFunc reads the discovery record at a path.
type DaemonDiscoveryReadFunc func(path string) ([]byte, error)

// DaemonDiscoveryPidIsAlive reports whether a pid names a live process.
type DaemonDiscoveryPidIsAlive func(pid int32) bool

// DaemonDiscoveryProbeFunc asks a daemon for its version with the record's token.
type DaemonDiscoveryProbeFunc func(ctx context.Context, info DaemonInfo) (DaemonDiscoveryReply, error)

// DaemonDiscoveryReply is the probe's answer: the HTTP status and the body.
type DaemonDiscoveryReply struct {
	Status int
	Body   []byte
}

// DaemonDiscoveryVerified is a daemon that passed every check.
type DaemonDiscoveryVerified struct {
	Pid     int32
	Port    int
	Version string
	// SignInURL carries the discovery token. Open it; never show or log it.
	SignInURL string
}

// DaemonDiscoveryFailure is why a daemon could not be verified. Its text names the
// folder and the step, never the token or the sign-in URL.
type DaemonDiscoveryFailure struct {
	Folder string
	Step   string
	Detail string
}

// Error describes the failure: "Could not open daemon for <folder>: <step> failed (<detail>)."
// Lock: none (pure).
func (f DaemonDiscoveryFailure) Error() string {
	return fmt.Sprintf("Could not open daemon for %s: %s failed (%s).", f.Folder, f.Step, f.Detail)
}

// ErrorDescription is the message the user sees: the
// same text as Error.
func (f DaemonDiscoveryFailure) ErrorDescription() (string, bool) {
	return f.Error(), true
}

// DaemonDiscovery finds the daemon that publishes discovery in a data folder and
// checks that it answers. Every step is read-only: daemon.json must be readable
// and valid, its pid live (and, when the caller names one, the folder lock's
// recorded owner), and an authenticated /api/v1/version must answer 200 with the
// API and build the record names. Nothing here starts, repairs or writes to the folder.
type DaemonDiscovery struct {
	readDiscovery DaemonDiscoveryReadFunc
	pidIsAlive    DaemonDiscoveryPidIsAlive
	probeVersion  DaemonDiscoveryProbeFunc
}

// NewDaemonDiscovery builds the discovery checker. A nil read or pid check takes
// the file and the process check, which DaemonDiscoveryProcessIsAlive
// which the caller supplies, so this package stays free of HTTP.
// Lock: none.
func NewDaemonDiscovery(readDiscovery DaemonDiscoveryReadFunc, pidIsAlive DaemonDiscoveryPidIsAlive,
	probeVersion DaemonDiscoveryProbeFunc) DaemonDiscovery {
	if readDiscovery == nil {
		readDiscovery = os.ReadFile
	}
	if pidIsAlive == nil {
		pidIsAlive = DaemonDiscoveryProcessIsAlive
	}
	return DaemonDiscovery{readDiscovery: readDiscovery, pidIsAlive: pidIsAlive, probeVersion: probeVersion}
}

// Verify verifies the daemon that published discovery in folder.
// Lock: none. Suspends: the version probe (a network call, run without any lock).
func (d DaemonDiscovery) Verify(ctx context.Context, folder string) (DaemonDiscoveryVerified, error) {
	return d.check(ctx, folder, nil)
}

// VerifyOwnedBy verifies the daemon in folder and requires its pid to be owner,
// the pid the folder's lock records for its holder.
// Lock: none. Suspends: the version probe.
func (d DaemonDiscovery) VerifyOwnedBy(ctx context.Context, folder string, owner int32) (DaemonDiscoveryVerified, error) {
	return d.check(ctx, folder, &owner)
}

// check runs every check in order; owner nil means no owner is required.
// Lock: none. Suspends: the version probe.
func (d DaemonDiscovery) check(ctx context.Context, folder string, owner *int32) (DaemonDiscoveryVerified, error) {
	fail := func(step, detail string) error {
		return DaemonDiscoveryFailure{Folder: folder, Step: step, Detail: detail}
	}
	file := filepath.Join(folder, DaemonInfoFileName)
	data, err := d.readDiscovery(file)
	if err != nil {
		return DaemonDiscoveryVerified{}, fail("read daemon.json", "file is missing or unreadable")
	}
	var info DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return DaemonDiscoveryVerified{}, fail("read daemon.json", "record is malformed")
	}
	switch {
	case info.Pid <= 0:
		return DaemonDiscoveryVerified{}, fail("validate daemon.json", "pid must be positive")
	case info.Port < 1 || info.Port > 65535:
		return DaemonDiscoveryVerified{}, fail("validate daemon.json", "port is invalid")
	case !daemonDiscoveryValidToken(info.Token):
		return DaemonDiscoveryVerified{}, fail("validate daemon.json", "token is invalid")
	case info.Version == "":
		return DaemonDiscoveryVerified{}, fail("validate daemon.json", "build version is empty")
	case info.APIVersion != VersionCurrent:
		return DaemonDiscoveryVerified{}, fail("validate daemon.json", fmt.Sprintf("unsupported API version %d", info.APIVersion))
	}
	if !d.pidIsAlive(info.Pid) {
		return DaemonDiscoveryVerified{}, fail("check daemon pid", fmt.Sprintf("process %d is not running", info.Pid))
	}
	if owner != nil && info.Pid != *owner {
		return DaemonDiscoveryVerified{}, fail("check daemon pid", fmt.Sprintf("process %d is not the lock's owner %d", info.Pid, *owner))
	}

	reply, err := d.probeVersion(ctx, info)
	if err != nil {
		return DaemonDiscoveryVerified{}, fail("check daemon API", "listener is unreachable or timed out")
	}
	if reply.Status != daemonDiscoveryHTTPOK {
		return DaemonDiscoveryVerified{}, fail("check daemon API", fmt.Sprintf("version request returned HTTP %d", reply.Status))
	}
	var version Version
	if err := json.Unmarshal(reply.Body, &version); err != nil {
		return DaemonDiscoveryVerified{}, fail("check daemon API", "version response is malformed")
	}
	if version.APIVersion != VersionCurrent || version.APIVersion != info.APIVersion {
		return DaemonDiscoveryVerified{}, fail("check daemon API", "API version does not match daemon.json")
	}
	if version.Version != info.Version {
		return DaemonDiscoveryVerified{}, fail("check daemon API", "build version does not match daemon.json")
	}

	signIn := daemonDiscoverySignInURL(info)
	if signIn == nil {
		return DaemonDiscoveryVerified{}, fail("build sign-in URL", "daemon address is invalid")
	}
	return DaemonDiscoveryVerified{Pid: info.Pid, Port: info.Port, Version: info.Version, SignInURL: *signIn}, nil
}

// daemonDiscoverySignInURL is `http://127.0.0.1:<port>/auth?token=<token>`, the
// loopback sign-in URL, or nil when the address is invalid.
// Lock: none (pure).
func daemonDiscoverySignInURL(info DaemonInfo) *string {
	if info.Port < 1 || info.Port > 65535 {
		return nil
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/auth?token=%s", info.Port, info.Token)
	return &url
}

// daemonDiscoveryValidToken reports whether token is 64 lower-case hex characters.
// Lock: none (pure).
func daemonDiscoveryValidToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for i := range len(token) {
		c := token[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// DaemonDiscoveryProcessIsAlive reports whether pid names a live process this user
// may signal. EPERM means it exists under another user, so it is alive too.
// Lock: none.
func DaemonDiscoveryProcessIsAlive(pid int32) bool {
	err := syscall.Kill(int(pid), 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
