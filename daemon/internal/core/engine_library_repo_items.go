// What a shared library repository holds, and where each of its belts and agents
// stands against this library.

package core

import (
	"fmt"
	"maps"
	"slices"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// LibraryItemState is where a repository's belt or agent stands against the library.
type LibraryItemState string

const (
	// LibraryItemAvailable is an item the library does not have.
	LibraryItemAvailable LibraryItemState = "available"
	// LibraryItemInstalled is the latest version, from this repository, already in the library.
	LibraryItemInstalled LibraryItemState = "installed"
	// LibraryItemUpdate is an item from this repository with a newer version in it.
	LibraryItemUpdate LibraryItemState = "update"
	// LibraryItemConflict is an item the library cannot take without losing a change.
	LibraryItemConflict LibraryItemState = "conflict"
)

// LibraryItem is one belt or agent a shared repository holds.
type LibraryItem struct {
	Kind         string           `json:"kind"`
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Version      int              `json:"version"`
	LocalVersion *int             `json:"localVersion"`
	State        LibraryItemState `json:"state"`
	Reason       *string          `json:"reason"`
	Actions      []LibraryAction  `json:"actions"`
}

// LibraryRepoItems says what a repository holds and where each item stands here.
// repoFiles are the repository's library files under its path, localFiles this
// data folder's belts and agents, and origin the repository's own. The items come
// out sorted, belts first then agents, by id. problems are what is wrong with the
// repository's files; a file that does not decode is left out of the items.
func LibraryRepoItems(origin LibraryOrigin, repoFiles, localFiles map[string][]byte) ([]LibraryItem, []string) {
	repoBelts, repoAgents, problems := libraryCheckRead(repoFiles)
	localBelts, localAgents, _ := libraryCheckRead(localFiles)
	var items []LibraryItem
	for _, id := range slices.Sorted(maps.Keys(repoBelts)) {
		latest := libraryNewest(repoBelts[id])
		belt := repoBelts[id][latest]
		state, reason := libraryBeltStanding(origin, belt, latest, localBelts, localAgents, repoAgents, repoBelts[id])
		items = append(items, libraryItem("belt", belt.ID, belt.Name, latest, localBelts[id], state, reason))
	}
	for _, id := range slices.Sorted(maps.Keys(repoAgents)) {
		latest := libraryNewest(repoAgents[id])
		agent := repoAgents[id][latest]
		state, reason := libraryAgentStanding(origin, agent, latest, localAgents[id], repoAgents[id])
		items = append(items, libraryItem("agent", agent.ID, agent.Name, latest, localAgents[id], state, reason))
	}
	return items, problems
}

// libraryBeltStanding is where a belt stands. The agent versions it pins must not
// have changed here, and then its own versions decide. repoVersions are the
// repository's versions of the belt, by number.
func libraryBeltStanding(origin LibraryOrigin, belt Belt, latest int, localBelts libraryBelts, localAgents libraryAgents, repoAgents libraryAgents, repoVersions map[int]Belt) (LibraryItemState, string) {
	for _, stage := range belt.Stages {
		if stage.Agent == nil {
			continue
		}
		ref := *stage.Agent
		repoAgent, inRepo := repoAgents[ref.ID][ref.Version]
		localAgent, inLibrary := localAgents[ref.ID][ref.Version]
		if inRepo && inLibrary && libraryAgentText(repoAgent) != libraryAgentText(localAgent) {
			return LibraryItemConflict, libraryChangedReason(ref.ID, ref.Version)
		}
	}
	versions := localBelts[belt.ID]
	if len(versions) == 0 {
		return LibraryItemAvailable, ""
	}
	newest := libraryNewest(versions)
	localBelt := versions[newest]
	fromOrigin := localBelt.Origin != nil && *localBelt.Origin == origin
	// Without this origin, the version still came from the repository when the repository holds the
	// same content under its number: an item installed before origins were kept, or one shared from here.
	held, inRepo := repoVersions[newest]
	sameHeld := inRepo && libraryBeltShared(localBelt) == libraryBeltShared(held)
	if !fromOrigin && !sameHeld {
		return LibraryItemConflict, fmt.Sprintf("%s is already in your library and didn't come from this repository.", belt.ID)
	}
	switch {
	case newest < latest:
		return LibraryItemUpdate, ""
	case newest == latest && fromOrigin && libraryBeltText(localBelt) != libraryBeltText(belt):
		return LibraryItemConflict, libraryChangedReason(belt.ID, latest)
	}
	return LibraryItemInstalled, ""
}

// libraryAgentStanding is where an agent stands, from its own versions and the repository's copies of
// them. A newest version with no origin from this repository counts as from it when the content matches
// the repository's copy under the same number, as libraryBeltStanding does.
func libraryAgentStanding(origin LibraryOrigin, agent Agent, latest int, versions map[int]Agent, repoVersions map[int]Agent) (LibraryItemState, string) {
	if len(versions) == 0 {
		return LibraryItemAvailable, ""
	}
	newest := libraryNewest(versions)
	localAgent := versions[newest]
	fromOrigin := localAgent.Origin != nil && *localAgent.Origin == origin
	held, inRepo := repoVersions[newest]
	sameHeld := inRepo && libraryAgentText(localAgent) == libraryAgentText(held)
	if !fromOrigin && !sameHeld {
		return LibraryItemConflict, fmt.Sprintf("%s is already in your library and didn't come from this repository.", agent.ID)
	}
	switch {
	case newest < latest:
		return LibraryItemUpdate, ""
	case newest == latest && fromOrigin && libraryAgentText(localAgent) != libraryAgentText(agent):
		return LibraryItemConflict, libraryChangedReason(agent.ID, latest)
	}
	return LibraryItemInstalled, ""
}

// libraryItem is one item with its state, the local version it has and its install action.
func libraryItem[T any](kind, id, name string, version int, local map[int]T, state LibraryItemState, reason string) LibraryItem {
	item := LibraryItem{Kind: kind, ID: id, Name: name, Version: version, State: state}
	if len(local) > 0 {
		newest := libraryNewest(local)
		item.LocalVersion = &newest
	}
	action := libraryAction("install", true, "")
	switch state {
	case LibraryItemInstalled:
		action = libraryAction("install", false, fmt.Sprintf("%s v%d is already in your library.", id, version))
	case LibraryItemConflict:
		action = libraryAction("install", false, reason)
	}
	if reason != "" {
		item.Reason = &reason
	}
	item.Actions = []LibraryAction{action}
	return item
}

// libraryChangedReason says a local version was changed and must be kept under a new id.
func libraryChangedReason(id string, version int) string {
	return fmt.Sprintf("You changed %s v%d locally; save it under a new id before updating.", id, version)
}

// libraryNewest is the highest version number in versions.
func libraryNewest[T any](versions map[int]T) int {
	newest := 0
	for v := range versions {
		newest = max(newest, v)
	}
	return newest
}

// libraryBeltText is a belt as its file reads, with its origin left out, so two
// belts with the same text have the same content.
func libraryBeltText(belt Belt) string {
	belt.Origin = nil
	data, _ := jsonx.MarshalStorage(belt)
	return string(data)
}

// libraryBeltShared is a belt as a share writes it: without its origin, and without the repositories,
// base branches and AFK mode that belong to one library (LibraryShareFiles strips the same).
func libraryBeltShared(belt Belt) string {
	belt.Origin = nil
	belt.Base = ""
	belt.RepositoryBases = nil
	belt.Afk = AfkModeGlobal
	belt.Triggers = slices.Clone(belt.Triggers)
	for index := range belt.Triggers {
		belt.Triggers[index].Repos = nil
	}
	return libraryBeltText(belt)
}

// libraryAgentText is an agent as its file reads, with its origin left out.
func libraryAgentText(agent Agent) string {
	agent.Origin = nil
	return AgentFileWrite(agent)
}
