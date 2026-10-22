// Tests of the model catalog lifecycle: two refreshes that overlap, one superseded by a forced
// refresh and a second runner, keep each runner's own catalog, and a contradictory answer keeps the
// last catalog and the permissions until a clean answer replaces them. The other model tests are in
// coordinator_model_test.go. The controlled catalog holds each discovery until the test answers it.

package coordinator

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// coordAnswer is one discovery's answer.
type coordAnswer struct {
	models []core.ModelChoice
	err    error
}

// coordCall is one discovery that started: its number and runner.
type coordCall struct {
	number  int
	backend core.Backend
}

// controlledDiscovery holds each discovery until the test answers it.
type controlledDiscovery struct {
	started chan coordCall
	mu      sync.Mutex
	count   int
	pending map[int]chan coordAnswer
}

// newH5mControlledDiscovery builds a discovery that starts no call until the test asks for one.
func newH5mControlledDiscovery() *controlledDiscovery {
	return &controlledDiscovery{started: make(chan coordCall, 16), pending: map[int]chan coordAnswer{}}
}

// Discover records the call, reports it started, and waits for the test's answer to it.
func (d *controlledDiscovery) Discover(ctx context.Context, backend core.Backend, _ string) ([]core.ModelChoice, error) {
	d.mu.Lock()
	d.count++
	number := d.count
	answer := make(chan coordAnswer, 1)
	d.pending[number] = answer
	d.mu.Unlock()
	d.started <- coordCall{number: number, backend: backend}
	select {
	case a := <-answer:
		return a.models, a.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// next waits for the next discovery to start and returns it.
func (d *controlledDiscovery) next(t *testing.T) coordCall {
	t.Helper()
	select {
	case call := <-d.started:
		return call
	case <-time.After(10 * time.Second):
		t.Fatalf("a discovery did not start")
		return coordCall{}
	}
}

// answer completes discovery number with models, or err when it is set.
func (d *controlledDiscovery) answer(number int, a coordAnswer) {
	d.mu.Lock()
	answer := d.pending[number]
	delete(d.pending, number)
	d.mu.Unlock()
	answer <- a
}

// refreshAsync runs one refresh of backend on its own goroutine, taking the lock as a turn does, and returns a
// channel that closes when it finishes.
func refreshAsync(c *Coordinator, service *ModelCatalogs, backend core.Backend, force bool) chan struct{} {
	done := make(chan struct{})
	go func() {
		c.Lock()
		service.Refresh(context.Background(), backend, c, force)
		c.EndTurn()
		close(done)
	}()
	return done
}

// TestOverlappingRequestsAndOtherRunnersRemainIsolated: a forced refresh supersedes an earlier one, so the older
// answer is dropped, and another runner's catalog is its own; nothing stays loading.
func TestOverlappingRequestsAndOtherRunnersRemainIsolated(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	discovery := newH5mControlledDiscovery()
	service := newModelCatalogs(discovery, 30*time.Second)

	first := refreshAsync(c, &service, core.BackendPi, false)
	firstCall := discovery.next(t)
	second := refreshAsync(c, &service, core.BackendPi, true)
	secondCall := discovery.next(t)
	other := refreshAsync(c, &service, core.BackendCodex, false)
	otherCall := discovery.next(t)
	if firstCall.backend != core.BackendPi || secondCall.backend != core.BackendPi || otherCall.backend != core.BackendCodex {
		t.Fatalf("the discoveries started for %v, %v, %v", firstCall.backend, secondCall.backend, otherCall.backend)
	}
	discovery.answer(secondCall.number, coordAnswer{models: []core.ModelChoice{{ID: "new", Name: "New"}}})
	<-second
	discovery.answer(firstCall.number, coordAnswer{models: []core.ModelChoice{{ID: "stale", Name: "Stale"}}})
	<-first
	discovery.answer(otherCall.number, coordAnswer{models: []core.ModelChoice{{ID: "codex", Name: "Codex"}}})
	<-other

	c.Lock()
	piIDs := modelIDsOf(c.settings.ModelCatalogs[string(core.BackendPi)])
	codexIDs := modelIDsOf(c.settings.ModelCatalogs[string(core.BackendCodex)])
	loading := len(service.loading)
	c.EndTurn()
	if !reflect.DeepEqual(piIDs, []string{"new"}) {
		t.Fatalf("pi catalog = %v, want [new]", piIDs)
	}
	if !reflect.DeepEqual(codexIDs, []string{"codex"}) {
		t.Fatalf("codex catalog = %v, want [codex]", codexIDs)
	}
	if loading != 0 {
		t.Fatalf("%d runners are still loading", loading)
	}
}

// modelIDsOf returns the ids of a catalog's models, or nil when there is no catalog.
func modelIDsOf(snapshot core.ModelCatalogSnapshot) []string {
	var ids []string
	for _, model := range snapshot.Models {
		ids = append(ids, model.ID)
	}
	return ids
}

// TestContradictoryContextKeepsTheLastCatalogAndPermissions: a discovery that reports one model with two context
// sizes keeps the last catalog and the permissions; the next clean discovery replaces the catalog, leaves the
// permissions, and makes the one allowed model unavailable.
func TestContradictoryContextKeepsTheLastCatalogAndPermissions(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	c.Lock()
	c.changeSettings(func(s *store.Settings) { s.AllowedModels = map[string]map[string]struct{}{} })
	c.EndTurn()
	discovery := newH5mControlledDiscovery()
	service := newModelCatalogs(discovery, 30*time.Second)

	first := refreshAsync(c, &service, core.BackendPi, true)
	firstCall := discovery.next(t)
	kept := core.ModelChoice{ID: "vendor/opus", Name: "Opus 5.5", ContextTokens: new(250_000)}
	discovery.answer(firstCall.number, coordAnswer{models: []core.ModelChoice{kept, {ID: "vendor/plain", Name: "Plain"}}})
	<-first
	if err := c.SetModelAllowed("vendor/opus", core.BackendPi, true); err != nil {
		t.Fatalf("allow vendor/opus: %v", err)
	}
	c.Lock()
	before := c.settings.ModelCatalogs[string(core.BackendPi)]
	allowed := c.settings.AllowedModels[string(core.BackendPi)]
	c.EndTurn()

	second := refreshAsync(c, &service, core.BackendPi, true)
	secondCall := discovery.next(t)
	discovery.answer(secondCall.number, coordAnswer{models: []core.ModelChoice{
		{ID: "vendor/opus", Name: "Opus 5.5", ContextTokens: new(250_000)},
		{ID: "vendor/opus", Name: "Opus 5.5", ContextTokens: new(1_000_000)},
	}})
	<-second
	c.Lock()
	kept2 := c.settings.ModelCatalogs[string(core.BackendPi)]
	allowed2 := c.settings.AllowedModels[string(core.BackendPi)]
	errText := service.errors[core.BackendPi]
	c.EndTurn()
	if !reflect.DeepEqual(kept2, before) || !reflect.DeepEqual(allowed2, allowed) {
		t.Fatalf("a contradictory answer replaced the catalog or the permissions")
	}
	if !strings.Contains(errText, "Contradictory context sizes") {
		t.Fatalf("error = %q, want the contradiction", errText)
	}
	visible := c.AllowedModels(core.BackendPi, "")
	if len(visible) != 1 || visible[0].ID != "vendor/opus" || visible[0].ContextTokens == nil || *visible[0].ContextTokens != 250_000 {
		t.Fatalf("allowed models = %+v, want vendor/opus at 250000", visible)
	}

	third := refreshAsync(c, &service, core.BackendPi, true)
	thirdCall := discovery.next(t)
	discovery.answer(thirdCall.number, coordAnswer{models: []core.ModelChoice{{ID: "vendor/plain", Name: "Plain"}}})
	<-third
	c.Lock()
	final := c.settings.ModelCatalogs[string(core.BackendPi)]
	finalAllowed := c.settings.AllowedModels[string(core.BackendPi)]
	unavailable := core.ModelAllowance{Identifiers: map[core.Backend]map[string]struct{}{core.BackendPi: finalAllowed}}.
		UnavailableIdentifiers(core.BackendPi, &final, core.BackendPi.Executable(c.env))
	remaining := c.settings.AllowedModels[string(core.BackendPi)]
	c.EndTurn()
	if _, ok := service.errors[core.BackendPi]; ok {
		t.Fatalf("a clean answer kept the error %q", service.errors[core.BackendPi])
	}
	if len(final.Models) != 1 || final.Models[0].ContextTokens != nil {
		t.Fatalf("the clean catalog = %+v, want one model with no context size", final.Models)
	}
	if !reflect.DeepEqual(finalAllowed, allowed) || !reflect.DeepEqual(remaining, allowed) {
		t.Fatalf("the permissions changed")
	}
	if got := c.AllowedModels(core.BackendPi, ""); len(got) != 0 {
		t.Fatalf("allowed models after the clean answer = %+v, want none", got)
	}
	if !reflect.DeepEqual(unavailable, []string{"vendor/opus"}) {
		t.Fatalf("unavailable = %v, want [vendor/opus]", unavailable)
	}
}
