// The run label text and the station names of
// a spot.

package core

import (
	"strconv"
	"strings"
)

// metroMapLabelText is the text a run label shows for runs, and the look that makes it urgent, if any.
func metroMapLabelText(runs []Run) (string, *MetroLayoutLook) {
	var refs []string
	for i, run := range runs {
		if i == 2 {
			break
		}
		refs = append(refs, run.Subject.Ref())
	}
	extra := ""
	if len(runs) > 2 {
		extra = metroMapLabelOverflow + strconv.Itoa(len(runs)-2)
	}
	var urgent *MetroLayoutLook
	hasHeld, hasWaiting := false, false
	for _, run := range runs {
		if run.State == RunStateHeld {
			hasHeld = true
		}
		if run.State == RunStateWaiting {
			hasWaiting = true
		}
	}
	if hasHeld {
		held := MetroLayoutLookHeld
		urgent = &held
	} else if hasWaiting {
		waiting := MetroLayoutLookWaiting
		urgent = &waiting
	}
	return strings.Join(refs, " ") + " " + metroLayoutStateWord(runs) + extra, urgent
}

// spotLines is the station name of a spot, on one line or two.
func (m MetroLayoutMap) spotLines(spot int) []string {
	id := m.Spots[spot].ID
	name := id
	if machine := m.Track.Machine(id); machine != nil {
		name = machine.Name
	}
	return metroLayoutWrap(name, 15)
}
