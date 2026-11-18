package detect

import (
	"math"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/reception"
)

const (
	MinSpeedKn     = 1.0           // slower ships (in port, at anchor) aren't judged
	MaxSilence     = 6 * time.Hour // beyond this the projection is meaningless
	MinCellData    = 30            // ship-minutes needed before we trust a cell's reception
	MaxFeedDownMin = 5             // longer feed outages during a silence: don't judge
)

// FeedAlive reports whether our feed was receiving anything in a given
// minute (Unix seconds / 60). nil means "always alive".
type FeedAlive func(minute int64) bool

// GapResult describes how unusual a ship's current silence is.
type GapResult struct {
	SilentMinutes   int     // minutes since the last report
	FeedDownMinutes int     // of those, minutes our own feed was down
	JudgedMinutes   int     // minutes spent in cells where we know the reception
	LogP            float64 // log chance of this much silence (0 = nothing judged)
}

// Prob is the chance of this silence happening by bad reception alone.
func (r GapResult) Prob() float64 { return math.Exp(r.LogP) }

// Suspicious: silence this long would happen less than alpha of the time.
func (r GapResult) Suspicious(alpha float64) bool { return r.LogP < math.Log(alpha) }

// ScoreGap judges a ship that has been silent since `last`, as of `now`.
func ScoreGap(m *reception.Model, last ais.Position, now time.Time, alive FeedAlive) GapResult {
	silent := now.Sub(last.Time)
	if silent > MaxSilence {
		silent = MaxSilence
	}
	minutes := int(silent / time.Minute)
	res := GapResult{SilentMinutes: minutes}
	if last.SpeedKn < MinSpeedKn {
		return res // stationary or unknown speed: don't judge
	}

	var cells []h3.Cell
	for i := 1; i <= minutes; i++ {
		minute := last.Time.Add(time.Duration(i)*time.Minute).Unix() / 60
		if alive != nil && !alive(minute) {
			res.FeedDownMinutes++
			continue // our feed wasn't listening: not the ship's fault
		}
		lat, lon := project(last, float64(i))
		p := ais.Position{Lat: lat, Lon: lon}
		if !grid.InDenmark(p) {
			continue // outside the study area: no evidence
		}
		c := grid.CellOf(p)
		if m.Data(c).Present < MinCellData {
			continue // not enough data about reception here
		}
		cells = append(cells, c)
	}

	if res.FeedDownMinutes > MaxFeedDownMin {
		return res // we lost track during a feed outage: can't judge
	}
	res.JudgedMinutes = len(cells)
	res.LogP = m.SilenceLogProb(cells)
	return res
}

// project: where the ship probably is after `minutes`, by dead reckoning.
func project(last ais.Position, minutes float64) (float64, float64) {
	if last.SpeedKn < 0 || last.CourseDeg < 0 {
		return last.Lat, last.Lon
	}
	km := last.SpeedKn * 1.852 * minutes / 60
	return geo.Destination(last.Lat, last.Lon, last.CourseDeg, km)
}
