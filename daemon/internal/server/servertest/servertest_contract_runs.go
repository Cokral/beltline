// Contract cases for the run endpoints. Each case runs on a fresh contract fixture; the goldens are
// recorded by the first accepted request of the test that owns them.

package servertest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// runCases are the registry cases the runs area serves, in registry order.
var runCases = []string{
	"runs", "run", "runAttemptOutput", "runAttemptArtifacts",
	"runDecide", "runResume", "runAnswer", "runAbort", "runDrop", "runPark", "runUnpark", "runPause",
	"runResumeRun", "runRetryContinuation", "runStartNow", "runContinueSession", "runAdjustStage",
	"runAdjustSwitch", "runAfk", "runErase", "runOverruleAfk", "tasks",
}

func init() {
	registerArea(runCases, exerciseRuns)
}

// exerciseRuns serves one runs case on a fresh fixture and checks its golden.
func exerciseRuns(_ *Fixture, t testing.TB, contractCase string) bool {
	scenario, ok := runScenarios[contractCase]
	if !ok {
		return false
	}
	g, reply := scenario(t)
	CheckGolden(t, contractCase, caseEndpoint(contractCase).GoldenName, reply, g.Root)
	return true
}

// runScenarios is each runs case's first accepted request, on its own fixture: the fixture the
// request is served on, and the reply.
var runScenarios = map[string]func(t testing.TB) (*Fixture, Reply){
	"runs":                 scenarioRuns,
	"run":                  scenarioRun,
	"runAttemptOutput":     scenarioAttemptOutput,
	"runAttemptArtifacts":  scenarioAttemptArtifacts,
	"runDecide":            scenarioDecide,
	"runResume":            scenarioResume,
	"runAnswer":            scenarioAnswer,
	"runAbort":             scenarioAbort,
	"runDrop":              scenarioDrop,
	"runPark":              scenarioPark,
	"runUnpark":            scenarioUnpark,
	"runPause":             scenarioPause,
	"runResumeRun":         scenarioResumeRun,
	"runRetryContinuation": scenarioRetryContinuation,
	"runStartNow":          scenarioStartNow,
	"runContinueSession":   scenarioContinueSession,
	"runAdjustStage":       scenarioAdjustStage,
	"runAdjustSwitch":      scenarioAdjustSwitch,
	"runAfk":               scenarioAfk,
	"runErase":             scenarioErase,
	"runOverruleAfk":       scenarioOverruleAfk,
	"tasks":                scenarioTasks,
}

// caseEndpoint is the registry entry of a contract case; it fails the test binary for an unknown case.
func caseEndpoint(contractCase string) routes.Endpoint {
	for _, endpoint := range routes.Endpoints {
		if endpoint.Case == contractCase {
			return endpoint
		}
	}
	panic("servertest: no registry endpoint for case " + contractCase)
}

// contractSegment is the encodeSegment of the contract: a path segment with every byte outside the
// unreserved set percent-encoded in upper-case hex.
func contractSegment(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if isUnreservedByte(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// isUnreservedByte reports whether c is in the unreserved set A-Z a-z 0-9 - . _ ~.
func isUnreservedByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

// casePath is the registry path of a case with its :id and :attempt placeholders filled in
// (already encoded); an empty value leaves the placeholder out of the substitution.
func casePath(contractCase string, id, attempt string) string {
	path := caseEndpoint(contractCase).Path
	if id != "" {
		path = strings.ReplaceAll(path, ":id", contractSegment(id))
	}
	if attempt != "" {
		path = strings.ReplaceAll(path, ":attempt", contractSegment(attempt))
	}
	return path
}

// mustJSON is the wire encoding of v (jsonx), failing the test on an encoding error.
func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	data, err := routes.JSONEncode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

// postRunCase posts body (nil for the empty object `{}` the commands take by default) to a run
// command's path, with the contract bearer.
func postRunCase(f *Fixture, path string, body []byte) Reply {
	if body == nil {
		body = []byte("{}")
	}
	return f.Do(Request{Method: "POST", Path: path, Body: body, Token: ContractToken})
}

// getCase gets path with the contract bearer.
func getCase(f *Fixture, path string) Reply {
	return f.Do(Request{Method: "GET", Path: path, Token: ContractToken})
}

// findRun is the first run, in the coordinator's order, that match accepts.
func findRun(t testing.TB, c *coordinator.Coordinator, description string, match func(core.Run) bool) core.Run {
	t.Helper()
	for _, run := range c.ReadRuns() {
		if match(run) {
			return run
		}
	}
	t.Fatalf("the contract demo has no run %s", description)
	return core.Run{}
}

// stageKind is the kind of a run's current stage, empty when the run has none.
func stageKind(run core.Run) core.StageKind {
	if stage := run.Stage(); stage != nil {
		return stage.Kind()
	}
	return ""
}

// canAbortRun reports whether one of the run's legs can be aborted.
func canAbortRun(run core.Run) bool {
	return slices.ContainsFunc(run.Legs, func(leg core.Leg) bool { return leg.CanAbort(run) })
}

// hasSessionAttempt reports whether run has an attempt with a session.
func hasSessionAttempt(run core.Run) bool {
	return slices.ContainsFunc(run.Attempts, func(attempt core.Attempt) bool {
		return attempt.Session != nil
	})
}

// pausedAt reports whether a run's pause is in the paused phase.
func pausedAt(run core.Run) bool {
	return run.Pause != nil && run.Pause.Phase == core.RunPausePhasePaused
}

// runIDs are the ids of runs, in order.
func runIDs(runs []core.Run) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return ids
}
