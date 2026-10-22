package core

import (
	"reflect"
	"testing"
)

// libraryBrowserTestEntry is one library entry with a name, the filter it belongs to and its search text.
func libraryBrowserTestEntry(id, name, category, search string) LibraryBrowserProjectionEntry {
	card := LibraryCard{ID: id, Name: name, Version: 1, Summary: search, StatusTone: LibraryCardStatusToneGrey}
	return LibraryBrowserProjectionEntry{Card: card, Filter: category, SearchText: name + " " + search}
}

// libraryBrowserTestIDs are the card ids of items, in order; never nil.
func libraryBrowserTestIDs(items []LibraryCard) []string {
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestSearchCountsAndSelectedFilterAreComputedFromTheSameMatchedSet(t *testing.T) {
	t.Parallel()
	entries := []LibraryBrowserProjectionEntry{
		libraryBrowserTestEntry("a", "Alpha", "running", "issues repo-one"),
		libraryBrowserTestEntry("b", "Beta", "paused", "issues repo-two"),
		libraryBrowserTestEntry("c", "Gamma", "archived", "reviews repo-three"),
	}
	result := LibraryBrowserProjectionMake(entries, libraryBrowserFilterDefinitions(), "paused", "issues", nil)

	if result.TotalCount != 3 {
		t.Errorf("totalCount = %d, want 3", result.TotalCount)
	}
	if result.MatchCount != 1 {
		t.Errorf("matchCount = %d, want 1", result.MatchCount)
	}
	if got := libraryBrowserTestIDs(result.Items); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("items = %v, want [b]", got)
	}
	counts := []int{}
	for _, filter := range result.Filters {
		counts = append(counts, filter.Count)
	}
	if !reflect.DeepEqual(counts, []int{2, 1, 1, 0}) {
		t.Errorf("filter counts = %v, want [2 1 1 0]", counts)
	}
}

func TestAllMeansLiveAndOrderingIsAlphabeticWithStableIDTieBreak(t *testing.T) {
	t.Parallel()
	entries := []LibraryBrowserProjectionEntry{
		libraryBrowserTestEntry("b", "zulu", "running", ""),
		libraryBrowserTestEntry("c", "Alpha", "paused", ""),
		libraryBrowserTestEntry("a", "alpha", "running", ""),
		libraryBrowserTestEntry("arch", "Archive", "archived", ""),
	}
	result := LibraryBrowserProjectionMake(entries, libraryBrowserFilterDefinitions(), "all", "", nil)
	if got := libraryBrowserTestIDs(result.Items); !reflect.DeepEqual(got, []string{"a", "c", "b"}) {
		t.Errorf("items = %v, want [a c b]", got)
	}
	counts := []int{}
	for _, filter := range result.Filters {
		counts = append(counts, filter.Count)
	}
	if !reflect.DeepEqual(counts, []int{3, 2, 1, 1}) {
		t.Errorf("filter counts = %v, want [3 2 1 1]", counts)
	}
	if result.TotalCount != 4 {
		t.Errorf("totalCount = %d, want 4", result.TotalCount)
	}
}

// TestSearchTrimsAndIgnoresCaseAndAnUnknownFilterShowsAll: the search is trimmed and matched
// without case, and a filter the browser does not offer falls back to all.
func TestSearchTrimsAndIgnoresCaseAndAnUnknownFilterShowsAll(t *testing.T) {
	t.Parallel()
	entries := []LibraryBrowserProjectionEntry{
		libraryBrowserTestEntry("a", "Alpha", "running", "issues repo-one"),
		libraryBrowserTestEntry("b", "Beta", "paused", "issues repo-two"),
		libraryBrowserTestEntry("c", "Gamma", "archived", "reviews repo-three"),
	}
	cases := []struct {
		name, filter, search string
		want                 []string
	}{
		{"padded and upper case search", "all", "  ISSUES  ", []string{"a", "b"}},
		{"unknown filter shows all", "nope", "issues", []string{"a", "b"}},
		{"selected filter with padded search", "paused", " Issues ", []string{"b"}},
		{"archived is only under its own filter", "archived", "REVIEWS", []string{"c"}},
	}
	for _, c := range cases {
		result := LibraryBrowserProjectionMake(entries, libraryBrowserFilterDefinitions(), c.filter, c.search, nil)
		if got := libraryBrowserTestIDs(result.Items); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: items = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestOrderFoldsCaseAndAccentsOfNames: names sort by their folded form, so "Émile" is with the
// e's, before "Zed", and the id breaks a tie.
func TestOrderFoldsCaseAndAccentsOfNames(t *testing.T) {
	t.Parallel()
	entries := []LibraryBrowserProjectionEntry{
		libraryBrowserTestEntry("z", "Zed", "running", "x"),
		libraryBrowserTestEntry("e", "Émile", "running", "x"),
		libraryBrowserTestEntry("e2", "emile", "running", "x"),
	}
	result := LibraryBrowserProjectionMake(entries, libraryBrowserFilterDefinitions(), "all", "", nil)
	if got := libraryBrowserTestIDs(result.Items); !reflect.DeepEqual(got, []string{"e", "e2", "z"}) {
		t.Errorf("items = %v, want [e e2 z]", got)
	}
}
