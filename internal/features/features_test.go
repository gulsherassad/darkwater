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

// n points heading north at 10 kn, stepSec apart
func straightTrack(n int, stepSec int64) []track.Point {
	var h []track.Point
	for i := 0; i < n; i++ {
		h = append(h, track.Point{T: int64(i) * stepSec, Lat: 55 + float64(i)*0.003, Lon: 11, SpeedKn: 10, CourseDeg: 0})
	}
	return h
}

func TestInputs(t *testing.T) {
	for _, s := range []Spec{Dense, Sparse} {
		in, ok := s.Inputs(straightTrack(s.HistLen, 60), 900)
		if !ok {
			t.Fatalf("%s: straight track should be usable", s.Version)
		}
		if len(in) != s.NumInputs() {
			t.Fatalf("%s: want %d inputs, got %d", s.Version, s.NumInputs(), len(in))
		}
		if len(s.ColumnNames()) != s.NumInputs()+2 {
			t.Fatalf("%s: column names must cover inputs + 2 targets", s.Version)
		}
		// Last history point is "now": zero offset
		lastStep := (s.HistLen - 1) * s.StepFeatures()
		if in[lastStep] != 0 || in[lastStep+1] != 0 || in[lastStep+2] != 0 {
			t.Fatalf("%s: last point should have zero time and position offset", s.Version)
		}
	}
}

// The dense recipe must not change, or models/traj_gru.onnx gets wrong inputs.
func TestDenseUnchanged(t *testing.T) {
	if Dense.NumInputs() != 65 || Dense.StepFeatures() != 6 {
		t.Fatalf("dense spec changed: %d inputs", Dense.NumInputs())
	}
	if Sparse.NumInputs() == Dense.NumInputs() {
		t.Fatal("specs need different widths so a model can't be paired with the wrong one")
	}
}

func TestSparseGapFeature(t *testing.T) {
	h := straightTrack(Sparse.HistLen, 240) // one fix every 4 min
	h[3].T += 60                            // one gap of 5 min, the next of 3 min
	in, ok := Sparse.Inputs(h, 900)
	if !ok {
		t.Fatal("6 fixes over ~20 min should be usable")
	}
	gap := func(k int) float32 { return in[k*Sparse.StepFeatures()+6] }
	if gap(0) != 0 || gap(1) != 4 || gap(3) != 5 || gap(4) != 3 {
		t.Fatalf("wrong gaps: %v %v %v %v", gap(0), gap(1), gap(3), gap(4))
	}
}

func TestInputsRejectsBadHistory(t *testing.T) {
	if _, ok := Dense.Inputs(straightTrack(5, 60), 900); ok {
		t.Fatal("too few points should be rejected")
	}
	h := straightTrack(Dense.HistLen, 60)
	h[3].SpeedKn = -1
	if _, ok := Dense.Inputs(h, 900); ok {
		t.Fatal("unknown speed should be rejected")
	}
	if Dense.Usable(straightTrack(Dense.HistLen, 240)) {
		t.Fatal("10 fixes over 36 min is too sparse for the dense spec")
	}
	if !Sparse.Usable(straightTrack(Sparse.HistLen, 240)) {
		t.Fatal("6 fixes over 20 min should suit the sparse spec")
	}
	if Sparse.Usable(straightTrack(Sparse.HistLen, 20*60)) {
		t.Fatal("6 fixes over 100 min is beyond the sparse window")
	}
}

func TestSpecForInputs(t *testing.T) {
	if s, err := SpecForInputs(65); err != nil || s.Version != Dense.Version {
		t.Fatal("65 inputs should be the dense spec")
	}
	if s, err := SpecForInputs(Sparse.NumInputs()); err != nil || s.Version != Sparse.Version {
		t.Fatal("47 inputs should be the sparse spec")
	}
	if _, err := SpecForInputs(12); err == nil {
		t.Fatal("unknown width should be an error")
	}
}
