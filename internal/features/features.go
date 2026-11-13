package features

import (
	"math"

	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/track"
)

const (
	HistLen        = 10      // history points per example
	MaxHistSpanSec = 15 * 60 // those 10 points must cover at most 15 minutes
	StepFeatures   = 6       // numbers per history point
	NumInputs      = HistLen*StepFeatures + 5
)

// ColumnNames lists every number in a row, in order: inputs then targets.
func ColumnNames() []string {
	var cols []string
	for k := 0; k < HistLen; k++ {
		for _, f := range []string{"dt_min", "dx_km", "dy_km", "speed_kn", "sin_course", "cos_course"} {
			cols = append(cols, f+"_"+string(rune('0'+k)))
		}
	}
	return append(cols, "horizon_min", "lat0", "lon0", "dr_dx_km", "dr_dy_km", "target_dx_km", "target_dy_km")
}

// LocalKm converts (lat, lon) to km east (dx) and north (dy) of (lat0, lon0).
// A flat approximation, accurate enough for distances under ~100 km.
func LocalKm(lat0, lon0, lat, lon float64) (dx, dy float64) {
	dx = (lon - lon0) * 111.320 * math.Cos(lat0*math.Pi/180)
	dy = (lat - lat0) * 110.574
	return dx, dy
}

// FromLocalKm is the reverse of LocalKm.
func FromLocalKm(lat0, lon0, dx, dy float64) (lat, lon float64) {
	lat = lat0 + dy/110.574
	lon = lon0 + dx/(111.320*math.Cos(lat0*math.Pi/180))
	return lat, lon
}

// Inputs turns a ship's history into the model's input numbers for
// predicting horizonSec ahead. ok is false if the history isn't usable;
// callers then fall back to dead reckoning.
func Inputs(history []track.Point, horizonSec int64) (in []float32, ok bool) {
	if len(history) < HistLen {
		return nil, false
	}
	h := history[len(history)-HistLen:]
	last := h[HistLen-1]
	if last.T-h[0].T > MaxHistSpanSec {
		return nil, false // history too spread out
	}
	for _, p := range h {
		if p.SpeedKn < 0 || p.CourseDeg < 0 {
			return nil, false // missing speed or course
		}
	}

	in = make([]float32, 0, NumInputs)
	for _, p := range h {
		dx, dy := LocalKm(last.Lat, last.Lon, p.Lat, p.Lon)
		c := float64(p.CourseDeg) * math.Pi / 180
		in = append(in,
			float32(float64(p.T-last.T)/60), // minutes before now (<= 0)
			float32(dx), float32(dy),
			p.SpeedKn,
			float32(math.Sin(c)), float32(math.Cos(c)),
		)
	}

	// Dead reckoning's guess, relative to the current position
	km := float64(last.SpeedKn) * 1.852 * float64(horizonSec) / 3600
	drLat, drLon := geo.Destination(last.Lat, last.Lon, float64(last.CourseDeg), km)
	drDx, drDy := LocalKm(last.Lat, last.Lon, drLat, drLon)

	in = append(in,
		float32(horizonSec)/60,
		float32(last.Lat), float32(last.Lon),
		float32(drDx), float32(drDy),
	)
	return in, true
}
