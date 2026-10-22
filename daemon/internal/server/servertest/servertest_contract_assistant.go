// Contract cases for the assistant endpoints: reading the configuration, starting and stopping. Each
// case runs on a fresh fixture; start and stop replay the configuration first.

package servertest

import (
	"slices"
	"testing"
)

// assistantContractCases are the assistant area's contract cases, in registry order.
var assistantContractCases = []string{"assistant", "assistantConfiguration", "assistantStart", "assistantStop"}

// assistantConfigurationBody is the configuration the test saves before starting the assistant.
const assistantConfigurationBody = `{"backend":"pi","model":"fixture-model","thinking":"high"}`

// assistantStartBody is the request the test starts the assistant with.
const assistantStartBody = `{"request":"  Add a belt  "}`

func init() {
	registerArea(assistantContractCases, exerciseAssistantContract)
}

// exerciseAssistantContract serves one assistant case on a fresh fixture and checks it against its golden.
// It reports false for a case of another area.
func exerciseAssistantContract(f *Fixture, t testing.TB, contractCase string) bool {
	if !slices.Contains(assistantContractCases, contractCase) {
		return false
	}
	switch contractCase {
	case "assistant":
		g := Start(t, Options{})
		reply := g.Do(Request{Method: "GET", Path: "/api/v1/assistant", Token: ContractToken})
		CheckGolden(t, contractCase, contractEndpoint(t, contractCase).GoldenName, reply, g.Root)
		checkAssistantShape(t, reply.Body)
	case "assistantConfiguration":
		g := Start(t, Options{})
		reply := g.Do(Request{Method: "POST", Path: contractEndpoint(t, contractCase).Path,
			Body: []byte(assistantConfigurationBody), Token: ContractToken})
		CheckGolden(t, contractCase, contractEndpoint(t, contractCase).GoldenName, reply, g.Root)
	case "assistantStart", "assistantStop":
		g := Start(t, Options{})
		configured := g.Do(Request{Method: "POST", Path: "/api/v1/assistant/configuration",
			Body: []byte(assistantConfigurationBody), Token: ContractToken})
		if configured.Status != 200 {
			t.Fatalf("%s: configuration answered %d", contractCase, configured.Status)
		}
		started := g.Do(Request{Method: "POST", Path: "/api/v1/assistant/start", Body: []byte(assistantStartBody), Token: ContractToken})
		if started.Status != 200 {
			t.Fatalf("%s: start answered %d", contractCase, started.Status)
		}
		reply := started
		if contractCase == "assistantStop" {
			reply = g.Do(Request{Method: "POST", Path: "/api/v1/assistant/stop", Token: ContractToken})
		}
		CheckGolden(t, contractCase, contractEndpoint(t, contractCase).GoldenName, reply, g.Root)
	}
	return true
}

// checkAssistantShape checks the `.assistant` assertions: a fresh assistant is neither
// running nor busy, has no command or check command, and has no problems. It reads the tree, because a
// fresh assistant writes its absent command as null.
func checkAssistantShape(t testing.TB, body []byte) {
	t.Helper()
	tree := contractTree(t, body)
	if contractMember(t, tree, "running") != false || contractMember(t, tree, "busy") != false {
		t.Fatal("assistant: a fresh assistant is running or busy")
	}
	if contractMember(t, tree, "command") != nil || contractMember(t, tree, "checkCommand") != nil {
		t.Fatal("assistant: a fresh assistant has a command")
	}
	if problems, _ := contractMember(t, tree, "problems").([]any); len(problems) != 0 {
		t.Fatal("assistant: a fresh assistant has problems")
	}
}
