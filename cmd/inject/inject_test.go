package main

import (
	"math"
	"testing"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/presence"
	"github.com/gulsherassad/darkwater/internal/reception"
)

var t0 = time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)

// A slow ship heard 5 minutes before the switch-off time t0.
var lastFix = ais.Position{MMSI: 219000001, Lat: 55.7, Lon: 12.6, SpeedKn: 1.5, CourseDeg: 90, Time: t0.Add(-5 * time.Minute)}

// Every cell along the ship's projected path hears ~50% of minutes, so
// 10 silent minutes = 0.5^10 ≈ 0.00098 < α 0.001.
func halfModel() *reception.Model {
	counts := map[h3.Cell]reception.Counts{}
	for m := 0.0; m <= 400; m++ {
		lat, lon := detect.Project(lastFix, m)
		counts[grid.CellOf(ais.Position{Lat: lat, Lon: lon})] = reception.Counts{Present: 1000, Heard: 500}
	}
	return reception.Fit(counts, 20)
}

func always(int64) bool { return true }

var minute0 = t0.Unix() / 60

func TestWatchCountsSilenceBeforeSwitchOff(t *testing.T) {
	o := Watch(halfModel(), lastFix, t0, watchSteps, always, minute0+1000, []float64{0.001}, []int{15})
	// Silent 5 min at T, needs 10: flagged 5 min after T. Naive 15 min: 10 min after T.
	if o.Alpha[0] != 5 || o.Naive[0] != 10 {
		t.Fatalf("want α at minute 5, naive at 10; got %v / %v", o.Alpha, o.Naive)
	}
	if o.Censored {
		t.Fatal("watch ended because every rule fired, not censored")
	}
}

func TestWatchCensoredAtEndOfFeed(t *testing.T) {
	// Feed data ends 3 minutes after T: minutes 0..3 watched, no detection yet.
	o := Watch(halfModel(), lastFix, t0, watchSteps, always, minute0+3, []float64{0.001}, []int{15})
	if !o.Censored || o.Outage || o.Watched != 4 || o.Alpha[0] != notDetected {
		t.Fatalf("want censored after 4 minutes, undetected: %+v", o)
	}
}

func TestWatchCensoredByFeedOutage(t *testing.T) {
	// Feed down from T+1 for 10 minutes: once more than MaxFeedDownMin are down, judging stops.
	alive := func(m int64) bool { return m <= minute0 || m > minute0+10 }
	o := Watch(halfModel(), lastFix, t0, watchSteps, alive, minute0+1000, []float64{0.001}, []int{60})
	if !o.Censored || !o.Outage {
		t.Fatalf("feed outage should censor the watch: %+v", o)
	}
	if o.Watched != detect.MaxFeedDownMin+1 {
		t.Fatalf("want %d minutes watched before the outage rule kicked in, got %d", detect.MaxFeedDownMin+1, o.Watched)
	}
}

func TestWatchStationaryLastFixIsNeverJudged(t *testing.T) {
	still := lastFix
	still.SpeedKn = 0.5 // below detect.MinSpeedKn
	o := Watch(halfModel(), still, t0, watchSteps, always, minute0+1000, []float64{0.05}, []int{15})
	if o.Alpha[0] != notDetected || o.Censored || o.Watched != watchSteps {
		t.Fatalf("model should never flag (watched the full 6 h, a miss): %+v", o)
	}
	if o.Naive[0] != 10 {
		t.Fatalf("naive rule ignores speed: want minute 10, got %d", o.Naive[0])
	}
}

// ScoreGap doesn't count outages for slow ships; the watch window must still end there.
func TestWatchOutageCensorsSlowShipsToo(t *testing.T) {
	still := lastFix
	still.SpeedKn = 0
	alive := func(m int64) bool { return m <= minute0 || m > minute0+30 }
	o := Watch(halfModel(), still, t0, watchSteps, alive, minute0+1000, []float64{0.05}, []int{60})
	if !o.Outage || o.Watched != detect.MaxFeedDownMin+1 || o.Naive[0] != notDetected {
		t.Fatalf("slow ship's watch must end at the outage too (naive can't fire during it): %+v", o)
	}
}

