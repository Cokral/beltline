// The one-turn reads of the belts and agents responses. Each read takes the lock once, so a response
// is one snapshot.

package coordinator

import (
	"reflect"
	"slices"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// BeltSnapshotReads are the reads a belt's API snapshot is built from, taken in one turn.
type BeltSnapshotReads struct {
	// Context is the belt's actions, warnings and revision.
	Context BeltResourceContext
	// GlobalAfkOn is the global AFK switch.
	GlobalAfkOn bool
	// Draft is the belt with its legacy stage keys converted.
	Draft core.Belt
	// Shadow is the belt's running shadow test, or nil.
	Shadow *core.ShadowSummary
}

// BeltsReads is what GET /api/v1/belts is built from: every belt record with its
// snapshot reads, and the library browser, in one turn.
type BeltsReads struct {
	Records   []store.BeltRecord
	Snapshots []BeltSnapshotReads
	Browser   core.LibraryBrowser
}

// LiveIDs are the ids of the belts that are not archived, in list order. The belt
// editor falls back to the first of them, and the Belts page opens it by default.
func (r BeltsReads) LiveIDs() []string {
	ids := make([]string, 0, len(r.Records))
	for _, record := range r.Records {
		if !record.Archived {
			ids = append(ids, record.Belt.ID)
		}
	}
	return ids
}

// AgentsReads is what GET /api/v1/agents is built from: every agent record with its
// belt count, and the library browser, in one turn.
type AgentsReads struct {
	Records    []core.AgentRecord
	BeltCounts map[string]int
	Browser    core.LibraryBrowser
}

// ReadBeltSnapshot returns the reads one belt's snapshot is built from, in one turn.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadBeltSnapshot(record store.BeltRecord) BeltSnapshotReads {
	c.Lock()
	defer c.EndTurn()
	return c.beltSnapshotReadsNow(record)
}

// beltSnapshotReadsNow is ReadBeltSnapshot with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) beltSnapshotReadsNow(record store.BeltRecord) BeltSnapshotReads {
	return BeltSnapshotReads{
		Context:     c.beltResourceContextNow(record, false),
		GlobalAfkOn: c.settings.Afk.Enabled,
		Draft:       c.library.ConvertingLegacyKeys(record.Belt),
		Shadow:      c.shadowSummaryNow(record),
	}
}

// ReadShadowSummary is the belt's running shadow test as its resource carries it, or nil.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadShadowSummary(record store.BeltRecord) *core.ShadowSummary {
	c.Lock()
	defer c.EndTurn()
	return c.shadowSummaryNow(record)
}

// shadowSummaryNow is the belt's running shadow test as its resource carries it, or nil.
// Lock: runs with c.mu held.
func (c *Coordinator) shadowSummaryNow(record store.BeltRecord) *core.ShadowSummary {
	test := record.ShadowTest()
	if test == nil || record.Shadow == nil {
		return nil
	}
	shadow := core.ShadowDefinition(*record.Shadow, record.Belt)
	summary := core.NewShadowSummary(*test, record.Belt, shadow, c.book.runs, c.library.Warnings(shadow))
	return &summary
}

// ReadBeltsReads returns the belts response's reads in one turn.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadBeltsReads(search, filter string) BeltsReads {
	c.Lock()
	defer c.EndTurn()
	c.library.reconcileFiles()
	records := c.readBeltsNow()
	snapshots := make([]BeltSnapshotReads, 0, len(records))
	for _, record := range records {
		snapshots = append(snapshots, BeltSnapshotReads{
			Context:     c.savedBeltContext(record),
			GlobalAfkOn: c.settings.Afk.Enabled,
			Draft:       c.library.ConvertingLegacyKeys(record.Belt),
			Shadow:      c.shadowSummaryNow(record),
		})
	}
	return BeltsReads{Records: records, Snapshots: snapshots, Browser: c.readBeltBrowserNow(search, filter)}
}

// beltContextMemo keeps each saved belt's context (beltResourceContextNow, not dirty) with the
// revisions it was worked out at: the library's, which every belt and agent write and every
// reload bump, and the settings', whose models the warnings check against.
type beltContextMemo struct {
	library, settings uint64
	byID              map[string]BeltResourceContext
}

// savedBeltContext is beltResourceContextNow(record, false) for a belt record of the library's
// own. Validating every belt and encoding it for its revision was most of a belts read, and
// both depend only on the library and the settings, so a context is worked out again only when
// either moved. The memo hands out copies of its slices. A test build checks some hits against a
// fresh context, as the projection memos are checked.
// Lock: runs with c.mu held.
func (c *Coordinator) savedBeltContext(record store.BeltRecord) BeltResourceContext {
	memo := &c.beltContexts
	if memo.byID == nil || memo.library != c.library.revision || memo.settings != c.settingsRevision {
		*memo = beltContextMemo{library: c.library.revision, settings: c.settingsRevision, byID: map[string]BeltResourceContext{}}
	}
	context, ok := memo.byID[record.Belt.ID]
	if !ok {
		context = c.beltResourceContextNow(record, false)
		memo.byID[record.Belt.ID] = context
	} else if shouldVerifyMemo() && !reflect.DeepEqual(context, c.beltResourceContextNow(record, false)) {
		panic("coordinator: a belt's context changed without a library or settings revision bump")
	}
	return BeltResourceContext{
		Actions:  slices.Clone(context.Actions),
		Warnings: slices.Clone(context.Warnings),
		Revision: context.Revision,
	}
}

// ReadAgentsReads returns the agents response's reads in one turn.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadAgentsReads(search, filter string) AgentsReads {
	c.Lock()
	defer c.EndTurn()
	c.library.reconcileFiles()
	records := c.readAgentsNow()
	beltCounts := make(map[string]int, len(records))
	// The count is the belts using the agent, which is all the presentation's UsedBy is; the
	// full resource (its warnings, actions and a copy of the agent) is not needed for it.
	for _, record := range records {
		beltCounts[record.Agent.ID] = len(c.library.AgentUsages(record.Agent.ID))
	}
	return AgentsReads{Records: records, BeltCounts: beltCounts, Browser: c.readAgentBrowserNow(search, filter)}
}

// AgentDetailReads is what GET /api/v1/agents/{id} is built from: the agent's latest
// saved record with its commands and presentation, read in one turn, so the fields and
// the detail describe the same saved version.
type AgentDetailReads struct {
	Resource AgentResource
}

// ReadAgentDetail returns the reads one agent's detail is built from, in one turn. found is
// false when the library has no agent with that id, which is the detail's not-found answer.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadAgentDetail(id string) (reads AgentDetailReads, found bool) {
	c.Lock()
	defer c.EndTurn()
	c.library.reconcileFiles()
	record := c.library.LatestAgent(id)
	if record == nil {
		return AgentDetailReads{}, false
	}
	return AgentDetailReads{
		Resource: c.readAgentResourceNow(*record),
	}, true
}
