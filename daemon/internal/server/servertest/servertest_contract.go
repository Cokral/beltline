// The contract cases for the endpoints this harness serves: each area registers the cases it runs.

package servertest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// Exercise serves the request of one exercised contract case through the fixture's router and
// checks the response against its golden. It fails for a case this harness does not serve.
func (f *Fixture) Exercise(t testing.TB, contractCase string) {
	t.Helper()
	switch contractCase {
	case "version":
		reply := f.Do(Request{Method: "GET", Path: "/api/v1/version", Token: ContractToken})
		CheckGolden(t, "version", "version.json", reply, f.Root)
	case "auth":
		reply := f.Do(Request{Method: "GET", Path: "/auth?token=" + ContractToken})
		if reply.Status != 302 {
			t.Fatalf("auth: expected HTTP 302, got %d", reply.Status)
		}
		if got := reply.Header.Get("Location"); got != "/" {
			t.Fatalf("auth: Location %q, want /", got)
		}
		if got := reply.Header.Get("Cache-Control"); got != "no-store" {
			t.Fatalf("auth: Cache-Control %q, want no-store", got)
		}
		if got := reply.Header.Get("Set-Cookie"); !strings.Contains(got, "Path=/api") {
			t.Fatalf("auth: Set-Cookie %q does not scope the cookie to /api", got)
		}
		CheckBody(t, "auth", "auth-success-body", reply.Body, f.Root)
	case "artifactLink":
		reply := f.IssueArtifactLink(t, f.ArtifactName(), ContractToken, "", "")
		CheckGolden(t, "artifactLink", "artifact-link.json", reply, f.Root)
	case "artifact":
		link := f.IssueArtifactLink(t, f.ArtifactName(), ContractToken, "", "")
		var issued struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(link.Body, &issued); err != nil {
			t.Fatalf("artifact link: %v", err)
		}
		reply := f.Do(Request{Method: "GET", Path: issued.URL})
		if reply.Status != 200 {
			t.Fatalf("artifact: expected HTTP 200, got %d", reply.Status)
		}
		if got := reply.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("artifact: Content-Type %q", got)
		}
		want, err := os.ReadFile(filepath.Join(testkit.Testdata(t, "golden"), "artifact.html"))
		if err != nil {
			t.Fatalf("artifact golden: %v", err)
		}
		if string(reply.Body) != string(want) {
			t.Fatalf("artifact body %q, want the golden %q", reply.Body, want)
		}
	default:
		for _, area := range areaExercisers {
			if area.exercise(f, t, contractCase) {
				return
			}
		}
		t.Fatalf("servertest: case %q is not exercised by this harness", contractCase)
	}
}

// areaExerciser serves the contract cases of one route area: exercise reports whether it
// knows the case (and has checked it). A route file adds its area in a file of its own,
// servertest_contract_<area>.go, with an init that calls registerArea, so the units never
// edit the same switch.
type areaExerciser struct {
	cases    []string
	exercise func(f *Fixture, t testing.TB, contractCase string) bool
}

// exercisedCases are the contract cases this harness serves and checks against their goldens: these
// four, and each area's cases as it registers them (registerArea).
var exercisedCases = []string{"auth", "version", "artifact", "artifactLink"}

// areaExercisers is every area registered by registerArea.
var areaExercisers []areaExerciser

// registerArea adds an area's cases to the exercised set and its exerciser to Exercise.
func registerArea(cases []string, exercise func(f *Fixture, t testing.TB, contractCase string) bool) {
	areaExercisers = append(areaExercisers, areaExerciser{cases: cases, exercise: exercise})
	exercisedCases = append(exercisedCases, cases...)
}

// ArtifactName is the artifact file the action run carries.
func (f *Fixture) ArtifactName() string { return ContractArtifactName }

// IssueArtifactLink posts an artifact link request for the action run's attempt and name, with the
// given bearer token (none when empty) and the run and attempt overrides (the action ones when empty).
// It returns the reply.
func (f *Fixture) IssueArtifactLink(t testing.TB, name, token, runID, attemptID string) Reply {
	t.Helper()
	if runID == "" {
		runID = f.ActionRunID
	}
	if attemptID == "" {
		attemptID = f.ActionAttemptID
	}
	body, err := json.Marshal(map[string]string{"runId": runID, "attemptId": attemptID, "name": name})
	if err != nil {
		t.Fatalf("artifact link body: %v", err)
	}
	return f.Do(Request{Method: "POST", Path: "/api/v1/artifacts/link", Body: body, Token: token})
}
