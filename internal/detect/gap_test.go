package detect

import (
	"testing"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/reception"
)

// A slow ship that stays inside one cell for the whole test.
var start = time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)
var ship = ais.Position{MMSI: 219000001, Lat: 55.7, Lon: 12.6, SpeedKn: 1.5, CourseDeg: 90, Time: start}

func modelWith(heard, present int) *reception.Model {
	c := grid.CellOf(ship)
	return reception.Fit(map[h3.Cell]reception.Counts{c: {Present: present, Heard: heard}}, 20)
}

func TestGoodCoverageLongSilenceIsSuspicious(t *testing.T) {
	m := modelWith(500, 1000) // ~50% per minute
	r := ScoreGap(m, ship, start.Add(20*time.Minute), nil)
	if !r.Suspicious(0.01) {
		t.Fatalf("20 silent minutes in good coverage should be suspicious: %+v (p=%.2g)", r, r.Prob())
	}
}

func TestPoorCoverageIsNotSuspicious(t *testing.T) {
	m := modelWith(10, 1000) // ~1% per minute
	r := ScoreGap(m, ship, start.Add(20*time.Minute), nil)
	if r.Suspicious(0.01) {
		t.Fatalf("20 silent minutes in poor coverage is normal: %+v (p=%.2g)", r, r.Prob())
	}
}

func TestStationaryShipIsNotJudged(t *testing.T) {
	m := modelWith(500, 1000)
	moored := ship
	moored.SpeedKn = 0
	if r := ScoreGap(m, moored, start.Add(2*time.Hour), nil); r.JudgedMinutes != 0 || r.Suspicious(0.01) {
		t.Fatalf("moored ships shouldn't be judged: %+v", r)
	}
}

func TestFeedOutageMeansNoJudgement(t *testing.T) {
	m := modelWith(500, 1000)
	// Feed down for minutes 1-30 of the silence
	down := func(minute int64) bool {
		offset := minute - start.Unix()/60
		return offset < 1 || offset > 30
	}
	r := ScoreGap(m, ship, start.Add(60*time.Minute), down)
	if r.FeedDownMinutes != 30 || r.Suspicious(0.01) {
		t.Fatalf("a 30-minute feed outage should block judgement: %+v", r)
	}
}

func TestShortFeedBlipIsTolerated(t *testing.T) {
	m := modelWith(500, 1000)
	// Feed down for just 2 minutes
	blip := func(minute int64) bool {
		offset := minute - start.Unix()/60
		return offset < 5 || offset > 6
	}
	r := ScoreGap(m, ship, start.Add(25*time.Minute), blip)
	if r.FeedDownMinutes != 2 || !r.Suspicious(0.01) {
		t.Fatalf("a 2-minute blip shouldn't block judgement: %+v", r)
	}
}
