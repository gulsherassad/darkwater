package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/replay"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

func main() {
	modelPath := flag.String("model", "data/grid/reception-res6.csv", "reception model")
	alpha := flag.Float64("alpha", 0.01, "flag silences less likely than this")
	flag.Parse()
	if flag.NArg() == 0 {
		log.Fatal("usage: go run ./cmd/gapscan [-alpha 0.01] <recording.jsonl>...")
	}

	m, err := reception.Load(*modelPath, 20)
	if err != nil {
		log.Fatal(err)
	}

	// Replay recordings: latest position per ship, plus which minutes the feed was alive
	store := tracker.New()
	alive := map[int64]bool{}
	var now time.Time
	for _, path := range flag.Args() {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			store.Update(p)
			alive[p.Time.Unix()/60] = true
			if p.Time.After(now) {
				now = p.Time
			}
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	feedAlive := func(minute int64) bool { return alive[minute] }

	// Score every ship as of the end of the recording
	type flagged struct {
		p ais.Position
		r detect.GapResult
	}
	var hits []flagged
	ships, judged, lostInOutage := 0, 0, 0
	for _, p := range store.Snapshot() {
		ships++
		r := detect.ScoreGap(m, p, now, feedAlive)
		if r.FeedDownMinutes > detect.MaxFeedDownMin {
			lostInOutage++
		}
		if r.JudgedMinutes > 0 {
			judged++
		}
		if r.Suspicious(*alpha) {
			hits = append(hits, flagged{p, r})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].r.LogP < hits[j].r.LogP })

	fmt.Printf("as of %s: %d ships, %d judged, %d skipped (silence spans a feed outage), %d flagged at α=%g\n\n",
		now.Format(time.RFC3339), ships, judged, lostInOutage, len(hits), *alpha)
	fmt.Printf("%-22s %10s %8s %8s %12s\n", "ship", "MMSI", "silent", "judged", "chance")
	for i, h := range hits {
		if i == 15 {
			fmt.Printf("... and %d more\n", len(hits)-15)
			break
		}
		name := h.p.Name
		if name == "" {
			name = "(unknown)"
		}
		fmt.Printf("%-22s %10d %5d min %5d min %12.2g\n",
			name, h.p.MMSI, h.r.SilentMinutes, h.r.JudgedMinutes, h.r.Prob())
	}
}
