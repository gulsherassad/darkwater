package detect

import (
	"sync"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/geo"
)

const (
	SpoofTeleport = "teleport"       // a jump needing an impossible speed
	SpoofClash    = "identity_clash" // repeated teleports: likely two transponders, one MMSI
	recentMax     = 50               // spoof events kept in memory for the API
)

// SpoofEvent is one suspicious jump, or a pattern of them.
type SpoofEvent struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	MMSI      int64     `json:"mmsi"`
	Name      string    `json:"name"`
	FromLat   float64   `json:"fromLat"`
	FromLon   float64   `json:"fromLon"`
	ToLat     float64   `json:"toLat"`
	ToLon     float64   `json:"toLon"`
	Km        float64   `json:"km"`
	Seconds   float64   `json:"seconds"`
	ImpliedKn float64   `json:"impliedKn"`
	Count     int       `json:"count,omitempty"` // teleports in the window (identity_clash only)
}

// SpoofDetector checks every incoming position against the ship's previous one.
type SpoofDetector struct {
	MaxKn       float64       // faster than this is impossible for a ship
	MinKm       float64       // ignore smaller jumps (GPS jitter)
	ClashCount  int           // this many teleports...
	ClashWindow time.Duration // ...within this window = identity clash
	OnEvent     func(SpoofEvent)

	mu        sync.Mutex
	last      map[int64]ais.Position
	teleports map[int64][]time.Time
	recent    []SpoofEvent
}

func NewSpoofDetector() *SpoofDetector {
	return &SpoofDetector{
		MaxKn: 50, MinKm: 1, ClashCount: 3, ClashWindow: time.Hour,
		last:      map[int64]ais.Position{},
		teleports: map[int64][]time.Time{},
	}
}

// Observe checks one accepted position. Safe to call from any goroutine.
func (d *SpoofDetector) Observe(p ais.Position) {
	d.mu.Lock()
	events := d.observeLocked(p)
	d.mu.Unlock()

	// Callbacks run AFTER unlocking: a slow callback can't stall the detector
	if d.OnEvent != nil {
		for _, e := range events {
			d.OnEvent(e)
		}
	}
}

func (d *SpoofDetector) observeLocked(p ais.Position) []SpoofEvent {
	prev, seen := d.last[p.MMSI]
	if seen && !p.Time.After(prev.Time) {
		return nil // not newer than what we have
	}
	d.last[p.MMSI] = p
	if !seen {
		return nil
	}

	dt := p.Time.Sub(prev.Time)
	km := geo.HaversineKm(prev.Lat, prev.Lon, p.Lat, p.Lon)
	if km < d.MinKm {
		return nil
	}
	kn := km / 1.852 / dt.Hours()
	if kn <= d.MaxKn {
		return nil
	}

	tele := SpoofEvent{
		Time: p.Time, Kind: SpoofTeleport, MMSI: p.MMSI, Name: p.Name,
		FromLat: prev.Lat, FromLon: prev.Lon, ToLat: p.Lat, ToLon: p.Lon,
		Km: km, Seconds: dt.Seconds(), ImpliedKn: kn,
	}
	events := []SpoofEvent{tele}

	// Count recent teleports for this MMSI; a pattern means an identity clash
	ts := append(d.teleports[p.MMSI], p.Time)
	cutoff := p.Time.Add(-d.ClashWindow)
	kept := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= d.ClashCount {
		clash := tele
		clash.Kind = SpoofClash
		clash.Count = len(kept)
		events = append(events, clash)
		kept = nil // start counting afresh, so one clash isn't reported every teleport
	}
	d.teleports[p.MMSI] = kept

	for _, e := range events {
		d.recent = append(d.recent, e)
	}
	if len(d.recent) > recentMax {
		d.recent = d.recent[len(d.recent)-recentMax:]
	}
	return events
}

// Recent returns the latest spoof events, oldest first.
func (d *SpoofDetector) Recent() []SpoofEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]SpoofEvent{}, d.recent...)
}
