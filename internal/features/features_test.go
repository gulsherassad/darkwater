package features

import (
	"math"
	"testing"

	"github.com/gulsherassad/darkwater/internal/track"
)

func TestLocalKmRoundTrip(t *testing.T) {
	dx, dy := LocalKm(55.5, 11.0, 55.6, 11.2)
	lat, lon := FromLocalKm(55.5, 11.0, dx, dy)
	if math.Abs(lat-55.6) > 1e-9 || math.Abs(lon-11.2) > 1e-9 {
		t.Fatalf("round trip failed: %.6f, %.6f", lat, lon)
	}
}

func straightTrack() []track.Point {
	var h []track.Point
	for i := 0; i < HistLen; i++ {
		h = append(h, track.Point{T: int64(i * 60), Lat: 55 + float64(i)*0.003, Lon: 11, SpeedKn: 10, CourseDeg: 0})
	}
	return h
}

func TestInputs(t *testing.T) {
	in, ok := Inputs(straightTrack(), 900)
	if !ok {
		t.Fatal("straight 10-point track should be usable")
	}
	if len(in) != NumInputs {
		t.Fatalf("want %d inputs, got %d", NumInputs, len(in))
	}
	if len(ColumnNames()) != NumInputs+2 {
		t.Fatal("column names must cover inputs + 2 targets")
	}
	// Last history point is "now": zero offset
	lastStep := (HistLen - 1) * StepFeatures
	if in[lastStep] != 0 || in[lastStep+1] != 0 || in[lastStep+2] != 0 {
		t.Fatal("last point should have zero time and position offset")
	}
}

func TestInputsRejectsBadHistory(t *testing.T) {
	if _, ok := Inputs(straightTrack()[:5], 900); ok {
		t.Fatal("too few points should be rejected")
	}
	h := straightTrack()
	h[3].SpeedKn = -1
	if _, ok := Inputs(h, 900); ok {
		t.Fatal("unknown speed should be rejected")
	}
}
