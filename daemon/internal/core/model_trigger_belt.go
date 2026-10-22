// The extension of Belt for
// its trigger entries.

package core

import "slices"
import "strconv"

// NextTriggerID is the next id that is not already on the belt. Numeric ids
// count upward and are not reused.
func (b Belt) NextTriggerID() string {
	used := map[string]struct{}{}
	highest, found := 0, false
	for _, e := range b.Triggers {
		used[e.ID] = struct{}{}
		if n, err := strconv.Atoi(e.ID); err == nil && (!found || n > highest) {
			highest, found = n, true
		}
	}
	n := highest + 1
	for {
		if _, taken := used[strconv.Itoa(n)]; !taken {
			return strconv.Itoa(n)
		}
		n++
	}
}

// AddTrigger appends an entry with trigger and no repositories.
func (b *Belt) AddTrigger(trigger Trigger) {
	entries := cloneEach(b.Triggers, TriggerEntry.Clone)
	b.Triggers = append(entries, TriggerEntry{ID: b.NextTriggerID(), Trigger: trigger.Clone(), Repos: []string{}})
}

// MoveTrigger moves the entry with id by offset places, when that stays on the
// belt.
func (b *Belt) MoveTrigger(id string, offset int) {
	index := b.triggerIndex(id)
	if index < 0 {
		return
	}
	next := index + offset
	if next < 0 || next >= len(b.Triggers) {
		return
	}
	entries := cloneEach(b.Triggers, TriggerEntry.Clone)
	entries[index], entries[next] = entries[next], entries[index]
	b.Triggers = entries
}

// RemoveTrigger removes the entries with id.
func (b *Belt) RemoveTrigger(id string) {
	kept := make([]TriggerEntry, 0, len(b.Triggers))
	for _, e := range cloneEach(b.Triggers, TriggerEntry.Clone) {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	b.Triggers = kept
	b.pruneRepositoryBases()
}

// UpdateTrigger applies change to the entry with id.
func (b *Belt) UpdateTrigger(id string, change func(*TriggerEntry)) {
	index := b.triggerIndex(id)
	if index < 0 {
		return
	}
	entries := cloneEach(b.Triggers, TriggerEntry.Clone)
	change(&entries[index])
	b.Triggers = entries
}

// triggerIndex is the position of the entry with id, or -1.
func (b Belt) triggerIndex(id string) int {
	return slices.IndexFunc(b.Triggers, func(e TriggerEntry) bool { return e.ID == id })
}

// Focusing is a copy whose first entry is entry, so a source can read one entry
// through Trigger and Repos.
func (b Belt) Focusing(entry TriggerEntry) Belt {
	belt := b
	belt.Triggers = cloneEach([]TriggerEntry{entry}, TriggerEntry.Clone)
	return belt
}

// RemoveRepositories empties every entry's repositories.
func (b *Belt) RemoveRepositories() {
	entries := cloneEach(b.Triggers, TriggerEntry.Clone)
	for i := range entries {
		entries[i].Repos = []string{}
	}
	b.Triggers = entries
}

// KeepingRepositories copies repositories onto entries with the same id and
// kind; a lone entry also keeps them when the installed belt had one entry of
// that kind. Anything else is left with no repositories, and the indexes of
// those are returned as unmatched.
func (b Belt) KeepingRepositories(existing Belt) (belt Belt, unmatched []int) {
	belt = b
	belt.Triggers = cloneEach(b.Triggers, TriggerEntry.Clone)
	unmatched = []int{}
	single := len(belt.Triggers) == 1 && len(existing.Triggers) == 1
	for index := range belt.Triggers {
		incoming := belt.Triggers[index]
		if match := existing.findEntry(incoming.ID, incoming.Trigger.Kind); match != nil {
			belt.Triggers[index].Repos = append([]string{}, match.Repos...)
		} else if single && existing.Triggers[0].Trigger.Kind == incoming.Trigger.Kind {
			belt.Triggers[index].Repos = append([]string{}, existing.Triggers[0].Repos...)
		} else {
			belt.Triggers[index].Repos = []string{}
			unmatched = append(unmatched, index)
		}
	}
	return belt, unmatched
}

// findEntry is the first entry with id and kind, or nil.
func (b Belt) findEntry(id string, kind TriggerKind) *TriggerEntry {
	for i := range b.Triggers {
		if b.Triggers[i].ID == id && b.Triggers[i].Trigger.Kind == kind {
			return &b.Triggers[i]
		}
	}
	return nil
}

// CommonProvides is what every entry provides; an input one kind lacks is not
// available.
func (b Belt) CommonProvides() map[Input]struct{} {
	if len(b.Triggers) == 0 {
		return map[Input]struct{}{}
	}
	common := b.Triggers[0].Trigger.Kind.Provides()
	for _, e := range b.Triggers[1:] {
		common = intersectInputs(common, e.Trigger.Kind.Provides())
	}
	return common
}

// intersectInputs is the inputs in both a and b, as a new set.
func intersectInputs(a, b map[Input]struct{}) map[Input]struct{} {
	out := map[Input]struct{}{}
	for in := range a {
		if _, ok := b[in]; ok {
			out[in] = struct{}{}
		}
	}
	return out
}

// MenuInputs is what a new stage can count on, including a pull request an
// agent can open when every entry starts from a base branch.
func (b Belt) MenuInputs() map[Input]struct{} {
	if len(b.Triggers) == 0 {
		return map[Input]struct{}{}
	}
	sets := make([]map[Input]struct{}, len(b.Triggers))
	for i, e := range b.Triggers {
		available := e.Trigger.Kind.Provides()
		if _, ok := available[InputBaseBranch]; ok {
			available[InputBranch] = struct{}{}
			available[InputPullRequest] = struct{}{}
		}
		sets[i] = available
	}
	menu := sets[0]
	for _, set := range sets[1:] {
		menu = intersectInputs(menu, set)
	}
	return menu
}

// StageKind is the kind new stages are shaped for: a review when not every entry
// can start a branch of its own, and an issue belt otherwise.
func (b Belt) StageKind() TriggerKind {
	if _, ok := b.MenuInputs()[InputBaseBranch]; ok {
		return TriggerKindIssues
	}
	return TriggerKindReviews
}
