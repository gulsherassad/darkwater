package main

import (
	"math/rand/v2"
	"sort"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/presence"
	"github.com/gulsherassad/darkwater/internal/reception"
)

const (
	watchSteps   = int(detect.MaxSilence/time.Minute) + 1 // minutes 0..360 after the switch-off
	switchOffKn  = 3.0                                    // ship must be moving at least this fast at T
	stayAfterMin = 120                                    // and stay in DMA's view this long after T
	notDetected  = -1
)

// Outcome of watching one silence minute by minute.
type Outcome struct {
	Watched  int   // minutes scored (minute 0 .. Watched-1 after the start)
	Censored bool  // watch ended early: end of feed data or a feed outage (not a miss)
	Outage   bool  // ...and it was a feed outage (detector stops judging by design)
	Alpha    []int // per alpha: first minute flagged, or notDetected
	Naive    []int // per naive threshold: first minute flagged, or notDetected
}

// Watch scores a ship whose last feed fix is `last`, at start, start+1 min, ...
// for up to `steps` minutes, exactly as the live monitor would (detect.ScoreGap).
// It stops early at the end of the feed data (endMinute, Unix minutes), when a
// feed outage makes the silence unjudgeable (detect.MaxFeedDownMin), or once
// every rule has fired.
func Watch(m *reception.Model, last ais.Position, start time.Time, steps int,
	alive detect.FeedAlive, endMinute int64, alphas []float64, naive []int) Outcome {

	o := Outcome{Alpha: fill(len(alphas)), Naive: fill(len(naive))}
	for k := 0; k < steps; k++ {
		now := start.Add(time.Duration(k) * time.Minute)
		if now.Unix()/60 > endMinute {
			o.Censored = true
			return o
		}
		// The watch window depends on the feed only, for every ship and rule:
		// ScoreGap skips counting outages for slow ships, so count them here.
		if feedDown(last, now, alive) > detect.MaxFeedDownMin {
			o.Censored, o.Outage = true, true
			return o
		}
		r := detect.ScoreGap(m, last, now, alive)
		o.Watched = k + 1
		pending := false
		for i, a := range alphas {
			if o.Alpha[i] == notDetected && r.Suspicious(a) {
				o.Alpha[i] = k
			}
			pending = pending || o.Alpha[i] == notDetected
		}
		for i, n := range naive {
			if o.Naive[i] == notDetected && r.SilentMinutes >= n {
				o.Naive[i] = k
			}
			pending = pending || o.Naive[i] == notDetected
		}
		if !pending {
			return o // every rule has fired: nothing left to learn
		}
	}
	return o
}

// feedDown counts the minutes since `last` that our feed was down, over the
// same minutes detect.ScoreGap looks at.
func feedDown(last ais.Position, now time.Time, alive detect.FeedAlive) int {
	silent := min(now.Sub(last.Time), detect.MaxSilence)
	down := 0
	for i := 1; i <= int(silent/time.Minute); i++ {
		if !alive(last.Time.Add(time.Duration(i)*time.Minute).Unix() / 60) {
			down++
		}
	}
	return down
}

func fill(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = notDetected
	}
	return s
}

// lastFixAtOrBefore is what the detector would know if the ship switched off
// at t: its last real feed fix at or before t. fixes are oldest first.
func lastFixAtOrBefore(fixes []ais.Position, t time.Time) (ais.Position, bool) {
	i := sort.Search(len(fixes), func(i int) bool { return fixes[i].Time.After(t) })
	if i == 0 {
		return ais.Position{}, false
	}
	return fixes[i-1], true
}

// candidates lists the minutes at which a ship could switch off: per DMA it is
// moving >= switchOffKn and stays present every minute for stayAfterMin after;
// the feed was alive; and the feed had heard the ship at least once before.
func candidates(truth map[int64]presence.Fix, fixes []ais.Position, alive detect.FeedAlive, from, to int64) []int64 {
	if len(fixes) == 0 {
		return nil
	}
	firstHeard := fixes[0].Time.Unix() / 60
	var out []int64
	for m := max(from, firstHeard); m <= to; m++ {
		f, ok := truth[m]
		if !ok || f.SpeedKn < switchOffKn || !alive(m) {
			continue
		}
		stays := true
		for k := int64(1); k <= stayAfterMin; k++ {
			if _, ok := truth[m+k]; !ok {
				stays = false
				break
			}
		}
		if stays {
			out = append(out, m)
		}
	}
	return out
}

// pick chooses one switch-off minute, reproducibly for each (seed, ship).
func pick(cands []int64, seed, mmsi int64) int64 {
	rng := rand.New(rand.NewPCG(uint64(seed), uint64(mmsi)))
	return cands[rng.IntN(len(cands))]
}

// controlGap reports whether the feed gap between fixes a and b is a fair
// false-alarm test: DMA shows the ship present and moving (>= detect.MinSpeedKn)
// in every minute from a to b.
func controlGap(truth map[int64]presence.Fix, a, b ais.Position) bool {
	for m := a.Time.Unix() / 60; m <= b.Time.Unix()/60; m++ {
		f, ok := truth[m]
		if !ok || f.SpeedKn < detect.MinSpeedKn {
			return false
		}
	}
	return true
}

// controlSteps: minutes after a's fix that the ship is silent before b arrives.
func controlSteps(a, b ais.Position) int {
	n := 0
	for k := 1; a.Time.Add(time.Duration(k) * time.Minute).Before(b.Time); k++ {
		n++
	}
	return n
}

// ---- Detection curves with censoring (Kaplan–Meier) ----

// survival holds one rule's results: for each injection, the minute it was
// detected, or (if undetected) the last minute we watched it.
type survival struct {
	times    []int
	detected []bool
}

func (s *survival) add(detectedAt, watched int) {
	if detectedAt != notDetected {
		s.times, s.detected = append(s.times, detectedAt), append(s.detected, true)
		return
	}
	if watched == 0 {
		return // never watched at all: carries no information
	}
	s.times, s.detected = append(s.times, watched-1), append(s.detected, false)
}

// curve returns S(t), the Kaplan–Meier chance of NOT being detected by minute t,
// for t = 0..watchSteps-1.
func (s *survival) curve() []float64 {
	out := make([]float64, watchSteps)
	surv := 1.0
	for t := range out {
		atRisk, events := 0, 0
		for i, ti := range s.times {
			if ti >= t {
				atRisk++
				if ti == t && s.detected[i] {
					events++
				}
			}
		}
		if atRisk > 0 {
			surv *= 1 - float64(events)/float64(atRisk)
		}
		out[t] = surv
	}
	return out
}

// within: detection rate by minute x, and how many injections inform it
// (detected by x, or still under watch at x). ok is false when nothing was
// under watch at x and not everyone had been detected: the rate would be an
// extrapolation.
func (s *survival) within(x int) (rate float64, n int, ok bool) {
	atRisk := 0
	for i, ti := range s.times {
		switch {
		case s.detected[i] && ti <= x:
			n++
		case ti >= x:
			n++
			atRisk++
		}
	}
	if len(s.times) == 0 {
		return 0, 0, false
	}
	surv := s.curve()[x]
	if atRisk == 0 && surv > 0 {
		return 0, n, false
	}
	return 1 - surv, n, true
}

// median: first minute by which half the injections are detected (ok = false if never reached).
func (s *survival) median() (int, bool) {
	for t, v := range s.curve() {
		if v <= 0.5 {
			return t, true
		}
	}
	return 0, false
}
