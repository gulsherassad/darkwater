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
