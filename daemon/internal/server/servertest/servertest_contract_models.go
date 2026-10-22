// Contract cases for the model endpoints: the models read and the model commands.

package servertest

import (
	"testing"
)

// The models area's contract cases.
var modelsCases = []string{"models", "modelRefresh", "modelAllow"}

func init() {
	registerArea(modelsCases, exerciseModels)
}

// exerciseModels serves one models case and checks its golden; it reports whether the case is the models area's.
// The refresh and allow commands change the catalog and settings, so each runs on a fresh fixture.
func exerciseModels(f *Fixture, t testing.TB, contractCase string) bool {
	switch contractCase {
	case "models":
		reply := f.Do(Request{Method: "GET", Path: "/api/v1/models/pi", Token: ContractToken})
		CheckGolden(t, "models", "models.json", reply, f.Root)
	case "modelRefresh":
		g := Start(t, Options{})
		reply := g.Do(Request{Method: "POST", Path: "/api/v1/models/pi/refresh", Token: ContractToken})
		CheckGolden(t, "modelRefresh", "command-model-refresh.json", reply, g.Root)
	case "modelAllow":
		g := Start(t, Options{})
		reply := g.Do(Request{Method: "POST", Path: "/api/v1/models/pi/allow", Token: ContractToken,
			Body: []byte(`{"model":"fixture-model","allowed":false}`)})
		CheckGolden(t, "modelAllow", "command-model-allow.json", reply, g.Root)
	default:
		return false
	}
	return true
}
