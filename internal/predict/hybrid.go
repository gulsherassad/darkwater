package predict

import "github.com/gulsherassad/darkwater/internal/track"

// Hybrid uses Short below SwitchSec and Long at or above it.
type Hybrid struct {
	Short, Long Predictor
	SwitchSec   int64
}

func (h Hybrid) Name() string { return "hybrid" }

func (h Hybrid) Predict(history []track.Point, horizonSec int64) (float64, float64) {
	if horizonSec < h.SwitchSec {
		return h.Short.Predict(history, horizonSec)
	}
	return h.Long.Predict(history, horizonSec)
}
