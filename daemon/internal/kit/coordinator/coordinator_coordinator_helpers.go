// Small helpers the coordinator files share: a run's stage lookup, trimmed
// text, the deep copy of an agent, the copy of a belt record, the saved
// pauses as intervals.

package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"maps"
	"slices"
	"strings"
	"time"
)

// findStage returns the run's stage with id, or nil.
func findStage(run core.Run, id string) *core.Stage {
	index := run.StageIndexOf(id)
	if index < 0 {
		return nil
	}
	stage := run.Stages[index].Clone()
	return &stage
}

// trimmedOrNil trims the text and returns it, or nil when nothing is left.
func trimmedOrNil(text *string) *string {
	if text == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// cloneAgent copies an agent, so the copy shares nothing with the library's. It was a JSON round
// trip, which cost most of a belts read; the field copy is the same copy without the encoding.
func cloneAgent(agent core.Agent) core.Agent {
	return agent.Clone()
}

// cloneBeltRecord copies a belt record, its belt and its fired times.
func cloneBeltRecord(record store.BeltRecord) store.BeltRecord {
	record.Belt = record.Belt.Clone()
	if record.Fired != nil {
		fired := make(map[string]time.Time, len(record.Fired))
		maps.Copy(fired, record.Fired)
		record.Fired = fired
	}
	record.ShadowTests = slices.Clone(record.ShadowTests)
	if record.Shadow != nil {
		shadow := record.Shadow.Clone()
		record.Shadow = &shadow
	}
	return record
}

// coreDateIntervals converts the saved pauses to the core's intervals.
func coreDateIntervals(in []store.DateInterval) []core.DateInterval {
	out := make([]core.DateInterval, 0, len(in))
	for _, interval := range in {
		start := interval.Start.Time
		end := start.Add(time.Duration(interval.Duration * float64(time.Second)))
		out = append(out, core.DateInterval{Start: start, End: end})
	}
	return out
}
