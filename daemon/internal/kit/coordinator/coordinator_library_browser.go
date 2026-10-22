package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// ReadBeltBrowser is the belt browser projection, read alongside the full editor
// records by its API response.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadBeltBrowser(search, filter string) core.LibraryBrowser {
	c.Lock()
	defer c.EndTurn()
	c.library.reconcileFiles()
	return c.readBeltBrowserNow(search, filter)
}

// readBeltBrowserNow is ReadBeltBrowser with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) readBeltBrowserNow(search, filter string) core.LibraryBrowser {
	records := c.browserBeltRecords()
	belts := make([]core.LibraryBeltState, 0, len(records))
	for _, record := range records {
		state := core.LibraryBeltState{Belt: record.Belt, Archived: record.Archived, Paused: record.Paused}
		if test := record.ShadowTest(); test != nil {
			state.Shadow = test.Shadow
		}
		belts = append(belts, state)
	}
	return core.LibraryBeltBrowserOf(belts, c.library.Agent, search, filter)
}

// ReadAgentBrowser is the agent browser projection, with usage and runner
// details from the same library read.
// Lock: exported entry point; takes c.mu for the whole read, so the read is one turn.
func (c *Coordinator) ReadAgentBrowser(search, filter string) core.LibraryBrowser {
	c.Lock()
	defer c.EndTurn()
	c.library.reconcileFiles()
	return c.readAgentBrowserNow(search, filter)
}

// readAgentBrowserNow is ReadAgentBrowser with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) readAgentBrowserNow(search, filter string) core.LibraryBrowser {
	agents := make([]core.LibraryAgentState, 0, len(c.library.agents))
	// A row shows who uses the agent and its glyph, the parts of its presentation
	// (agentPresentation) the browser reads; the rest, its warnings above all, is the detail's.
	for _, record := range c.library.agents {
		record.Agent = cloneAgent(record.Agent)
		agents = append(agents, core.LibraryAgentState{Record: record, UsedBy: c.library.AgentUsages(record.Agent.ID), Glyph: record.Agent.Glyph()})
	}
	return core.LibraryAgentBrowserOf(agents, search, filter)
}

// browserBeltRecords is the saved belts in the order the browsers list them.
// Lock: runs with c.mu held.
func (c *Coordinator) browserBeltRecords() []store.BeltRecord {
	records := append([]store.BeltRecord(nil), c.library.belts...)
	sortBeltRecords(records)
	return records
}
