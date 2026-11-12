package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/dma"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/replay"
)

const outPath = "data/grid/cells-res6.csv"

type cellStats struct {
	dmaMsgs, feedMsgs   int
	dmaShips, feedShips map[int64]struct{}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/cells <dma.csv> <recording.jsonl>...")
		os.Exit(2)
	}

	cells := map[h3.Cell]*cellStats{}
	get := func(c h3.Cell) *cellStats {
		s := cells[c]
		if s == nil {
			s = &cellStats{dmaShips: map[int64]struct{}{}, feedShips: map[int64]struct{}{}}
			cells[c] = s
		}
		return s
	}

	// 1) Feed recordings
	for _, path := range os.Args[2:] {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			if !grid.InDenmark(p) {
				return
			}
			s := get(grid.CellOf(p))
			s.feedMsgs++
			s.feedShips[p.MMSI] = struct{}{}
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// 2) DMA file, streamed, skipping duplicates
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	rd, err := dma.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	last := map[int64]time.Time{}
	for {
		p, reason, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if reason != "" || p.Validate() != "" || !grid.InDenmark(p) {
			continue
		}
		if prev, seen := last[p.MMSI]; seen && !p.Time.After(prev) {
			continue // duplicate or out of order
		}
		last[p.MMSI] = p.Time

		s := get(grid.CellOf(p))
		s.dmaMsgs++
		s.dmaShips[p.MMSI] = struct{}{}
	}

	// 3) Summary: where does each source have coverage?
	var both, dmaOnly, feedOnly int
	for _, s := range cells {
		switch {
		case s.dmaMsgs > 0 && s.feedMsgs > 0:
			both++
		case s.dmaMsgs > 0:
			dmaOnly++
		default:
			feedOnly++
		}
	}
	fmt.Printf("cells with data: %d (both %d, DMA only %d, feed only %d)\n",
		len(cells), both, dmaOnly, feedOnly)

	type row struct {
		cell h3.Cell
		s    *cellStats
	}
	var rows []row
	for c, s := range cells {
		rows = append(rows, row{c, s})
	}
	sort.Slice(rows, func(i, j int) bool { return len(rows[i].s.dmaShips) > len(rows[j].s.dmaShips) })

	fmt.Println("\nbusiest cells (by distinct ships DMA saw):")
	fmt.Println("  cell              DMA ships  feed ships")
	for i := 0; i < 10 && i < len(rows); i++ {
		fmt.Printf("  %s   %6d   %8d\n", rows[i].cell, len(rows[i].s.dmaShips), len(rows[i].s.feedShips))
	}

	// 4) Save every cell to CSV for the next steps
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		log.Fatal(err)
	}
	out, err := os.Create(outPath)
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	w := csv.NewWriter(out)
	w.Write([]string{"cell", "dma_msgs", "dma_ships", "feed_msgs", "feed_ships"})
	for _, r := range rows {
		w.Write([]string{
			r.cell.String(),
			strconv.Itoa(r.s.dmaMsgs),
			strconv.Itoa(len(r.s.dmaShips)),
			strconv.Itoa(r.s.feedMsgs),
			strconv.Itoa(len(r.s.feedShips)),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nwrote %d cells to %s\n", len(rows), outPath)
}
