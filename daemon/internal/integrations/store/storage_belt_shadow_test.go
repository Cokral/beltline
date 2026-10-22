package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// shadowRecord saves belt once and returns its record with a running test.
func shadowRecord(t *testing.T, s Storage, belt core.Belt) BeltRecord {
	t.Helper()
	saved, err := s.SaveNewVersion(belt)
	if err != nil {
		t.Fatal(err)
	}
	record := NewBeltRecord(BeltRecordArgs{Belt: saved})
	record.ShadowTests = []core.ShadowTest{{ID: "t1", Live: saved.Version, StartedAt: jsonx.Date{Time: time.Unix(1_700_000_000, 0)}}}
	return record
}

// TestShadowVersionsShareTheNumbersButNotTheFolder checks a shadow version takes
// the next number, lives in the shadow folder so the live version stays the
// newest in the belt's own, and reloads with its test.
func TestShadowVersionsShareTheNumbersButNotTheFolder(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	record := shadowRecord(t, s, belt)
	shadow, record, err := s.SaveShadowVersion(belt, record)
	if err != nil || shadow.Version != 2 {
		t.Fatalf("shadow save %d, %v", shadow.Version, err)
	}
	if record.ShadowTest().Shadow != 2 || record.Shadow == nil || record.Shadow.Version != 2 {
		t.Fatalf("record after the shadow save %+v", record.ShadowTests)
	}
	if _, err := os.Stat(filepath.Join(s.Paths.Belts(), belt.ID, "shadow", "v2.json")); err != nil {
		t.Fatal(err)
	}
	if live, err := s.SaveNewVersion(belt); err != nil || live.Version != 3 {
		t.Fatalf("a live save after the shadow takes %d, %v", live.Version, err)
	}
	if got := s.Belt(belt.ID, 2); got == nil || got.Version != 2 {
		t.Fatalf("Belt reads the shadow version: %v", got)
	}
	loaded := s.LoadBelts()
	if len(loaded) != 1 || loaded[0].Belt.Version != 3 || loaded[0].Shadow == nil || loaded[0].Shadow.Version != 2 {
		t.Fatalf("loaded %+v", loaded)
	}
	if test := loaded[0].ShadowTest(); test == nil || test.ID != "t1" || test.Live != 1 {
		t.Fatalf("loaded test %+v", loaded[0].ShadowTests)
	}
}

// TestPromoteShadowKeepsItsNumberOnlyWhenItIsTheNewest checks an accepted shadow
// newer than every live version keeps its number, and one live has passed is
// saved again as the next version.
func TestPromoteShadowKeepsItsNumberOnlyWhenItIsTheNewest(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	record := shadowRecord(t, s, belt)
	if _, _, err := s.SaveShadowVersion(belt, record); err != nil {
		t.Fatal(err)
	}
	promoted, err := s.PromoteShadow(belt.ID, 2)
	if err != nil || promoted.Version != 2 {
		t.Fatalf("promoted %d, %v", promoted.Version, err)
	}
	if got := s.LoadBelts(); got[0].Belt.Version != 2 {
		t.Fatalf("live after promote %d", got[0].Belt.Version)
	}

	other := newTestStorage(t)
	record = shadowRecord(t, other, belt)
	if _, _, err := other.SaveShadowVersion(belt, record); err != nil {
		t.Fatal(err)
	}
	if _, err := other.SaveNewVersion(belt); err != nil {
		t.Fatal(err)
	}
	promoted, err = other.PromoteShadow(belt.ID, 2)
	if err != nil || promoted.Version != 4 {
		t.Fatalf("promoted behind live %d, %v", promoted.Version, err)
	}
}
