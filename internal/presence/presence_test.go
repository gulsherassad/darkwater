package presence

import (
	"math"
	"testing"
)

func TestWalkInterpolatesShortGapsOnly(t *testing.T) {
	fixes := map[int64][]Fix{
		1: {
			{Minute: 100, Lat: 55, Lon: 11, SpeedKn: 10},
			{Minute: 104, Lat: 55.4, Lon: 11, SpeedKn: 14}, // 4-min DMA gap: filled
			{Minute: 200, Lat: 56, Lon: 11, SpeedKn: 10},   // 96-min gap: not filled
		},
	}
	got := map[int64]Fix{}
	observed := 0
	Walk(fixes, func(mmsi int64, f Fix, obs bool) {
		got[f.Minute] = f
		if obs {
			observed++
		}
	})
	if len(got) != 6 || observed != 3 {
		t.Fatalf("want 3 fixes + 3 interpolated minutes, got %d minutes (%d observed)", len(got), observed)
	}
	mid := got[102]
	if math.Abs(mid.Lat-55.2) > 1e-9 || math.Abs(mid.SpeedKn-12) > 1e-9 {
		t.Fatalf("minute 102 should be halfway: %+v", mid)
	}
	if _, ok := got[150]; ok {
		t.Fatal("long DMA gap must not be filled")
	}
}

func TestInFitSetSplitsRoughlyInHalf(t *testing.T) {
	fit := 0
	for mmsi := int64(219000000); mmsi < 219010000; mmsi += 10 { // MMSIs ending in 0, like big ships
		if InFitSet(mmsi) {
			fit++
		}
	}
	if fit < 400 || fit > 600 {
		t.Fatalf("hash split should be ~50%%, got %d of 1000", fit)
	}
}
