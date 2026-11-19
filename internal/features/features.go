package features

import (
	"fmt"
	"math"

	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/track"
)

// Spec is one version of the model's input recipe. Training (trajexport) and
// serving (model.GRU) must use the same Spec, so a model is tied to exactly one.
type Spec struct {
	Version    string // written to meta.json and the model's metrics
	HistLen    int    // history points per example
	MaxSpanSec int64  // those points must cover at most this long
	WithGap    bool   // add the time since the previous fix as a feature
}

// Dense is v1: built for DMA's ~10 s reporting (models/traj_gru.onnx).
var Dense = Spec{Version: "dense-v1", HistLen: 10, MaxSpanSec: 15 * 60}

// Sparse is v2: built for the live feed's ~3–4 min gaps (models/traj_gru_sparse.onnx).
var Sparse = Spec{Version: "sparse-v2", HistLen: 6, MaxSpanSec: 90 * 60, WithGap: true}

// ContextFeatures follow the sequence: horizon, lat0, lon0, dr_dx, dr_dy.
const ContextFeatures = 5

// StepFeatures is the count of numbers per history point.
func (s Spec) StepFeatures() int {
	if s.WithGap {
		return 7
	}
	return 6
}

func (s Spec) NumInputs() int { return s.HistLen*s.StepFeatures() + ContextFeatures }

// SpecForInputs finds the Spec whose input width is n (e.g. read from an ONNX model).
func SpecForInputs(n int) (Spec, error) {
	for _, s := range []Spec{Dense, Sparse} {
		if s.NumInputs() == n {
			return s, nil
		}
	}
	return Spec{}, fmt.Errorf("no feature spec has %d inputs", n)
}

// ColumnNames lists every number in a row, in order: inputs then targets.
func (s Spec) ColumnNames() []string {
	step := []string{"dt_min", "dx_km", "dy_km", "speed_kn", "sin_course", "cos_course"}
	if s.WithGap {
		step = append(step, "gap_min")
	}
	var cols []string
	for k := 0; k < s.HistLen; k++ {
		for _, f := range step {
			cols = append(cols, fmt.Sprintf("%s_%d", f, k))
		}
	}
	return append(cols, "horizon_min", "lat0", "lon0", "dr_dx_km", "dr_dy_km", "target_dx_km", "target_dy_km")
}

// Window returns the history points the model would read, or ok=false if the
// history isn't usable (too short, too spread out, or missing speed/course).
func (s Spec) Window(history []track.Point) (h []track.Point, ok bool) {
	if len(history) < s.HistLen {
		return nil, false
	}
	h = history[len(history)-s.HistLen:]
	if h[len(h)-1].T-h[0].T > s.MaxSpanSec {
		return nil, false // history too spread out
	}
	for _, p := range h {
		if p.SpeedKn < 0 || p.CourseDeg < 0 {
			return nil, false // missing speed or course
		}
	}
	return h, true
}

// Usable reports whether Inputs would succeed for this history.
func (s Spec) Usable(history []track.Point) bool {
	_, ok := s.Window(history)
	return ok
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
func (s Spec) Inputs(history []track.Point, horizonSec int64) (in []float32, ok bool) {
	h, ok := s.Window(history)
	if !ok {
		return nil, false
	}
	last := h[len(h)-1]

	in = make([]float32, 0, s.NumInputs())
	for i, p := range h {
		dx, dy := LocalKm(last.Lat, last.Lon, p.Lat, p.Lon)
		c := float64(p.CourseDeg) * math.Pi / 180
		in = append(in,
			float32(float64(p.T-last.T)/60), // minutes before now (<= 0)
			float32(dx), float32(dy),
			p.SpeedKn,
			float32(math.Sin(c)), float32(math.Cos(c)),
		)
		if s.WithGap {
			gap := 0.0 // first point in the window: no previous fix
			if i > 0 {
				gap = float64(p.T-h[i-1].T) / 60
			}
			in = append(in, float32(gap))
		}
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
