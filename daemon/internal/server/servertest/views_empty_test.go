// The first start's empty collections: every list is an explicit empty array, the state and settings
// carry explicit nulls, and the empty Overview and Work match their goldens.

package servertest

import (
	"bytes"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestFirstStartHasExplicitEmptyCollectionsAndNulls checks that a first start's lists are explicit empty
// arrays, its state and settings carry explicit nulls, and its views answer their empty bodies.
func TestFirstStartHasExplicitEmptyCollectionsAndNulls(t *testing.T) {
	t.Parallel()
	f := EmptyReadOnlyFixture(t)
	for path, key := range map[string]string{"/api/v1/runs": "runs", "/api/v1/belts": "belts", "/api/v1/agents": "agents"} {
		reply := getCase(f, path)
		members, err := jsonx.Members(reply.Body)
		if err != nil || string(members[key]) != "[]" {
			t.Fatalf("%s: %s = %s (%v), want an empty array", path, key, members[key], err)
		}
	}

	state, err := jsonx.Members(getCase(f, "/api/v1/state").Body)
	if err != nil || string(state["lastCheck"]) != "null" || string(state["notices"]) != "[]" {
		t.Fatalf("empty state: lastCheck %s, notices %s (%v)", state["lastCheck"], state["notices"], err)
	}
	settings, err := jsonx.Members(getCase(f, "/api/v1/settings").Body)
	if err != nil || string(settings["configurationThinking"]) != "null" || string(settings["pausedAt"]) != "null" {
		t.Fatalf("default settings: configurationThinking %s, pausedAt %s (%v), want explicit nulls",
			settings["configurationThinking"], settings["pausedAt"], err)
	}

	for path, golden := range map[string]string{
		"/api/v1/views/overview": "views-overview-empty.json",
		"/api/v1/views/work":     "views-work-empty.json",
		"/api/v1/views/insights": "views-insights-empty.json",
	} {
		reply := getCase(f, path)
		CheckGolden(t, "empty "+path, golden, reply, f.Root)
	}
	if waiting := getCase(f, "/api/v1/waiting"); string(waiting.Body) != `{"waiting":[]}` {
		t.Fatalf("empty waiting: %s", waiting.Body)
	}
	decisions := getCase(f, "/api/v1/afk/decisions")
	if !bytes.Equal(decisions.Body, mustMarshal(t, api.NewAfkDecisionsResponse(nil, nil, nil, "all"))) {
		t.Fatalf("empty decisions: %s, want the shared empty log", decisions.Body)
	}
}
