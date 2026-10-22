package shell

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Call is one command a Recorder saw. Deadline is the context's deadline, zero when it has none.
type Call struct {
	Executable string
	Args       []string
	Dir        string
	Input      *string
	Deadline   time.Time
}

// Line is the command as one string, "gh api repos/x", for assertions.
func (c Call) Line() string { return strings.Join(append([]string{c.Executable}, c.Args...), " ") }

// Recorder is the test Runner: it records every call and answers from Reply
// (nil means status 0 and no output). Safe for concurrent use.
type Recorder struct {
	Reply func(Call) Result

	mu    sync.Mutex
	calls []Call
}

// Run records the call and returns Reply's answer.
func (r *Recorder) Run(ctx context.Context, executable string, args []string, dir string, input *string) Result {
	call := Call{Executable: executable, Args: append([]string(nil), args...), Dir: dir, Input: input}
	if deadline, ok := ctx.Deadline(); ok {
		call.Deadline = deadline
	}
	r.mu.Lock()
	r.calls = append(r.calls, call)
	reply := r.Reply
	r.mu.Unlock()
	if reply == nil {
		return Result{}
	}
	return reply(call)
}

// Calls returns a copy of the calls so far, in order.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Call(nil), r.calls...)
}

// Stalled is the test Runner for a command that never finishes on its own: it
// waits for its context to end, then returns Reply, the output it printed before
// it was stopped. A test gives its caller a deadline, or the wait is the limit.
type Stalled struct {
	Reply Result
}

// Run waits for ctx to end and returns Reply.
func (s Stalled) Run(ctx context.Context, _ string, _ []string, _ string, _ *string) Result {
	<-ctx.Done()
	return s.Reply
}
