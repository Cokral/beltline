package coordinator

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// ModelCatalogs asks each agent CLI for the models it offers and keeps the
// result, the loading flags and the last error per runner. A part of
// Coordinator: its state is read and written with c.mu held; the discovery
// itself runs without it.
type ModelCatalogs struct {
	// loading holds the runners whose discovery is in flight.
	loading map[core.Backend]struct{}
	// errors holds each runner's last discovery error, until a forced refresh.
	errors map[core.Backend]string
	// requested is the executable each runner's discovery was last asked of.
	requested map[core.Backend]string
	// requests holds the identity of each runner's latest request, so a superseded
	// answer is rejected.
	requests  map[core.Backend]string
	discovery runner.ModelCatalogDiscovery
	// timeout bounds one discovery.
	timeout time.Duration
	// revision counts writes to loading and errors, which the models projection reads.
	revision uint64
}

// setLoading marks discovery for runner as in flight. Every write to loading
// or errors goes through these methods, so the revision moves with it.
// Lock: runs with c.mu held.
func (m *ModelCatalogs) setLoading(runner core.Backend) {
	m.loading[runner] = struct{}{}
	m.revision++
}

// clearLoading marks discovery for runner as finished. Lock: runs with c.mu held.
func (m *ModelCatalogs) clearLoading(runner core.Backend) {
	delete(m.loading, runner)
	m.revision++
}

// setError keeps runner's discovery error text. Lock: runs with c.mu held.
func (m *ModelCatalogs) setError(runner core.Backend, text string) {
	m.errors[runner] = text
	m.revision++
}

// clearError forgets runner's discovery error. Lock: runs with c.mu held.
func (m *ModelCatalogs) clearError(runner core.Backend) {
	delete(m.errors, runner)
	m.revision++
}

// newModelCatalogs builds the model catalogs over discovery, bounded by timeout
// .
// Lock: runs during New, before the coordinator is shared.
func newModelCatalogs(discovery runner.ModelCatalogDiscovery, timeout time.Duration) ModelCatalogs {
	return ModelCatalogs{
		loading: map[core.Backend]struct{}{}, errors: map[core.Backend]string{},
		requested: map[core.Backend]string{}, requests: map[core.Backend]string{},
		discovery: discovery, timeout: timeout,
	}
}

// RequestDiscovery starts discovery for a runner that has no usable catalog,
// without waiting for it. loading is set at once, so a read made now already
// reports the refresh. Only the first request per executable runs; a failure
// stays in errors until a forced refresh.
// Lock: exported on the part; runs with c.mu held. Spawns: Refresh, through c.Spawn.
func (m *ModelCatalogs) RequestDiscovery(runner core.Backend, c *Coordinator) {
	executable := runner.Executable(c.env)
	if m.isRequested(runner, executable) {
		return
	}
	m.setLoading(runner)
	c.Spawn(func(ctx context.Context) {
		c.Lock()
		defer c.EndTurn()
		m.Refresh(ctx, runner, c, false)
	})
}

// isRequested reports whether discovery was already asked of executable for runner. Lock: none (pure).
func (m *ModelCatalogs) isRequested(runner core.Backend, executable string) bool {
	requested, ok := m.requested[runner]
	return ok && requested == executable
}

// Refresh runs discovery for one runner and stores the catalog it returns, or its
// error. A catalog is kept on failure, and a superseded answer is dropped.
// Lock: exported on the part; runs with c.mu held. Suspends: boundedDiscovery
// (run without the lock through c.Suspend).
func (m *ModelCatalogs) Refresh(ctx context.Context, runner core.Backend, c *Coordinator, force bool) {
	executable := runner.Executable(c.env)
	if !force && m.isRequested(runner, executable) {
		return
	}
	m.requested[runner] = executable
	request := c.ids.UUID()
	m.requests[runner] = request
	m.setLoading(runner)
	m.clearError(runner)
	var models []core.ModelChoice
	var err error
	c.Suspend(func() { models, err = m.boundedDiscovery(ctx, runner, executable) })
	if m.requests[runner] != request {
		return
	}
	if err == nil && !modelCatalogRowsNamed(models) {
		err = core.NewFailure(fmt.Sprintf("%s returned a malformed catalog. Update its CLI and Retry.", runner.Label()))
	}
	if err == nil {
		snapshot, snapshotErr := core.NewModelCatalogSnapshot(runner, executable, c.clock.Now(), models)
		if snapshotErr == nil {
			snapshotErr = c.saveModelCatalogNow(snapshot)
		}
		err = snapshotErr
	}
	if err != nil {
		if m.requests[runner] != request {
			return
		}
		m.setError(runner, err.Error())
	}
	if m.requests[runner] != request {
		return
	}
	m.clearLoading(runner)
}

// modelCatalogRowsNamed reports whether every discovered model has an id and a name. Lock: none (pure).
func modelCatalogRowsNamed(models []core.ModelChoice) bool {
	return !slices.ContainsFunc(models, func(model core.ModelChoice) bool {
		return strings.TrimSpace(model.ID) == "" || model.Name == ""
	})
}

// boundedDiscovery runs one discovery of runner's executable, failing after timeout.
// Lock: not held. The caller releases c.mu through c.Suspend around this call.
func (m *ModelCatalogs) boundedDiscovery(ctx context.Context, runner core.Backend, executable string) ([]core.ModelChoice, error) {
	gate := newModelCatalogsCompletion()
	timer := time.AfterFunc(m.timeout, func() {
		gate.finish(modelCatalogsResult{err: core.NewFailure(fmt.Sprintf(
			"%s catalog discovery timed out. Check its executable and terminal sign-in, then Retry.", runner.Label()))})
	})
	defer timer.Stop()
	discovery := m.discovery
	go func() {
		models, err := discovery.Discover(ctx, runner, executable)
		gate.finish(modelCatalogsResult{models: models, err: err})
	}()
	result := <-gate.result
	return result.models, result.err
}

// modelCatalogsCompletion gates a bounded discovery so that exactly one of the
// discovery and the timer answers it.
type modelCatalogsCompletion struct {
	// result receives the one answer.
	result chan modelCatalogsResult
	once   sync.Once
}

// modelCatalogsResult is a discovery's answer: the models or the error.
type modelCatalogsResult struct {
	models []core.ModelChoice
	err    error
}

// newModelCatalogsCompletion makes a gate for one discovery.
// Lock: none.
func newModelCatalogsCompletion() *modelCatalogsCompletion {
	return &modelCatalogsCompletion{result: make(chan modelCatalogsResult, 1)}
}

// finish delivers the answer, unless one was already delivered.
// Lock: none.
func (g *modelCatalogsCompletion) finish(result modelCatalogsResult) {
	g.once.Do(func() { g.result <- result })
}
