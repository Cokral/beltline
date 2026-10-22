// The contract case for the tools endpoint.

package servertest

import (
	"testing"
)

// The tools area's contract cases.
var toolsCases = []string{"tools"}

func init() {
	registerArea(toolsCases, exerciseTools)
}

// exerciseTools serves the tools case and checks its golden; it reports whether the case is the tools area's.
func exerciseTools(f *Fixture, t testing.TB, contractCase string) bool {
	if contractCase != "tools" {
		return false
	}
	reply := f.Do(Request{Method: "GET", Path: "/api/v1/tools", Token: ContractToken})
	CheckGolden(t, "tools", "tools.json", reply, f.Root)
	return true
}
