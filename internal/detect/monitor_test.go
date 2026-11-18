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
