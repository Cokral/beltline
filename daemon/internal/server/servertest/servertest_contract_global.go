// Contract cases for the global commands (pause, AFK, check-now and quit), each with the state its
// request needs.

package servertest

import (
	"context"
	"testing"
)

// The global area's contract cases.
var globalCases = []string{"pause", "afk", "checkNow", "quit"}

func init() {
	registerArea(globalCases, exerciseGlobal)
}

// exerciseGlobal serves one global case and checks its golden; it reports whether the case is the global area's.
func exerciseGlobal(f *Fixture, t testing.TB, contractCase string) bool {
	switch contractCase {
	case "pause":
		g := Start(t, Options{})
		reply := g.Do(Request{Method: "POST", Path: "/api/v1/pause", Token: ContractToken, Body: []byte(`{"paused":true}`)})
		CheckGolden(t, "pause", "command-pause.json", reply, g.Root)
	case "afk":
		// The test pauses first, so the kicked tick is a no-op and the response is deterministic.
		g := Start(t, Options{})
		g.Coordinator.SetGloballyPaused(context.Background(), true)
		reply := g.Do(Request{Method: "POST", Path: "/api/v1/afk", Token: ContractToken, Body: []byte(`{"enabled":true}`)})
		CheckGolden(t, "afk", "command-afk.json", reply, g.Root)
	case "checkNow":
		// The test's order: pause, a paused check (which changes nothing), resume, tick, then the check.
		g := Start(t, Options{})
		g.Coordinator.SetGloballyPaused(context.Background(), true)
		g.Do(Request{Method: "POST", Path: "/api/v1/check-now", Token: ContractToken})
		g.Coordinator.SetGloballyPaused(context.Background(), false)
		g.Coordinator.Tick(context.Background())
		g.Coordinator.Idle()
		reply := g.Do(Request{Method: "POST", Path: "/api/v1/check-now", Token: ContractToken})
		CheckGolden(t, "checkNow", "command-check-now.json", reply, g.Root)
	case "quit":
		g := Start(t, Options{})
		reply := g.Do(Request{Method: "POST", Path: "/api/v1/quit", Token: ContractToken})
		CheckGolden(t, "quit", "command-quit.json", reply, g.Root)
	default:
		return false
	}
	return true
}
