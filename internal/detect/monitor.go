package detect

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const (
	LevelWatch = "watch" // unusual: keep an eye on it
	LevelAlert = "alert" // genuinely unlikely to be bad reception
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
	ProjLat       float64   `json:"projLat"` // where it probably is now
	ProjLon       float64   `json:"projLon"`
}

// Monitor re-scores every ship on a timer and keeps the current alert list.
type Monitor struct {
	model      *reception.Model
	store      *tracker.Store
	alive      FeedAlive
	WatchAlpha float64
	AlertAlpha float64

	mu     sync.RWMutex
	alerts []Alert
}

func NewMonitor(m *reception.Model, store *tracker.Store, alive FeedAlive) *Monitor {
	return &Monitor{model: m, store: store, alive: alive, WatchAlpha: 0.01, AlertAlpha: 0.001}
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
			LastSeen: p.Time, LastLat: p.Lat, LastLon: p.Lon, ProjLat: projLat, ProjLon: projLon,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chance < out[j].Chance })
	return out, judged
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
			alerts, judged := mo.Scan(time.Now())
			mo.mu.Lock()
			mo.alerts = alerts
			mo.mu.Unlock()

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
