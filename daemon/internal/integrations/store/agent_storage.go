package store

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// agentState is what agents/<id>/state.json holds.
type agentState struct {
	Archived bool `json:"archived"`
}

// agentFolder is agents/<id>/.
func (s Storage) agentFolder(id string) string {
	return filepath.Join(s.Paths.Agents(), id)
}

// LoadAgents is every agent's latest version: the built-ins first, in the
// shipped order, then by when each was made, then by name.
// The library is one folder per agent, each version a Markdown file you can
// also edit by hand, and state.json saying whether it is archived.
func (s Storage) LoadAgents() []core.AgentRecord {
	type entry struct {
		record  core.AgentRecord
		created time.Time
	}
	var entries []entry
	for _, folder := range subfolders(s.Paths.Agents()) {
		id := filepath.Base(folder)
		all := versions(folder, "md")
		if len(all) == 0 {
			continue
		}
		agent, ok := s.Agent(id, slices.Max(all))
		if !ok {
			continue
		}
		var state agentState
		archived := s.Read(filepath.Join(folder, "state.json"), &state) == nil && state.Archived
		first := filepath.Join(folder, "v"+strconv.Itoa(slices.Min(all))+".md")
		entries = append(entries, entry{
			record:  core.AgentRecord{Agent: agent, Archived: archived},
			created: birthTime(first),
		})
	}
	rank := func(id string) int {
		for i, builtIn := range core.DefaultsBuiltInIDs {
			if builtIn == id {
				return i
			}
		}
		return len(core.DefaultsBuiltInIDs)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		ra, rb := rank(a.record.Agent.ID), rank(b.record.Agent.ID)
		if ra != rb {
			return ra < rb
		}
		if !a.created.Equal(b.created) {
			return a.created.Before(b.created)
		}
		return a.record.Agent.Name < b.record.Agent.Name
	})
	records := make([]core.AgentRecord, 0, len(entries))
	for _, e := range entries {
		records = append(records, e.record)
	}
	return records
}

// Agent is one agent version as saved, and whether it exists. Only the
// current shipped version can fall back to bundled text; never serve new
// wording under an older pin.
func (s Storage) Agent(id string, version int) (core.Agent, bool) {
	path := filepath.Join(s.agentFolder(id), "v"+strconv.Itoa(version)+".md")
	if data, err := os.ReadFile(path); err == nil && utf8.Valid(data) {
		agent, err := core.AgentFileParse(string(data), id, version)
		if err != nil {
			return core.Agent{}, false
		}
		return agent, true
	}
	if version == core.DefaultsShippedVersion(id) {
		if agent := core.DefaultsBuiltIn(id, nil); agent != nil {
			return *agent, true
		}
	}
	return core.Agent{}, false
}

// AgentRef is the agent version a reference names.
func (s Storage) AgentRef(ref core.AgentRef) (core.Agent, bool) {
	return s.Agent(ref.ID, ref.Version)
}

// AgentVersions is the versions an agent has on disk, oldest first.
func (s Storage) AgentVersions(id string) []int {
	all := versions(s.agentFolder(id), "md")
	sort.Ints(all)
	return all
}

// SaveNewAgentVersion saves agent as its next version and returns it with that
// version number. This write does not consult the write fault.
func (s Storage) SaveNewAgentVersion(agent core.Agent) (core.Agent, error) {
	folder := s.agentFolder(agent.ID)
	if err := os.MkdirAll(folder, 0o777); err != nil {
		return core.Agent{}, err
	}
	saved := agent
	saved.Version = nextVersion(versions(folder, "md"))
	path := filepath.Join(folder, "v"+strconv.Itoa(saved.Version)+".md")
	if err := WriteFileAtomic(path, []byte(core.AgentFileWrite(saved))); err != nil {
		return core.Agent{}, err
	}
	return saved, nil
}

// SetAgentArchived records whether an agent is archived.
func (s Storage) SetAgentArchived(id string, archived bool) error {
	return s.Write(agentState{Archived: archived}, filepath.Join(s.agentFolder(id), "state.json"))
}

// SeedAgents adds the shipped version of each built-in agent when the library
// lacks it or has only older versions. Saved versions and user versions at or
// above the shipped version stay untouched. Errors are ignored.
func (s Storage) SeedAgents() {
	for _, id := range core.DefaultsBuiltInIDs {
		shipped := core.DefaultsShippedVersion(id)
		highest := 0
		if all := s.AgentVersions(id); len(all) > 0 {
			highest = all[len(all)-1]
		}
		if highest >= shipped {
			continue
		}
		text, ok := core.BuiltInAgentFile(id)
		if !ok {
			continue
		}
		folder := s.agentFolder(id)
		if os.MkdirAll(folder, 0o777) != nil {
			continue
		}
		_ = WriteFileAtomic(filepath.Join(folder, "v"+strconv.Itoa(shipped)+".md"), []byte(text))
	}
}
