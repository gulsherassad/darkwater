package main

import (
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/replay"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./cmd/spoofscan <recording.jsonl>... (one source, in time order)")
	}

	store := tracker.New()
	d := detect.NewSpoofDetector()

	type tally struct {
		name      string
		teleports int
		clashes   int
		maxKn     float64
	}
	byShip := map[int64]*tally{}
	d.OnEvent = func(e detect.SpoofEvent) {
		t := byShip[e.MMSI]
		if t == nil {
			t = &tally{name: e.Name}
			byShip[e.MMSI] = t
		}
		switch e.Kind {
		case detect.SpoofTeleport:
			t.teleports++
			t.maxKn = max(t.maxKn, e.ImpliedKn)
		case detect.SpoofClash:
			t.clashes++
		}
	}

	positions := 0
	for _, path := range os.Args[1:] {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			if _, ok := store.Update(p); !ok {
				return // stale or duplicate: same rule as live
			}
			positions++
			d.Observe(p)
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	totalTele, totalClash := 0, 0
	type row struct {
		mmsi int64
		t    *tally
	}
	var rows []row
	for m, t := range byShip {
		totalTele += t.teleports
		totalClash += t.clashes
		rows = append(rows, row{m, t})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].t.teleports > rows[j].t.teleports })

	fmt.Printf("%d positions from %d ships\n", positions, store.Len())
	fmt.Printf("teleports: %d from %d ships, identity clashes: %d\n\n", totalTele, len(byShip), totalClash)
	fmt.Printf("%-22s %10s %10s %8s %12s\n", "ship", "MMSI", "teleports", "clashes", "max speed")
	for i, r := range rows {
		if i == 15 {
			fmt.Printf("... and %d more\n", len(rows)-15)
			break
		}
		name := r.t.name
		if name == "" {
			name = "(unknown)"
		}
		fmt.Printf("%-22s %10d %10d %8d %9.0f kn\n", name, r.mmsi, r.t.teleports, r.t.clashes, r.t.maxKn)
	}
}
