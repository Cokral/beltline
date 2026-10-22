// Turns is the coordinator's main-actor model: one turn at a time holds the lock, every external call
// or wait happens in a suspension, background work is counted, and after each turn the resource
// projections say which resources changed.

package coordinator

import (
	"bytes"
	"context"
	"sort"
	"sync"
)

// Resource is a part of the coordinator's state a browser refetches when it
// changes, in declaration order.
type Resource string

const (
	ResourceRuns      Resource = "runs"
	ResourceLibrary   Resource = "library"
	ResourceSettings  Resource = "settings"
	ResourceState     Resource = "state"
	ResourceIntake    Resource = "intake"
	ResourceAssistant Resource = "assistant"
	ResourceAfk       Resource = "afk"
	ResourceModels    Resource = "models"
)

// AllResources lists every resource in declaration order.
var AllResources = []Resource{
	ResourceRuns, ResourceLibrary, ResourceSettings, ResourceState, ResourceIntake,
	ResourceAssistant, ResourceAfk, ResourceModels,
}

// Turns is the main actor. Embed it, set Project and OnChange, and call Start
// once the state is loaded.
type Turns struct {
	mu sync.Mutex
	wg sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc

	// Project returns a resource's projection: the coordinator state that
	// resource shows, encoded with jsonx.
	// Called with the lock held.
	Project func(Resource) []byte
	// OnChange receives the resources whose projection changed in a turn,
	// sorted. Called with the lock held, so it must not block or call back.
	OnChange func([]Resource)

	baselines map[Resource][]byte

	// held, while holding, keeps spawned work from starting until Release or
	// Idle (tests only; see Hold).
	heldMu  sync.Mutex
	holding bool
	held    []func(ctx context.Context)
}

// Hold keeps work spawned from now on from starting until Release or Idle.
// Tests use it to assert what their assertions expect before their next
// suspension: there, a Task a command spawned cannot run until the test awaits.
func (t *Turns) Hold() {
	t.heldMu.Lock()
	t.holding = true
	t.heldMu.Unlock()
}

// Release starts the held work and stops holding.
func (t *Turns) Release() {
	t.heldMu.Lock()
	t.holding = false
	work := t.held
	t.held = nil
	t.heldMu.Unlock()
	for _, w := range work {
		t.Spawn(w)
	}
}

// Start records the baseline projections and makes the turns live. Call it
// with the state loaded and before any other goroutine uses the coordinator.
func (t *Turns) Start(parent context.Context) {
	t.ctx, t.cancel = context.WithCancel(parent)
	t.baselines = map[Resource][]byte{}
	if t.Project != nil {
		for _, r := range AllResources {
			t.baselines[r] = t.Project(r)
		}
	}
}

// Lock begins a turn. Every exported coordinator method starts with
// `c.Lock(); defer c.EndTurn()`.
func (t *Turns) Lock() { t.mu.Lock() }

// EndTurn compares every resource's projection with the last one, reports the
// changed ones, and unlocks. The unlock is deferred, so a panic in a projection
// (a test build's memo check, say) does not leave the lock held for the cleanup.
func (t *Turns) EndTurn() {
	defer t.mu.Unlock()
	t.compare()
}

// Suspend is an await inside a main-actor function: it ends the turn,
// runs slow without the lock, and begins a new turn. State read before it may
// have changed after it.
func (t *Turns) Suspend(slow func()) {
	t.EndTurn()
	defer t.mu.Lock()
	slow()
}

// Spawn starts background work from a turn: the work runs in
// goroutine without the lock (it takes the lock, with Lock/EndTurn, for each
// main-actor part) and is counted, so Idle waits for it. ctx is cancelled by
// Close.
func (t *Turns) Spawn(work func(ctx context.Context)) {
	t.heldMu.Lock()
	if t.holding {
		t.held = append(t.held, work)
		t.heldMu.Unlock()
		return
	}
	t.heldMu.Unlock()
	ctx := t.liveContext()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		work(ctx)
	}()
}

// Idle waits until no spawned work is running. Tests call it instead of
// sleeping; call it without the lock held. Work that spawns more work is
// waited for too.
func (t *Turns) Idle() {
	t.Release()
	t.wg.Wait()
}

// Close cancels spawned work's context and waits for it to finish.
func (t *Turns) Close() {
	if t.cancel != nil {
		t.cancel()
	}
	t.Release()
	t.wg.Wait()
}

// Context is the coordinator's context, cancelled by Close. Before Start it is a
// background context, so work spawned before Start never receives nil.
func (t *Turns) Context() context.Context { return t.liveContext() }

// liveContext is the context spawned work receives: the coordinator's once it is
// started, a background context before that.
func (t *Turns) liveContext() context.Context {
	if t.ctx == nil {
		return context.Background()
	}
	return t.ctx
}

func (t *Turns) compare() {
	if t.Project == nil || t.baselines == nil {
		return
	}
	var changed []Resource
	for _, r := range AllResources {
		now := t.Project(r)
		if !bytes.Equal(now, t.baselines[r]) {
			t.baselines[r] = now
			changed = append(changed, r)
		}
	}
	if len(changed) > 0 && t.OnChange != nil {
		sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
		t.OnChange(changed)
	}
}
