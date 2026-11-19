package predict

import (
	"math"
	"testing"

	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/track"
)

func TestDeadReckoning(t *testing.T) {
	// Heading due east at 10 knots for 60 minutes = 18.52 km
	h := []track.Point{{T: 0, Lat: 55, Lon: 10, SpeedKn: 10, CourseDeg: 90}}
	lat, lon := DeadReckoning{}.Predict(h, 3600)

	if d := geo.HaversineKm(55, 10, lat, lon); math.Abs(d-18.52) > 0.01 {
		t.Fatalf("want 18.52 km, got %.3f", d)
	}
	if lon <= 10 {
		t.Fatal("heading east should increase longitude")
	}
}

func TestDeadReckoningUnknownSpeed(t *testing.T) {
	h := []track.Point{{T: 0, Lat: 55, Lon: 10, SpeedKn: -1, CourseDeg: 90}}
	lat, lon := DeadReckoning{}.Predict(h, 3600)
	if lat != 55 || lon != 10 {
		t.Fatal("unknown speed should fall back to the last position")
	}
}

// fake predictor that answers with a fixed spot, usable only for >= minPts points
type fake struct {
	minPts   int
	lat, lon float64
}

func (f fake) Name() string                                        { return "fake" }
func (f fake) Usable(h []track.Point) bool                         { return len(h) >= f.minPts }
func (f fake) Predict(_ []track.Point, _ int64) (float64, float64) { return f.lat, f.lon }

func TestHybridPicksModelByHistory(t *testing.T) {
	dense := fake{minPts: 10, lat: 1, lon: 1}
	sparse := fake{minPts: 6, lat: 2, lon: 2}
	h := Hybrid{Short: StayPut{}, Long: dense, Sparse: sparse, SwitchSec: 600}
	hist := func(n int) []track.Point {
		return make([]track.Point, n) // positions don't matter to the fakes
	}

	if lat, _ := h.Predict(hist(10), 300); lat != 0 {
		t.Fatal("below the switch: Short")
	}
	if lat, _ := h.Predict(hist(10), 1800); lat != 1 {
		t.Fatal("dense history: Long")
	}
	if lat, _ := h.Predict(hist(7), 1800); lat != 2 {
		t.Fatal("sparse history: Sparse")
	}
	for _, c := range []struct {
		n    int
		hz   int64
		want string
	}{{10, 300, PathShort}, {10, 1800, PathLong}, {7, 1800, PathSparse}, {3, 1800, PathFallback}} {
		if _, got := h.Pick(hist(c.n), c.hz); got != c.want {
			t.Fatalf("%d points, %d s: want path %s, got %s", c.n, c.hz, c.want, got)
		}
	}
	// Too little for either model: dead reckoning (unknown speed here → last position)
	short := []track.Point{{Lat: 55, Lon: 10, SpeedKn: -1, CourseDeg: -1}}
	if lat, lon := h.Predict(short, 1800); lat != 55 || lon != 10 {
		t.Fatal("unusable history: dead reckoning")
	}
	// Without a sparse model the old behaviour holds
	h.Sparse = nil
	if lat, _ := h.Predict(hist(7), 1800); lat != 0 {
		t.Fatal("no sparse model: dead reckoning")
	}
}
