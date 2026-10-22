package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// RunMetroWire is the run page's belt as a metro line (`RunMetro`), built from
// the same snapshot as `RunMapWire`, so its stations read the same stage
// statuses the machines map shows.
//
// The type is a namespace with one static function; here it is a
// package-level function.
func RunMetroWire(snapshot coordinator.CoordinatorRunViewSnapshot) core.RunMetro {
	return core.RunMetroLayoutMap(snapshot.Run, snapshot.Presentation.Stages)
}
