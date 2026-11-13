package predict

import (
	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/track"
)

// Predictor guesses where a ship will be horizonSec seconds after
// the last point in its history.
type Predictor interface {
	Name() string
	Predict(history []track.Point, horizonSec int64) (lat, lon float64)
}

// StayPut assumes the ship doesn't move. The floor every method must beat.
type StayPut struct{}

func (StayPut) Name() string { return "stay-put" }

func (StayPut) Predict(h []track.Point, _ int64) (float64, float64) {
	last := h[len(h)-1]
	return last.Lat, last.Lon
}

// DeadReckoning assumes the ship keeps its last speed and course.
type DeadReckoning struct{}

func (DeadReckoning) Name() string { return "dead-reckoning" }

func (DeadReckoning) Predict(h []track.Point, horizonSec int64) (float64, float64) {
	last := h[len(h)-1]
	if last.SpeedKn < 0 || last.CourseDeg < 0 {
		return last.Lat, last.Lon // unknown speed or course: fall back to stay-put
	}
	km := float64(last.SpeedKn) * 1.852 * float64(horizonSec) / 3600 // knots -> km
	return geo.Destination(last.Lat, last.Lon, float64(last.CourseDeg), km)
}
