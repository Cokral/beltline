package core

import (
	"slices"
	"strings"
	"testing"
)

// TestStateFlagReadsTrueAndOneOnly checks the flag reads: the
// literals true and false, and the JSON numbers 1 and 0.
func TestStateFlagReadsTrueAndOneOnly(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		`{"archived":true}`:           true,
		`{"archived":1}`:              true,
		`{"archived": 1 }`:            true,
		`{"archived":false}`:          false,
		`{"archived":0}`:              false,
		`{"archived":2}`:              false,
		`{"archived":1.0}`:            false,
		`{"archived":"true"}`:         false,
		`{"archived":"1"}`:            false,
		`{"archived":null}`:           false,
		`{"archived":[true]}`:         false,
		`{"paused":true}`:             false,
		`{}`:                          false,
		`[true]`:                      false,
		`"archived"`:                  false,
		`{"archived":true`:            false,
		`not json`:                    false,
		`{"archived":0,"archived":1}`: true,
		`{"archived":1,"archived":0}`: false,
	}
	for in, want := range cases {
		if got := StateFlag([]byte(in), "archived"); got != want {
			t.Errorf("StateFlag(%s) = %v, want %v", in, got, want)
		}
	}
	if !StateFlag([]byte(`{"paused":1}`), "paused") {
		t.Error("paused 1 not read")
	}
}

// TestLibraryCheckSkipsAnArchivedBeltWrittenWithOne checks that an archived
// flag written as 1 keeps its belt out of the library check.
func TestLibraryCheckSkipsAnArchivedBeltWrittenWithOne(t *testing.T) {
	t.Parallel()
	broken := map[string][]byte{"belts/old/v1.json": []byte(`{"id":"old"}`)}
	if problems := LibraryCheckProblems(broken, nil); !containsSubstringPrefix(problems, "belts/old/") {
		t.Fatalf("an unarchived broken belt reported no problem: %v", problems)
	}
	archived := map[string][]byte{
		"belts/old/v1.json":    broken["belts/old/v1.json"],
		"belts/old/state.json": []byte(`{"archived":1}`),
	}
	if problems := LibraryCheckProblems(archived, nil); containsSubstringPrefix(problems, "belts/old/") {
		t.Fatalf("an archived belt (archived: 1) was checked: %v", problems)
	}
	quoted := map[string][]byte{
		"belts/old/v1.json":    broken["belts/old/v1.json"],
		"belts/old/state.json": []byte(`{"archived":"1"}`),
	}
	if problems := LibraryCheckProblems(quoted, nil); !containsSubstringPrefix(problems, "belts/old/") {
		t.Fatalf("a quoted archived flag hid the belt: %v", problems)
	}
}

// containsSubstringPrefix reports whether any value starts with prefix.
func containsSubstringPrefix(values []string, prefix string) bool {
	return slices.ContainsFunc(values, func(v string) bool { return strings.HasPrefix(v, prefix) })
}
