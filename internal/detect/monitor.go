package detect

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/geo"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	LevelWatch = "watch" // unusual: keep an eye on it
	LevelAlert = "alert" // genuinely unlikely to be bad reception
)

// Event kinds, logged whenever a ship's alert status changes
const (
	EventOpen       = "open"       // newly flagged
	EventEscalate   = "escalate"   // watch -> alert
	EventDeescalate = "deescalate" // alert -> watch
	EventReappeared = "reappeared" // heard again: no longer silent
	EventCleared    = "cleared"    // stopped being judged (left coverage, feed outage)
)

// Alert is one suspicious ship, as the API and map see it.
type Alert struct {
	MMSI          int64     `json:"mmsi"`
	Name          string    `json:"name"`
	Level         string    `json:"level"`
	SilentMinutes int       `json:"silentMinutes"`
	JudgedMinutes int       `json:"judgedMinutes"`
	Chance        float64   `json:"chance"` // chance bad reception explains the silence
	LastSeen      time.Time `json:"lastSeen"`
	LastLat       float64   `json:"lastLat"`
	LastLon       float64   `json:"lastLon"`
	SpeedKn       float64   `json:"speedKn"`
	CourseDeg     float64   `json:"courseDeg"`
	ProjLat       float64   `json:"projLat"` // where it probably is now
	ProjLon       float64   `json:"projLon"`
}

// Event is one change in a ship's alert status.
type Event struct {
	Time  time.Time `json:"time"`
	Kind  string    `json:"kind"`
	Alert           // the alert's fields, flattened into the same JSON object
	// Only for "reappeared": where it turned up, and how far from the projection
	ReappearedLat float64 `json:"reappearedLat,omitempty"`
	ReappearedLon float64 `json:"reappearedLon,omitempty"`
	MissKm        float64 `json:"missKm,omitempty"`
}

// Monitor re-scores every ship on a timer and keeps the current alert list.
type Monitor struct {
	model      *reception.Model
	store      *tracker.Store
	alive      FeedAlive
	WatchAlpha float64
	AlertAlpha float64
	OnEvent    func(Event) // optional: called for every status change

	mu     sync.RWMutex
	alerts []Alert
	prev   map[int64]Alert // last scan's alerts, for diffing
}

func NewMonitor(m *reception.Model, store *tracker.Store, alive FeedAlive) *Monitor {
	return &Monitor{
		model: m, store: store, alive: alive,
		WatchAlpha: 0.01, AlertAlpha: 0.001,
		prev: map[int64]Alert{},
	}
}

// Scan scores every ship as of now. Returns alerts (most suspicious first)
// and how many ships were judged at all.
func (mo *Monitor) Scan(now time.Time) ([]Alert, int) {
	var out []Alert
	judged := 0
	for _, p := range mo.store.Snapshot() {
		r := ScoreGap(mo.model, p, now, mo.alive)
		if r.JudgedMinutes > 0 {
			judged++
		}
		level := ""
		switch {
		case r.Suspicious(mo.AlertAlpha):
			level = LevelAlert
		case r.Suspicious(mo.WatchAlpha):
			level = LevelWatch
		default:
			continue
		}
		projLat, projLon := Project(p, float64(r.SilentMinutes))
		out = append(out, Alert{
			MMSI: p.MMSI, Name: p.Name, Level: level,
			SilentMinutes: r.SilentMinutes, JudgedMinutes: r.JudgedMinutes, Chance: r.Prob(),
			LastSeen: p.Time, LastLat: p.Lat, LastLon: p.Lon,
			SpeedKn: p.SpeedKn, CourseDeg: p.CourseDeg,
			ProjLat: projLat, ProjLon: projLon,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chance < out[j].Chance })
	return out, judged
}

// Diff compares this scan's alerts with the previous scan's and returns
// what changed. It updates the remembered state.
func (mo *Monitor) Diff(now time.Time, alerts []Alert) []Event {
	cur := make(map[int64]Alert, len(alerts))
	var events []Event

	for _, a := range alerts {
		cur[a.MMSI] = a
		old, had := mo.prev[a.MMSI]
		switch {
		case !had:
			events = append(events, Event{Time: now, Kind: EventOpen, Alert: a})
		case old.Level != a.Level && a.Level == LevelAlert:
			events = append(events, Event{Time: now, Kind: EventEscalate, Alert: a})
		case old.Level != a.Level:
			events = append(events, Event{Time: now, Kind: EventDeescalate, Alert: a})
		}
	}

	for mmsi, old := range mo.prev {
		if _, still := cur[mmsi]; still {
			continue
		}
		e := Event{Time: now, Kind: EventCleared, Alert: old}
		if p, ok := mo.store.Get(mmsi); ok && p.Time.After(old.LastSeen) {
			// Heard again: compare where it turned up with where we projected it
			last := ais.Position{Lat: old.LastLat, Lon: old.LastLon, SpeedKn: old.SpeedKn, CourseDeg: old.CourseDeg}
			projLat, projLon := Project(last, p.Time.Sub(old.LastSeen).Minutes())
			e.Kind = EventReappeared
			e.ReappearedLat, e.ReappearedLon = p.Lat, p.Lon
			e.MissKm = geo.HaversineKm(projLat, projLon, p.Lat, p.Lon)
		}
		events = append(events, e)
	}

	mo.prev = cur
	return events
}

// Run scans every `every` until ctx is cancelled.
// onUpdate (optional) receives each new alert list.
func (mo *Monitor) Run(ctx context.Context, every time.Duration, onUpdate func([]Alert)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC() // store everything in UTC; convert only for display
			alerts, judged := mo.Scan(now)
			mo.mu.Lock()
			mo.alerts = alerts
			mo.mu.Unlock()

			for _, e := range mo.Diff(now, alerts) {
				if mo.OnEvent != nil {
					mo.OnEvent(e)
				}
			}

			watch := 0
			for _, a := range alerts {
				if a.Level == LevelWatch {
					watch++
				}
			}
			log.Printf("monitor: %d judged, %d watch, %d alert", judged, watch, len(alerts)-watch)
			if onUpdate != nil {
				onUpdate(alerts)
			}
		}
	}
}

// Current returns a copy of the latest alert list.
func (mo *Monitor) Current() []Alert {
	mo.mu.RLock()
	defer mo.mu.RUnlock()
	return append([]Alert{}, mo.alerts...)
}
