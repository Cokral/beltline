// The Belts section,
// BeltRecord and BeltState.

package store

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// BeltRecord is a belt's latest version and whether it is paused.
type BeltRecord struct {
	Belt     core.Belt
	Paused   bool
	Archived bool
	// Created is when its first version was saved; belts are listed in this order.
	Created time.Time
	// Fired is when each schedule entry last fired, or skipped a slot, by entry
	// id. An entry is absent until that schedule is first checked.
	Fired map[string]time.Time
	// WatchOutcomes records whether the belt's watch stage has already been
	// given the outcomes for the events a watch sees, so removing one sticks.
	WatchOutcomes bool
	// ReviewOutcomes is likewise for the review events (approved, commented,
	// changes requested). New changes is not one of these.
	ReviewOutcomes bool
	// ShadowTests are the belt's shadow tests, the running one last.
	ShadowTests []core.ShadowTest
	// Shadow is the running test's shadow version, or nil.
	Shadow *core.Belt
}

// BeltRecordArgs are the parameters of NewBeltRecord.
type BeltRecordArgs struct {
	Belt     core.Belt
	Paused   bool
	Archived bool
	Created  time.Time
	// LastFired, when set, attaches to the belt's schedule entry. It is used
	// only when Fired is nil.
	LastFired *time.Time
	// Fired, when non-nil, is taken as the schedule cursors.
	Fired          map[string]time.Time
	WatchOutcomes  bool
	ReviewOutcomes bool
}

// NewBeltRecord creates a record.
// created is the clock's time, passed in by the caller.
func NewBeltRecord(a BeltRecordArgs) BeltRecord {
	record := BeltRecord{
		Belt:           a.Belt,
		Paused:         a.Paused,
		Archived:       a.Archived,
		Created:        a.Created,
		WatchOutcomes:  a.WatchOutcomes,
		ReviewOutcomes: a.ReviewOutcomes,
		Fired:          map[string]time.Time{},
	}
	switch {
	case a.Fired != nil:
		record.Fired = copyFired(a.Fired)
	case a.LastFired != nil:
		record.Fired[scheduleEntryID(a.Belt)] = *a.LastFired
	}
	return record
}

// EffectivelyPaused is true while the belt is paused or archived.
func (r BeltRecord) EffectivelyPaused() bool { return r.Paused || r.Archived }

// LastFired is the first schedule entry's cursor, or nil. A belt saved before
// entries keeps that one date under the legacy id.
func (r BeltRecord) LastFired() *time.Time {
	when, ok := r.Fired[scheduleEntryID(r.Belt)]
	if !ok {
		return nil
	}
	return &when
}

// SetLastFired sets or clears the first schedule entry's cursor.
func (r *BeltRecord) SetLastFired(when *time.Time) {
	if r.Fired == nil {
		r.Fired = map[string]time.Time{}
	}
	id := scheduleEntryID(r.Belt)
	if when != nil {
		r.Fired[id] = *when
	} else {
		delete(r.Fired, id)
	}
}

// state is what state.json holds for this record.
func (r BeltRecord) state() beltState {
	last := r.LastFired()
	return beltState{
		Paused:         r.Paused,
		Archived:       r.Archived,
		LastFired:      dateOrNil(last),
		Schedules:      toDates(r.Fired),
		WatchOutcomes:  r.WatchOutcomes,
		ReviewOutcomes: r.ReviewOutcomes,
		ShadowTests:    r.ShadowTests,
	}
}

// ShadowTest is the running shadow test, or nil.
func (r BeltRecord) ShadowTest() *core.ShadowTest { return core.ShadowRunning(r.ShadowTests) }

// scheduleEntryID is the id of the belt's first schedule entry, or the legacy
// id when it has none.
func scheduleEntryID(belt core.Belt) string {
	for _, entry := range belt.Triggers {
		if entry.Trigger.Kind == core.TriggerKindSchedule {
			return entry.ID
		}
	}
	return core.TriggerEntryLegacyID
}

