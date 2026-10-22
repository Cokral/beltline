// The factory track's reads: the machine, belt and layer lookups, a stretch's bend,
// the runs at a machine and the short names a run's chip shows. The types are in
// present_factory_track.go; the build is in present_factory_track_build.go.

package core

import (
	"slices"
	"strings"
	"unicode"
)

// factoryTrackGenericWords are the words a short belt name does not take as its second word.
var factoryTrackGenericWords = map[string]struct{}{
	"belt": {}, "fixes": {}, "tasks": {}, "issues": {}, "requests": {},
	"to": {}, "of": {}, "the": {}, "for": {}, "and": {},
}

// Machine is the machine with id, or nil.
func (f FactoryTrack) Machine(id string) *FactoryTrackMachine {
	for i := range f.Machines {
		if f.Machines[i].ID == id {
			machine := f.Machines[i]
			return &machine
		}
	}
	return nil
}

// Belt is the belt with id, or nil.
func (f FactoryTrack) Belt(id string) *FactoryTrackBelt {
	for i := range f.Belts {
		if f.Belts[i].ID == id {
			belt := f.Belts[i]
			return &belt
		}
	}
	return nil
}

// MachineOf is the machine a belt's stage is part of, or nil.
func (f FactoryTrack) MachineOf(stage, belt string) *FactoryTrackMachine {
	for i := range f.Machines {
		for _, member := range f.Machines[i].Members {
			if member.StageID == stage && member.BeltID == belt {
				machine := f.Machines[i]
				return &machine
			}
		}
	}
	return nil
}

// LayerOf is the layer a node sits in, counted from the entrances; nil when it is not placed.
func (f FactoryTrack) LayerOf(node FactoryTrackNode) *int {
	for i, layer := range f.Layers {
		if slices.Contains(layer, node) {
			index := i
			return &index
		}
	}
	return nil
}

// FactoryTrackBend is where a stretch changes rows.
type FactoryTrackBend string

const (
	// FactoryTrackBendOut is a change right after the stretch's start, where the track splits.
	FactoryTrackBendOut FactoryTrackBend = "out"
	// FactoryTrackBendIn is a change right before the stretch's end, where tracks join.
	FactoryTrackBendIn FactoryTrackBend = "in"
	// FactoryTrackBendMiddle is a change halfway along the stretch.
	FactoryTrackBendMiddle FactoryTrackBend = "middle"
)

// Bend is where a stretch changes rows: right after its start when the track splits there,
// right before its end when tracks join there, else halfway.
func (f FactoryTrack) Bend(stretch FactoryTrackTrack) FactoryTrackBend {
	splitCount, joinCount := 0, 0
	for _, track := range f.Tracks {
		if track.From == stretch.From {
			splitCount++
		}
		if track.To == stretch.To {
			joinCount++
		}
	}
	splits := splitCount > 1
	joins := joinCount > 1
	switch {
	case splits == joins:
		return FactoryTrackBendMiddle
	case splits:
		return FactoryTrackBendOut
	default:
		return FactoryTrackBendIn
	}
}

// RunsAt is the active runs at a machine.
func (f FactoryTrack) RunsAt(machine FactoryTrackMachine, runs []Run) []Run {
	out := []Run{}
	for i := range runs {
		if runs[i].State.IsActive() && machine.Contains(runs[i]) {
			out = append(out, runs[i])
		}
	}
	return out
}

// factoryTrackKey is a stage or machine name as a key: trimmed of whitespace and lower-cased.
func factoryTrackKey(name string) string {
	return LowercasedFull(strings.TrimSpace(name))
}

// FactoryTrackShortName is a belt's name, short enough for a run's chip: its first word, or two
// when the first is short, leaving out words like "fixes".
func FactoryTrackShortName(beltName string) string {
	words := strings.FieldsFunc(beltName, unicode.IsSpace)
	if len(words) == 0 {
		return ""
	}
	first := words[0]
	if markdownCharacterCount(first) < factoryTrackShortNameMin && len(words) > 1 {
		if _, generic := factoryTrackGenericWords[LowercasedFull(words[1])]; !generic {
			return PrefixCharacters(first+" "+words[1], factoryTrackShortNameLimit)
		}
	}
	return PrefixCharacters(first, factoryTrackShortNameLimit)
}

// PrefixCharacters is the first n characters of s,
// counting characters, not runes.
func PrefixCharacters(s string, n int) string {
	chars := Characters(s)
	if len(chars) > n {
		chars = chars[:n]
	}
	return strings.Join(chars, "")
}
