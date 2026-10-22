package core

import (
	"sort"
	"strconv"
	"strings"
	"sync"
)

// metroLayoutPlanCacheLimit is how many plans the cache keeps before it empties.
const metroLayoutPlanCacheLimit = 64

// metroPlanCacheStore is the process-wide cache of plans, keyed by the paths and layers they depend on.
type metroPlanCacheStore struct {
	mu    sync.Mutex
	plans map[string]metroLayoutPlan
}

// metroPlanCache is the cache of metro plans.
var metroPlanCache metroPlanCacheStore

// metroLayoutPlanCached is the plan for routes on track, reused from the cache when the paths and layers
// are unchanged. A cached plan is shared read-only: its lanes and
// tracks are never changed after it is built.
func metroLayoutPlanCached(routes []MetroLayoutRoute, track FactoryTrack) metroLayoutPlan {
	var paths []string
	for _, route := range routes {
		for _, path := range route.Paths {
			parent := "-"
			if path.Parent != nil {
				parent = strconv.Itoa(*path.Parent)
			}
			from := ValueOr(path.From, "-")
			to := ValueOr(path.To, "-")
			// The names of the routes it carries are part of its shape: a pill is drawn from them.
			var pills []string
			for _, pill := range path.Pills {
				pills = append(pills, pill.Fork+"\u0005"+pill.Route+"\u0005"+pill.Text+"\u0005"+pill.Stop)
			}
			paths = append(paths, path.Belt+"\u0001"+parent+"\u0001"+from+"\u0001"+to+"\u0001"+
				strings.Join(path.Stops, "\u0002")+"\u0001"+strconv.Itoa(path.Depth)+"\u0001"+strconv.FormatBool(path.Back)+
				"\u0001"+strings.Join(pills, "\u0004"))
		}
	}
	var layers []string
	for _, machine := range track.Machines {
		layers = append(layers, machine.ID+"\u0001"+strconv.Itoa(metroLayerOf(track, machine.ID)))
	}
	sort.Strings(layers)
	key := strings.Join(append(append(paths, "\u0003"), layers...), "\u0000")

	metroPlanCache.mu.Lock()
	plan, ok := metroPlanCache.plans[key]
	metroPlanCache.mu.Unlock()
	if ok {
		return plan
	}
	plan = newMetroLayoutPlan(routes, track)
	metroPlanCache.mu.Lock()
	defer metroPlanCache.mu.Unlock()
	if metroPlanCache.plans == nil || len(metroPlanCache.plans) >= metroLayoutPlanCacheLimit {
		metroPlanCache.plans = map[string]metroLayoutPlan{}
	}
	metroPlanCache.plans[key] = plan
	return plan
}