func TestLastFixAtOrBefore(t *testing.T) {
	fixes := []ais.Position{{Time: t0.Add(-10 * time.Minute)}, {Time: t0}, {Time: t0.Add(time.Minute)}}
	if p, ok := lastFixAtOrBefore(fixes, t0); !ok || !p.Time.Equal(t0) {
		t.Fatal("a fix exactly at T counts as before")
	}
	if _, ok := lastFixAtOrBefore(fixes, t0.Add(-11*time.Minute)); ok {
		t.Fatal("no fix before T: nothing for the detector to know")
	}
}

func TestCandidates(t *testing.T) {
	// Ship present minutes 0..200 at 10 kn, except 2 kn at minute 5.
	truth := map[int64]presence.Fix{}
	for m := int64(0); m <= 200; m++ {
		truth[m] = presence.Fix{Minute: m, SpeedKn: 10}
	}
	truth[5] = presence.Fix{Minute: 5, SpeedKn: 2}
	fixes := []ais.Position{{Time: time.Unix(3*60, 0)}} // first heard at minute 3
	alive := func(m int64) bool { return m <= 50 }

	got := candidates(truth, fixes, alive, 0, 200)
	// From 3 (first heard) to 50 (feed alive; 80 would be the 2-hour limit), minus minute 5
	if len(got) != 47 || got[0] != 3 || got[len(got)-1] != 50 {
		t.Fatalf("want minutes 3..50 without 5 (47), got %d: %v", len(got), got)
	}
	for _, m := range got {
		if m == 5 {
			t.Fatal("minute 5 is too slow")
		}
	}
	delete(truth, 130) // ship leaves DMA's view at 130: switch-offs after minute 9 can't be confirmed for 2 h
	if got := candidates(truth, fixes, alive, 0, 200); len(got) != 6 {
		t.Fatalf("want minutes 3,4,6,7,8,9, got %v", got)
	}
	if pick(got, 1, 219000001) != pick(got, 1, 219000001) {
		t.Fatal("pick must be reproducible")
	}
}

func TestControlGap(t *testing.T) {
	truth := map[int64]presence.Fix{}
	for m := int64(0); m <= 10; m++ {
		truth[m] = presence.Fix{Minute: m, SpeedKn: 5}
	}
	a, b := ais.Position{Time: time.Unix(0, 0)}, ais.Position{Time: time.Unix(10*60+30, 0)}
	if !controlGap(truth, a, b) || controlSteps(a, b) != 10 {
		t.Fatalf("present and moving throughout: fair control with 10 silent minutes (got %d)", controlSteps(a, b))
	}
	truth[4] = presence.Fix{Minute: 4, SpeedKn: 0.5}
	if controlGap(truth, a, b) {
		t.Fatal("ship stopped during the gap: not a fair control")
	}
}

func TestSurvival(t *testing.T) {
	var s survival
	s.add(10, 0)           // detected at 10
	s.add(20, 0)           // detected at 20
	s.add(notDetected, 6)  // watched 0..5, censored
	s.add(notDetected, 31) // watched 0..30, censored
	s.add(notDetected, 0)  // never watched: ignored
	// At 10: 3 at risk (10, 20, 30), 1 detected → 1/3
	rate, n, ok := s.within(15)
	if !ok || math.Abs(rate-1.0/3) > 1e-9 || n != 3 {
		t.Fatalf("within 15: want 1/3 from 3, got %.3f from %d (ok %v)", rate, n, ok)
	}
	// At 20: 2 at risk, 1 detected → S = 2/3 · 1/2 = 1/3 → median 20
	if m, ok := s.median(); !ok || m != 20 {
		t.Fatalf("median: want 20, got %d (%v)", m, ok)
	}
	if _, _, ok := s.within(60); ok {
		t.Fatal("nobody watched for 60 min: rate must be n/a, not extrapolated")
	}

	// Everyone detected early: the rate later on is known (100%) even with nobody left to watch
	var all survival
	all.add(3, 0)
	all.add(7, 0)
	if r, _, ok := all.within(60); !ok || r != 1 {
		t.Fatalf("all detected by 7 min: want 100%% within 60, got %.2f (%v)", r, ok)
	}
}
