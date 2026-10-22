package shell

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
)

// Stopped reasons: why a command did not finish on its own. Message reports one
// before any output the command printed.
const (
	// TimedOutReason is a command that ran past its limit or a caller's deadline.
	TimedOutReason = "command timed out"
	// CancelledReason is a command its caller cancelled.
	CancelledReason = "command cancelled"
)

// The fixed wall-clock limits of one command. A transfer (git clone, fetch or push,
// and gh repo clone) moves repository data and gets the longer one.
const (
	CommandLimit  = 2 * time.Minute
	TransferLimit = 10 * time.Minute
)

// Statuses of a stopped command, so they never read as a tool's own: 124 for a
// timeout, as timeout(1) reports one, and 130 for a cancellation, as Ctrl-C does.
const (
	timedOutStatus  = 124
	cancelledStatus = 130
)

// Bounded runs executable through r under the fixed limit for it (see Limit). The
// command stops when that limit or ctx ends first, whichever is sooner, and a
// stopped command's Result carries its reason in Stopped. A ctx that has already
// ended starts no command.
func Bounded(ctx context.Context, r Runner, executable string, args []string, dir string, input *string) Result {
	return bounded(ctx, r, Limit(executable, args), executable, args, dir, input)
}

// bounded is Bounded with the limit given, so tests can use a short one.
func bounded(parent context.Context, r Runner, limit time.Duration, executable string, args []string, dir string, input *string) Result {
	if err := parent.Err(); err != nil {
		return stopped(Result{}, err)
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	// A runner that ignores ctx and returns a clean result still counts as stopped
	// once ctx has ended: its output came after the limit.
	return stopped(r.Run(ctx, executable, args, dir, input), ctx.Err())
}

// stopped is result, or when err says the command was stopped, result with its
// reason and status. Captured output is kept; Message reports the reason first.
func stopped(result Result, err error) Result {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		result.Stopped = TimedOutReason
		result.Status = timedOutStatus
	case errors.Is(err, context.Canceled):
		result.Stopped = CancelledReason
		result.Status = cancelledStatus
	}
	return result
}

// Limit is the fixed wall-clock limit of one command: TransferLimit for a transfer
// (see IsTransfer), else CommandLimit.
func Limit(executable string, args []string) time.Duration {
	if IsTransfer(executable, args) {
		return TransferLimit
	}
	return CommandLimit
}

// IsTransfer reports whether a command moves repository data: git clone, fetch or
// push, and gh repo clone. git's options before its subcommand do not hide it.
func IsTransfer(executable string, args []string) bool {
	switch executable {
	case "git":
		switch GitSubcommand(args) {
		case "clone", "fetch", "push":
			return true
		}
	case "gh":
		return len(args) >= 2 && args[0] == "repo" && args[1] == "clone"
	}
	return false
}

// gitOptionsWithValue are git's global options whose value is the next argument.
// Written with an = they carry their value, and are skipped alone.
var gitOptionsWithValue = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env"}

// GitSubcommand is the git subcommand in args, after any global options such as
// -C dir or -c name=value. It is empty when args name none.
func GitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if slices.Contains(gitOptionsWithValue, args[i]) {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-") {
			continue
		}
		return args[i]
	}
	return ""
}