// LoadBelts is every belt's latest version, in the order each was first
// saved (then by name). A folder without a readable latest version is skipped.
func (s Storage) LoadBelts() []BeltRecord {
	records := []BeltRecord{}
	for _, folder := range subfolders(s.Paths.Belts()) {
		all := versions(folder, "json")
		if len(all) == 0 {
			continue
		}
		latest, first := slices.Max(all), slices.Min(all)
		var belt core.Belt
		if s.Read(filepath.Join(folder, "v"+strconv.Itoa(latest)+".json"), &belt) != nil {
			continue
		}
		var state *beltState
		var loaded beltState
		if s.Read(filepath.Join(folder, "state.json"), &loaded) == nil {
			state = &loaded
		}
		created := birthTime(filepath.Join(folder, "v"+strconv.Itoa(first)+".json"))
		record := BeltRecord{
			Belt:           belt,
			Paused:         state != nil && state.Paused,
			Archived:       state != nil && state.Archived,
			Created:        created,
			Fired:          firedFromState(state, belt),
			WatchOutcomes:  state != nil && state.WatchOutcomes,
			ReviewOutcomes: state != nil && state.ReviewOutcomes,
		}
		if state != nil {
			record.ShadowTests = slices.Clone(state.ShadowTests)
		}
		if test := record.ShadowTest(); test != nil {
			var shadow core.Belt
			if s.Read(filepath.Join(folder, "shadow", "v"+strconv.Itoa(test.Shadow)+".json"), &shadow) == nil {
				record.Shadow = &shadow
			}
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		return a.Belt.Name < b.Belt.Name
	})
	return records
}

// firedFromState reads the schedule cursors of a state file. A file that
// only recorded one lastFired attaches it to the belt's schedule entry.
func firedFromState(state *beltState, belt core.Belt) map[string]time.Time {
	if state != nil && len(state.Schedules) > 0 {
		return toTimes(state.Schedules)
	}
	if state == nil || state.LastFired == nil {
		return map[string]time.Time{}
	}
	return map[string]time.Time{scheduleEntryID(belt): state.LastFired.Time}
}

// versions lists the version numbers of files named v<n>.<ext> in folder, in
// no particular order. A number is read as Atoi reads it: a sign and leading zeros are accepted.
func versions(folder, ext string) []int {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	var found []int
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "v") || !strings.HasSuffix(name, "."+ext) {
			continue
		}
		number, err := strconv.Atoi(name[1 : len(name)-len(ext)-1])
		if err == nil {
			found = append(found, number)
		}
	}
	return found
}

// subfolders lists the entries of dir by name, sorted. A missing or unreadable
// directory is empty.
func subfolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, filepath.Join(dir, entry.Name()))
	}
	return names
}

// Belt is one saved version of a belt, live or shadow, or nil when it does not exist.
func (s Storage) Belt(id string, version int) *core.Belt {
	var belt core.Belt
	if s.Read(s.versionPath(id, version), &belt) == nil {
		return &belt
	}
	if s.Read(s.shadowPath(id, version), &belt) == nil {
		return &belt
	}
	return nil
}

// versionPath is belts/<id>/v<n>.json.
func (s Storage) versionPath(id string, version int) string {
	return filepath.Join(s.Paths.Belts(), id, "v"+strconv.Itoa(version)+".json")
}

// shadowPath is belts/<id>/shadow/v<n>.json. Shadow versions live apart, so the
// highest version in the belt's own folder is always the live one.
func (s Storage) shadowPath(id string, version int) string {
	return filepath.Join(s.Paths.Belts(), id, "shadow", "v"+strconv.Itoa(version)+".json")
}

// allVersions are the belt's live and shadow version numbers, which share one sequence.
func (s Storage) allVersions(id string) []int {
	folder := filepath.Join(s.Paths.Belts(), id)
	return append(versions(folder, "json"), versions(filepath.Join(folder, "shadow"), "json")...)
}

// SaveNewVersion saves belt as its next version and returns it with that
// version number.
func (s Storage) SaveNewVersion(belt core.Belt) (core.Belt, error) {
	saved := belt
	saved.Version = nextVersion(s.allVersions(belt.ID))
	return saved, s.Write(saved, s.versionPath(belt.ID, saved.Version))
}

// SaveShadowVersion writes definition as the belt's next version in its shadow
// folder, then record's state with the running test pointing at it. The version
// is written first: a failed state write leaves a file no test names.
func (s Storage) SaveShadowVersion(definition core.Belt, record BeltRecord) (core.Belt, BeltRecord, error) {
	saved := definition
	saved.Version = nextVersion(s.allVersions(definition.ID))
	if err := s.Write(saved, s.shadowPath(definition.ID, saved.Version)); err != nil {
		return core.Belt{}, record, err
	}
	record.ShadowTests = slices.Clone(record.ShadowTests)
	for i := range record.ShadowTests {
		if record.ShadowTests[i].Running() {
			record.ShadowTests[i].Shadow = saved.Version
		}
	}
	shadow := saved.Clone()
	record.Shadow = &shadow
	if err := s.Write(record.state(), s.stateURL(definition.ID)); err != nil {
		return core.Belt{}, record, err
	}
	return saved, record, nil
}

