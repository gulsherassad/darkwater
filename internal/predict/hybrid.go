package predict

import "github.com/gulsherassad/darkwater/internal/track"

// Checker is implemented by predictors that only work on some histories
// (e.g. a GRU needing K recent fixes). Predictors without it are assumed to
// handle any history.
type Checker interface {
	Usable(history []track.Point) bool
}

// Hybrid uses Short below SwitchSec. At or above it, it uses Long if Long can
// handle the history (dense fixes), else Sparse if set and usable (live-feed
// gaps), else dead reckoning.
type Hybrid struct {
	Short, Long Predictor
	Sparse      Predictor // optional
	SwitchSec   int64
}

func (h Hybrid) Name() string { return "hybrid" }

func usable(p Predictor, history []track.Point) bool {
	if p == nil {
		return false
	}
	c, ok := p.(Checker)
	return !ok || c.Usable(history)
}

func (h Hybrid) Predict(history []track.Point, horizonSec int64) (float64, float64) {
	if horizonSec < h.SwitchSec {
		return h.Short.Predict(history, horizonSec)
	}
	for _, p := range []Predictor{h.Long, h.Sparse} {
		if usable(p, history) {
			return p.Predict(history, horizonSec)
		}
	}
	return DeadReckoning{}.Predict(history, horizonSec)
}
