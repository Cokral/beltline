package servertest

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// libraryListing is the part of the belts and agents lists these tests read: each row's id, name and version.
type libraryListing struct {
	Belts []struct {
		Belt struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version int    `json:"version"`
		} `json:"draft"`
	} `json:"belts"`
	Agents []struct {
		Agent struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version int    `json:"version"`
		} `json:"agent"`
	} `json:"agents"`
}

// libraryDetail is the part of an agent's detail these tests read.
type libraryDetail struct {
	Agent struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version int    `json:"version"`
	} `json:"agent"`
}

// TestLibraryRoutesShowConfiguratorEditsAndRemovalsWithoutATick: the belts and agents routes read the data folder
// as it is now. A belt or agent written or removed there shows in the next GET, with no tick between.
func TestLibraryRoutesShowConfiguratorEditsAndRemovalsWithoutATick(t *testing.T) {
	t.Parallel()
	g := Start(t, Options{})
	storage := store.NewStorage(store.Paths{Root: g.Root})

	belt := g.Coordinator.ReadBelts()[0].Belt
	belt.Name = "Renamed on disk"
	if _, err := storage.SaveNewVersion(belt); err != nil {
		t.Fatalf("write belt version: %v", err)
	}
	agent := g.Coordinator.ReadAgent("triage").Agent
	agent.Name = "Triage, renamed on disk"
	if _, err := storage.SaveNewAgentVersion(agent); err != nil {
		t.Fatalf("write agent version: %v", err)
	}

	var listed libraryListing
	reply := getCase(g, "/api/v1/belts")
	requireStatus(t, reply, http.StatusOK)
	decodePartial(t, reply, &listed)
	if !hasListedBelt(listed, belt.ID, belt.Name, belt.Version+1) {
		t.Fatalf("GET /api/v1/belts does not show the belt's new version: %+v", listed.Belts)
	}
	reply = getCase(g, "/api/v1/agents")
	requireStatus(t, reply, http.StatusOK)
	decodePartial(t, reply, &listed)
	if !hasListedAgent(listed, "triage", "Triage, renamed on disk", agent.Version+1) {
		t.Fatalf("GET /api/v1/agents does not show the agent's new version: %+v", listed.Agents)
	}
	reply = getCase(g, "/api/v1/agents/triage")
	requireStatus(t, reply, http.StatusOK)
	var detail libraryDetail
	decodePartial(t, reply, &detail)
	if detail.Agent.Version != agent.Version+1 || detail.Agent.Name != "Triage, renamed on disk" {
		t.Fatalf("GET /api/v1/agents/triage shows version %d %q, want the version the list shows", detail.Agent.Version, detail.Agent.Name)
	}

	created := core.NewAgent(core.AgentArgs{ID: "fresh-agent", Name: "Fresh agent"})
	if _, err := storage.SaveNewAgentVersion(created); err != nil {
		t.Fatalf("write new agent: %v", err)
	}
	reply = getCase(g, "/api/v1/agents/fresh-agent")
	requireStatus(t, reply, http.StatusOK)

	if err := os.RemoveAll(filepath.Join(g.Root, "agents", "fresh-agent")); err != nil {
		t.Fatalf("remove agent: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(g.Root, "belts", belt.ID)); err != nil {
		t.Fatalf("remove belt: %v", err)
	}
	reply = getCase(g, "/api/v1/agents/fresh-agent")
	requireStatus(t, reply, http.StatusNotFound)
	reply = getCase(g, "/api/v1/agents")
	requireStatus(t, reply, http.StatusOK)
	decodePartial(t, reply, &listed)
	if hasListedAgent(listed, "fresh-agent", "", 0) {
		t.Fatalf("GET /api/v1/agents still lists the removed agent")
	}
	reply = getCase(g, "/api/v1/belts")
	requireStatus(t, reply, http.StatusOK)
	decodePartial(t, reply, &listed)
	if hasListedBelt(listed, belt.ID, "", 0) {
		t.Fatalf("GET /api/v1/belts still lists the removed belt")
	}
}

// hasListedBelt reports whether the list has the belt id with name and version, where a zero name or version matches
// only the absence of the id.
func hasListedBelt(listed libraryListing, id, name string, version int) bool {
	for _, row := range listed.Belts {
		if row.Belt.ID == id {
			return name == "" || (row.Belt.Name == name && row.Belt.Version == version)
		}
	}
	return false
}

// hasListedAgent reports whether the list has the agent id with name and version, where a zero name or version matches
// only the absence of the id.
func hasListedAgent(listed libraryListing, id, name string, version int) bool {
	for _, row := range listed.Agents {
		if row.Agent.ID == id {
			return name == "" || (row.Agent.Name == name && row.Agent.Version == version)
		}
	}
	return false
}

// decodePartial decodes the fields a test names from a reply, ignoring the rest of the response.
func decodePartial(t testing.TB, reply Reply, v any) {
	t.Helper()
	if err := json.Unmarshal(reply.Body, v); err != nil {
		t.Fatalf("decode %s: %v", reply.Body, err)
	}
}
