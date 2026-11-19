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

// Which branch Pick took
const (
	PathShort    = "short"    // below SwitchSec
	PathLong     = "long"     // dense model
	PathSparse   = "sparse"   // sparse model
	PathFallback = "fallback" // neither model could read the history: dead reckoning
)

// Pick returns the predictor the hybrid uses for this question, and which branch that is.
func (h Hybrid) Pick(history []track.Point, horizonSec int64) (Predictor, string) {
	switch {
	case horizonSec < h.SwitchSec:
		return h.Short, PathShort
	case usable(h.Long, history):
		return h.Long, PathLong
	case usable(h.Sparse, history):
		return h.Sparse, PathSparse
	}
	return DeadReckoning{}, PathFallback
}

func (h Hybrid) Predict(history []track.Point, horizonSec int64) (float64, float64) {
	p, _ := h.Pick(history, horizonSec)
	return p.Predict(history, horizonSec)
}
