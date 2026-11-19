package detect

import (
	"testing"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
)

var t0 = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

func pos(mmsi int64, minutes float64, lat, lon float64) ais.Position {
	return ais.Position{MMSI: mmsi, Lat: lat, Lon: lon, Time: t0.Add(time.Duration(minutes * float64(time.Minute)))}
}

func collect(d *SpoofDetector) *[]SpoofEvent {
	var got []SpoofEvent
	d.OnEvent = func(e SpoofEvent) { got = append(got, e) }
	return &got
}

func TestNormalShipIsNotSpoofing(t *testing.T) {
	d := NewSpoofDetector()
	got := collect(d)
	// ~12 knots north: 0.0036° of latitude per minute ≈ 0.4 km/min
	for i := 0; i < 10; i++ {
		d.Observe(pos(219000001, float64(i), 55+float64(i)*0.0036, 11))
	}
	if len(*got) != 0 {
		t.Fatalf("a 12-knot ship shouldn't trigger anything: %+v", *got)
	}
}

func TestTeleport(t *testing.T) {
	d := NewSpoofDetector()
	got := collect(d)
	d.Observe(pos(219000002, 0, 55.0, 11.0))
	d.Observe(pos(219000002, 1, 55.09, 11.0)) // ~10 km in 1 minute ≈ 324 knots
	if len(*got) != 1 || (*got)[0].Kind != SpoofTeleport || (*got)[0].ImpliedKn < 300 {
		t.Fatalf("want one ~324 kn teleport, got %+v", *got)
	}
}

func TestSmallJitterIgnored(t *testing.T) {
	d := NewSpoofDetector()
	got := collect(d)
	d.Observe(pos(219000003, 0, 55.0, 11.0))
	d.Observe(pos(219000003, 0.1, 55.004, 11.0)) // ~0.45 km in 6 s: fast, but under MinKm
	if len(*got) != 0 {
		t.Fatalf("sub-1 km jitter should be ignored: %+v", *got)
	}
}

func TestIdentityClash(t *testing.T) {
	d := NewSpoofDetector()
	got := collect(d)
	// Same MMSI alternating between two places 50 km apart, every 2 minutes
	for i := 0; i < 4; i++ {
		lat := 55.0
		if i%2 == 1 {
			lat = 55.45
		}
		d.Observe(pos(219000004, float64(i*2), lat, 11.0))
	}
	clashes := 0
	for _, e := range *got {
		if e.Kind == SpoofClash {
			clashes++
		}
	}
	if clashes != 1 {
		t.Fatalf("want exactly 1 identity clash from 3 teleports, got %d: %+v", clashes, *got)
	}
}
