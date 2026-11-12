package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/gulsherassad/darkwater/internal/dma"
)

// Gap buckets: how long between two messages from the same ship
var buckets = []time.Duration{
	10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
	5 * time.Minute, 15 * time.Minute, time.Hour,
}
var labels = []string{"<10s", "10-30s", "30s-1m", "1-2m", "2-5m", "5-15m", "15m-1h", ">1h"}

func bucketFor(gap time.Duration) int {
	for i, b := range buckets {
		if gap < b {
			return i
		}
	}
	return len(buckets)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/dmastats <file.csv>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	rd, err := dma.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}

	start := time.Now()
	rows, kept, totalGaps := 0, 0, 0
	skipped := map[string]int{}
	last := map[int64]time.Time{} // MMSI -> time of its previous message
	counts := make([]int, len(labels))
	var first, latest time.Time

	for {
		p, reason, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		rows++
		if rows%2_000_000 == 0 {
			log.Printf("%dM rows...", rows/1_000_000)
		}

		if reason == "" {
			reason = p.Validate() // same bouncer as the live feed
		}
		if reason != "" {
			skipped[reason]++
			continue
		}

		if prev, seen := last[p.MMSI]; seen {
			gap := p.Time.Sub(prev)
			if gap < 0 {
				skipped["out_of_order"]++
				continue
			}
			if gap == 0 {
				skipped["duplicate"]++
				continue
			}
			counts[bucketFor(gap)]++
			totalGaps++
		}
		last[p.MMSI] = p.Time
		kept++

		if first.IsZero() || p.Time.Before(first) {
			first = p.Time
		}
		if p.Time.After(latest) {
			latest = p.Time
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\nrows:     %d in %s (%.0f rows/s)\n", rows, elapsed.Round(time.Second), float64(rows)/elapsed.Seconds())
	fmt.Printf("kept:     %d\n", kept)
	fmt.Printf("ships:    %d\n", len(last))
	fmt.Printf("time:     %s -> %s\n", first.Format(time.RFC3339), latest.Format(time.RFC3339))
	fmt.Printf("skipped:  %v\n", skipped)

	fmt.Println("\nTime between messages from the same ship:")
	cum := 0.0
	for i, label := range labels {
		pct := 100 * float64(counts[i]) / float64(totalGaps)
		cum += pct
		fmt.Printf("  %-7s %6.2f%%   (cumulative %6.2f%%)\n", label, pct, cum)
	}

	if len(rd.UnknownStatus) > 0 {
		fmt.Printf("\nunrecognised status labels: %v\n", rd.UnknownStatus)
	}
}
