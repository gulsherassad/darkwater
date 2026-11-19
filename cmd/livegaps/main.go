// Command livegaps measures how far apart the live feed's fixes are per ship,
// so dense DMA tracks can be thinned to look like the feed (see track.Thin).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/replay"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const maxGap = 2 * time.Hour // longer = feed outage or ship left the area, not a reporting gap

func percentile(xs []int64, p float64) int64 {
	return xs[int(p*float64(len(xs)-1))]
}

func main() {
	dir := flag.String("dir", "data/raw-server", "live recordings (*.jsonl)")
	out := flag.String("out", "data/ml/live-gaps.csv", "where to save the gaps")
	flag.Parse()

	files, err := replay.Files(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if len(files) == 0 {
		log.Fatalf("no recordings in %s", *dir)
	}

	// Same path as the live feed: replay.ReadFile validates, store.Update returns the gap
	store := tracker.New()
	var gaps []int64
	positions, tooLong := 0, 0
	for _, path := range files {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			positions++
			gap, ok := store.Update(p)
			if !ok || gap == 0 {
				return // stale/duplicate, or the ship's first fix
			}
			if gap > maxGap {
				tooLong++
				return
			}
			gaps = append(gaps, int64(gap.Seconds()))
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	if len(gaps) == 0 {
		log.Fatal("no gaps found")
	}

	// Save in recording order (not sorted), one gap in seconds per line
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "gap_sec")
	for _, g := range gaps {
		fmt.Fprintln(w, g)
	}
	if err := w.Flush(); err != nil {
		log.Fatal(err)
	}
	f.Close()

	sorted := append([]int64(nil), gaps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	fmt.Printf("%d files, %d positions, %d ships\n", len(files), positions, store.Len())
	fmt.Printf("%d gaps (ignored %d longer than %s)\n", len(gaps), tooLong, maxGap)
	for _, q := range []struct {
		name string
		p    float64
	}{{"p10", 0.1}, {"p25", 0.25}, {"median", 0.5}, {"p75", 0.75}, {"p90", 0.9}} {
		g := percentile(sorted, q.p)
		fmt.Printf("  %-6s %5d s  (%.1f min)\n", q.name, g, float64(g)/60)
	}
	fmt.Printf("saved %s\n", *out)
}
