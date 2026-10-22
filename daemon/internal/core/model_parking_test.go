package core

import (
	"testing"
)

// TestLegacyJsonWithoutParkingDecodesAsUnparked checks that JSON saved before parking loads as unparked.
// The parking is set directly, not by the engine, so only the model's decoding
// and round trip are checked.
func TestLegacyJsonWithoutParkingDecodesAsUnparked(t *testing.T) {
	t.Parallel()
	run := nfDefaultRun()
	obj := testJSONMap(t, run)
	delete(obj, "parking")
	decoded := testRunFrom(t, obj)
	if decoded.Parking != nil {
		t.Errorf("parking = %+v, want none", decoded.Parking)
	}
	if decoded.StageID != run.StageID {
		t.Errorf("stageId = %q, want %q", decoded.StageID, run.StageID)
	}
	nfParked(&run, testT0)
	round := btDecode[Run](t, btEncode(t, run))
	if round.Parking == nil || !nfSameParkingRoutes(round.Parking.Routes, run.Parking.Routes) {
		t.Fatalf("parking routes after a round trip = %+v, want %+v", round.Parking, run.Parking)
	}
	if !round.Parking.At.Equal(run.Parking.At.Time) {
		t.Errorf("parking at = %v, want %v", round.Parking.At.Time, run.Parking.At.Time)
	}
}
