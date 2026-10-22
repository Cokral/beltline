// The Insights reads: the runs and the library's belt names, through core.Insights and
// core.InsightsEvolution.

package coordinator

import (
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// ReadInsightsView is the Insights view for query, ending at now (the clock when nil) with days
// counted in loc. It reports false when query names a belt, or a version of it, that neither the
// library nor any run has.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadInsightsView(query core.InsightsQuery, now *time.Time, loc *time.Location) (core.InsightsView, bool) {
	c.Lock()
	defer c.EndTurn()
	library := c.insightsLibrary()
	if !core.InsightsHasBelt(query, library, c.book.runs) {
		return core.InsightsView{}, false
	}
	return core.Insights(c.book.runs, library, query, c.momentOf(now), loc), true
}

// ReadInsightsEvolution is the Evolution view of a belt for query, with weeks counted in loc. It
// reports false when the belt is neither the library's nor any run's.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadInsightsEvolution(query core.InsightsEvolutionQuery, loc *time.Location) (core.InsightsEvolutionView, bool) {
	c.Lock()
	defer c.EndTurn()
	library := c.insightsLibrary()
	if query.Belt == "" || !core.InsightsHasBelt(core.InsightsQuery{Belt: query.Belt}, library, c.book.runs) {
		return core.InsightsEvolutionView{}, false
	}
	return core.InsightsEvolution(c.book.runs, library, query, loc), true
}

// insightsLibrary is the library's belts as Insights needs them.
// Lock: runs with c.mu held.
func (c *Coordinator) insightsLibrary() []core.InsightsLibraryBelt {
	library := make([]core.InsightsLibraryBelt, 0, len(c.library.belts))
	for _, record := range c.library.belts {
		library = append(library, core.InsightsLibraryBelt{
			ID: record.Belt.ID, Name: record.Belt.Name, Version: record.Belt.Version, Stages: record.Belt.Stages,
			Archived: record.Archived, ShadowTests: record.ShadowTests,
		})
	}
	return library
}
