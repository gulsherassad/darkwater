package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/dma"
	"github.com/gulsherassad/darkwater/internal/replay"
)

func inDenmark(p ais.Position) bool {
	return p.Lat >= 54 && p.Lat <= 58.5 && p.Lon >= 7 && p.Lon <= 16
}

// near: within roughly 10 m of each other
func near(a, b ais.Position) bool {
	return math.Abs(a.Lat-b.Lat) < 0.0001 && math.Abs(a.Lon-b.Lon) < 0.0002
}

type key struct {
	mmsi int64
	t    int64
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/compare <dma.csv> <recording.jsonl>...")
		os.Exit(2)
	}

	// 1) Load the feed recordings into memory, grouped by ship (they're small)
	feed := map[int64][]ais.Position{}
	var start, end time.Time
	total := 0
	for _, path := range os.Args[2:] {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			feed[p.MMSI] = append(feed[p.MMSI], p)
			total++
			if start.IsZero() || p.Time.Before(start) {
				start = p.Time
			}
			if p.Time.After(end) {
				end = p.Time
			}
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	if total == 0 {
		log.Fatal("no positions found in the recordings")
	}
	fmt.Printf("feed: %d positions from %d ships, %s -> %s\n",
		total, len(feed), start.Format(time.RFC3339), end.Format(time.RFC3339))

	// 2) Stream the DMA file, matching it against the feed
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	rd, err := dma.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}

	offsets := map[time.Duration]int{} // DMA time minus feed time -> count
	matched := map[key]bool{}          // feed positions found in DMA
	dmaShips := map[int64]bool{}       // ships DMA saw during the feed's window

	for {
		p, reason, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if reason != "" || p.Validate() != "" {
			continue
		}

		if !p.Time.Before(start) && !p.Time.After(end) && inDenmark(p) {
			dmaShips[p.MMSI] = true
		}

		// Same ship, same spot, moving: probably the same broadcast
		for _, fp := range feed[p.MMSI] {
			if fp.SpeedKn < 2 || !near(p, fp) {
				continue
			}
			off := p.Time.Sub(fp.Time).Round(time.Second)
			if off < -2*time.Hour || off > 2*time.Hour {
				continue
			}
			offsets[off]++
			matched[key{fp.MMSI, fp.Time.UnixNano()}] = true
		}
	}

	// 3) Report
	moving := 0
	for _, ps := range feed {
		for _, p := range ps {
			if p.SpeedKn >= 2 {
				moving++
			}
		}
	}
	fmt.Printf("\nmoving feed positions found in DMA: %d of %d (%.1f%%)\n",
		len(matched), moving, 100*float64(len(matched))/float64(moving))

	type kv struct {
		off   time.Duration
		count int
	}
	var top []kv
	for o, c := range offsets {
		top = append(top, kv{o, c})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].count > top[j].count })
	fmt.Println("\nmost common clock offsets (DMA minus feed):")
	for i := 0; i < len(top) && i < 5; i++ {
		fmt.Printf("  %8s  %d matches\n", top[i].off, top[i].count)
	}

	both := 0
	for mmsi := range feed {
		if dmaShips[mmsi] {
			both++
		}
	}
	fmt.Printf("\nships seen during the feed window: DMA %d, feed %d, both %d (feed saw %.0f%% of DMA's ships)\n",
		len(dmaShips), len(feed), both, 100*float64(both)/float64(len(dmaShips)))
}
