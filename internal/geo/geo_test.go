package geo

import (
	"math"
	"testing"
)

func TestHaversine(t *testing.T) {
	// Copenhagen to Aarhus is about 157 km
	d := HaversineKm(55.6761, 12.5683, 56.1629, 10.2039)
	if math.Abs(d-157) > 2 {
		t.Fatalf("Copenhagen-Aarhus: want ~157 km, got %.1f", d)
	}
	if HaversineKm(55, 10, 55, 10) != 0 {
		t.Fatal("same point should be 0 km apart")
	}
}

func TestDestinationRoundTrip(t *testing.T) {
	// Travel 25 km north-east, then measure: should be 25 km away
	lat, lon := Destination(55.5, 11.0, 45, 25)
	if d := HaversineKm(55.5, 11.0, lat, lon); math.Abs(d-25) > 0.001 {
		t.Fatalf("want 25 km, got %.4f", d)
	}
	if lat <= 55.5 || lon <= 11.0 {
		t.Fatalf("north-east should increase lat and lon, got %.4f, %.4f", lat, lon)
	}
}
