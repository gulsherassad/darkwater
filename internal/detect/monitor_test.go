package detect

import (
	"testing"
	"time"

	"github.com/gulsherassad/darkwater/internal/tracker"
)

func TestMonitorLevels(t *testing.T) {
	store := tracker.New()
	store.Update(ship)                                 // heard once, at `start`, then silent
	mo := NewMonitor(modelWith(500, 1000), store, nil) // ~50% hearing chance per minute

	// 8 silent minutes: 0.5^8 ≈ 0.004 → below 0.01 (watch), above 0.001 (not alert)
	alerts, judged := mo.Scan(start.Add(8 * time.Minute))
	if judged != 1 || len(alerts) != 1 || alerts[0].Level != LevelWatch {
		t.Fatalf("8 min: want 1 watch, got judged=%d %+v", judged, alerts)
	}

	// 30 silent minutes: 0.5^30 ≈ 1e-9 → alert
	alerts, _ = mo.Scan(start.Add(30 * time.Minute))
	if len(alerts) != 1 || alerts[0].Level != LevelAlert {
		t.Fatalf("30 min: want 1 alert, got %+v", alerts)
	}

	// 2 silent minutes: 0.25 → nothing
	if alerts, _ = mo.Scan(start.Add(2 * time.Minute)); len(alerts) != 0 {
		t.Fatalf("2 min: want no alerts, got %+v", alerts)
	}
}

func TestMonitorEvents(t *testing.T) {
	store := tracker.New()
	store.Update(ship)
	mo := NewMonitor(modelWith(500, 1000), store, nil)

	// Silent 8 min → opens as watch
	now := start.Add(8 * time.Minute)
	alerts, _ := mo.Scan(now)
	if ev := mo.Diff(now, alerts); len(ev) != 1 || ev[0].Kind != EventOpen {
		t.Fatalf("want 1 open event, got %+v", ev)
	}

	// Silent 30 min → escalates to alert
	now = start.Add(30 * time.Minute)
	alerts, _ = mo.Scan(now)
	if ev := mo.Diff(now, alerts); len(ev) != 1 || ev[0].Kind != EventEscalate {
		t.Fatalf("want 1 escalate event, got %+v", ev)
	}

	// Heard again at 31 min, exactly where dead reckoning expected it
	back := ship
	back.Time = start.Add(31 * time.Minute)
	back.Lat, back.Lon = Project(ship, 31)
	store.Update(back)

	now = start.Add(32 * time.Minute)
	alerts, _ = mo.Scan(now)
	ev := mo.Diff(now, alerts)
	if len(ev) != 1 || ev[0].Kind != EventReappeared || ev[0].MissKm > 0.01 {
		t.Fatalf("want 1 reappeared event with ~0 km miss, got %+v", ev)
	}
}
