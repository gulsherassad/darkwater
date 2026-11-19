package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/replay"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

func label(name string, mmsi int64) string {
	if name == "" {
		return fmt.Sprint(mmsi)
	}
	return name
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./cmd/encscan <recording.jsonl>... (one source, in time order)")
	}
	files := os.Args[1:]

	// Pass 1: learn where ships moor, so ports can be ignored
	mask := detect.NewPortMask()
	for _, path := range files {
		if _, err := replay.ReadFile(path, func(p ais.Position) {
			if p.Validate() == "" {
				mask.Add(p)
			}
		}); err != nil {
			log.Fatal(err)
		}
	}
	ports := mask.Finish(3)

	// Pass 2: replay, scanning once per minute of message time
	store := tracker.New()
	d := detect.NewEncounterDetector()
	d.Masked = mask.Masked
	var events []detect.EncounterEvent
	d.OnEvent = func(e detect.EncounterEvent) { events = append(events, e) }

	var lastScan time.Time
	scans, candidates := 0, 0
	for _, path := range files {
		if _, err := replay.ReadFile(path, func(p ais.Position) {
			if p.Validate() != "" {
				return
			}
			if _, ok := store.Update(p); !ok {
				return
			}
			if p.Time.Sub(lastScan) >= time.Minute {
				candidates += d.Scan(p.Time, store.Snapshot())
				scans++
				lastScan = p.Time
			}
		}); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Printf("%d port squares masked\n", ports)
	fmt.Printf("%d scans, %.0f candidate ships per scan (slow, fresh, outside ports)\n\n",
		scans, float64(candidates)/float64(max(scans, 1)))

	starts, ends := 0, 0
	for _, e := range events {
		if e.Kind == detect.EncounterStart {
			starts++
		} else {
			ends++
		}
		fmt.Printf("%s  %-15s %-20s %-20s %4.0f min  %.4f,%.4f\n",
			e.Time.Format("01-02 15:04"), e.Kind,
			label(e.Name1, e.MMSI1), label(e.Name2, e.MMSI2), e.Minutes, e.Lat, e.Lon)
	}
	fmt.Printf("\n%d encounters started, %d ended, %d still open at end of data\n", starts, ends, starts-ends)
}
