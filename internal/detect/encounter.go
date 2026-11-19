package detect

import (
	"sort"
	"sync"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/geo"
)

const (
	EncounterStart = "encounter_start"
	EncounterEnd   = "encounter_end"
)

type EncounterEvent struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	MMSI1   int64     `json:"mmsi1"`
	Name1   string    `json:"name1"`
	MMSI2   int64     `json:"mmsi2"`
	Name2   string    `json:"name2"`
	Lat     float64   `json:"lat"`
	Lon     float64   `json:"lon"`
	Minutes float64   `json:"minutes"` // how long they've been together
}

type pairKey struct{ a, b int64 } // a < b, so each pair has exactly one key

type pairState struct {
	first, last  time.Time // first and latest scan that saw them together
	open         bool      // encounter_start already reported
	lat, lon     float64
	name1, name2 string
}

// EncounterDetector finds pairs of slow ships staying close together.
type EncounterDetector struct {
	MaxKm       float64       // closer than this = "together"
	MaxKn       float64       // both must be slower than this
	MaxAge      time.Duration // ignore ships whose last position is older
	MinDuration time.Duration // together this long = encounter
	EndAfter    time.Duration // apart (or unseen) this long = encounter over
	Masked      func(lat, lon float64) bool
	OnEvent     func(EncounterEvent)

	mu    sync.Mutex
	pairs map[pairKey]*pairState
}

func NewEncounterDetector() *EncounterDetector {
	return &EncounterDetector{
		MaxKm: 0.2, MaxKn: 3,
		MaxAge: 30 * time.Minute, MinDuration: 30 * time.Minute, EndAfter: 30 * time.Minute,
		pairs: map[pairKey]*pairState{},
	}
}

// Scan checks the latest position of every ship. Call it periodically.
// Returns how many ships were candidates (slow, fresh, outside ports).
func (d *EncounterDetector) Scan(now time.Time, ships []ais.Position) int {
	d.mu.Lock()
	n, events := d.scanLocked(now, ships)
	d.mu.Unlock()

	if d.OnEvent != nil {
		for _, e := range events {
			d.OnEvent(e)
		}
	}
	return n
}

func (d *EncounterDetector) scanLocked(now time.Time, ships []ais.Position) (int, []EncounterEvent) {
	// 1) Keep only slow, recently-seen ships outside ports
	var c []ais.Position
	for _, p := range ships {
		if p.SpeedKn < 0 || p.SpeedKn > d.MaxKn || p.NavStatus == navMoored {
			continue
		}
		if now.Sub(p.Time) > d.MaxAge {
			continue
		}
		if d.Masked != nil && d.Masked(p.Lat, p.Lon) {
			continue
		}
		c = append(c, p)
	}

	// 2) Sort by latitude: close ships are neighbours in this list
	sort.Slice(c, func(i, j int) bool { return c[i].Lat < c[j].Lat })
	maxDLat := d.MaxKm / 111.0 // 1° latitude ≈ 111 km

	var events []EncounterEvent
	for i := range c {
		for j := i + 1; j < len(c); j++ {
			if c[j].Lat-c[i].Lat > maxDLat {
				break // everyone further down the list is even further north
			}
			if geo.HaversineKm(c[i].Lat, c[i].Lon, c[j].Lat, c[j].Lon) > d.MaxKm {
				continue
			}
			a, b := c[i], c[j]
			if a.MMSI > b.MMSI {
				a, b = b, a
			}
			k := pairKey{a.MMSI, b.MMSI}
			s := d.pairs[k]
			if s == nil {
				s = &pairState{first: now}
				d.pairs[k] = s
			}
			s.last = now
			s.lat, s.lon = (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2
			s.name1, s.name2 = a.Name, b.Name

			if !s.open && now.Sub(s.first) >= d.MinDuration {
				s.open = true
				events = append(events, s.event(EncounterStart, now, k))
			}
		}
	}

	// 3) Pairs not seen together for a while: close them (deleting while ranging is fine in Go)
	for k, s := range d.pairs {
		if now.Sub(s.last) <= d.EndAfter {
			continue
		}
		if s.open {
			events = append(events, s.event(EncounterEnd, s.last, k))
		}
		delete(d.pairs, k)
	}
	return len(c), events
}

func (s *pairState) event(kind string, t time.Time, k pairKey) EncounterEvent {
	return EncounterEvent{
		Time: t, Kind: kind,
		MMSI1: k.a, Name1: s.name1, MMSI2: k.b, Name2: s.name2,
		Lat: s.lat, Lon: s.lon,
		Minutes: s.last.Sub(s.first).Minutes(),
	}
}