// PromoteShadow makes a shadow version live. When it is newer than every live
// version its file moves into the belt's folder as it is, so the runs on it keep
// their version; otherwise it is saved again as the next version, so the live
// version is always the newest.
func (s Storage) PromoteShadow(id string, version int) (core.Belt, error) {
	var shadow core.Belt
	if err := s.Read(s.shadowPath(id, version), &shadow); err != nil {
		return core.Belt{}, err
	}
	live := versions(filepath.Join(s.Paths.Belts(), id), "json")
	if len(live) > 0 && slices.Max(live) > version {
		return s.SaveNewVersion(shadow)
	}
	if err := s.Write(shadow, s.versionPath(id, version)); err != nil {
		return core.Belt{}, err
	}
	// The copy in the shadow folder stays: removing it could only lose the version.
	return shadow, nil
}

// nextVersion is one past the highest of the versions, or 1 when there are none.
func nextVersion(all []int) int {
	if len(all) == 0 {
		return 1
	}
	return slices.Max(all) + 1
}

// SaveVersion writes record's pause and other state, then definition as the
// next version. Pause is on disk before that version can be read as the
// latest, so a warned belt cannot come back runnable. The two files are not
// one transaction: when the version write fails, the state already written is
// what a restart loads with the previous version.
func (s Storage) SaveVersion(definition core.Belt, record BeltRecord) (core.Belt, error) {
	record.Belt = definition
	if err := s.Write(record.state(), s.stateURL(definition.ID)); err != nil {
		return core.Belt{}, err
	}
	return s.SaveNewVersion(definition)
}

// StoredState reads the pause fields on disk. ok is false when state.json
// cannot be read.
func (s Storage) StoredState(id string) (StoredBeltState, bool) {
	var state beltState
	if s.Read(s.stateURL(id), &state) != nil {
		return StoredBeltState{}, false
	}
	return StoredBeltState{
		Paused:         state.Paused,
		Archived:       state.Archived,
		LastFired:      timeOrNil(state.LastFired),
		Schedules:      toTimes(state.Schedules),
		WatchOutcomes:  state.WatchOutcomes,
		ReviewOutcomes: state.ReviewOutcomes,
		ShadowTests:    slices.Clone(state.ShadowTests),
	}, true
}

// SetPaused records whether a belt is paused, keeping the rest of its state.
func (s Storage) SetPaused(id string, paused bool) error {
	url := s.stateURL(id)
	state := s.readStateOr(url, newBeltState(paused, false, nil, nil, false, false))
	state.Paused = paused
	return s.Write(state, url)
}

// SetArchived records whether a belt is archived, keeping the rest of its state.
func (s Storage) SetArchived(id string, archived bool) error {
	url := s.stateURL(id)
	state := s.readStateOr(url, newBeltState(false, false, nil, nil, false, false))
	state.Archived = archived
	return s.Write(state, url)
}

// readStateOr is the state at url, or fallback when it cannot be read.
func (s Storage) readStateOr(url string, fallback beltState) beltState {
	var state beltState
	if s.Read(url, &state) != nil {
		return fallback
	}
	return state
}

// stateURL is belts/<id>/state.json.
func (s Storage) stateURL(id string) string {
	return filepath.Join(s.Paths.Belts(), id, "state.json")
}

// SaveState writes whether the belt is paused and when its schedule last
// fired.
func (s Storage) SaveState(record BeltRecord) error {
	return s.Write(record.state(), s.stateURL(record.Belt.ID))
}

// dateOrNil wraps a time for a file field, or returns nil.
func dateOrNil(when *time.Time) *jsonx.Date {
	if when == nil {
		return nil
	}
	return &jsonx.Date{Time: *when}
}

// timeOrNil unwraps a file's date field, or returns nil.
func timeOrNil(when *jsonx.Date) *time.Time {
	if when == nil {
		return nil
	}
	t := when.Time
	return &t
}

// toDates converts cursors for a state file. The result is never nil.
func toDates(times map[string]time.Time) map[string]jsonx.Date {
	out := make(map[string]jsonx.Date, len(times))
	for id, when := range times {
		out[id] = jsonx.Date{Time: when}
	}
	return out
}

// toTimes converts cursors read from a state file. The result is never nil.
func toTimes(dates map[string]jsonx.Date) map[string]time.Time {
	out := make(map[string]time.Time, len(dates))
	for id, when := range dates {
		out[id] = when.Time
	}
	return out
}

// copyFired copies a cursor map. A nil map copies to an empty one.
func copyFired(times map[string]time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(times))
	maps.Copy(out, times)
	return out
}
