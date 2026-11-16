package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/dma"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/replay"
)

const (
	priorStrength = 20.0
	minCellData   = 30 // ship-minutes before a cell counts in the spread stats
	outPath       = "data/grid/reception-res6.csv"
)

var naiveMinutes = []int{5, 10, 15, 20, 30}
var alphas = []float64{0.05, 0.01, 0.001}

type key struct {
	mmsi   int64
	minute int64
}

type gap struct {
	minutes int
	logP    float64
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[int(p*float64(len(s)-1))]
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/reception <dma.csv> <recording.jsonl>...")
		os.Exit(2)
	}

	// 1) Feed: which ships it heard in which minute, and which minutes it was alive
	heard := map[key]bool{}
	alive := map[int64]bool{}
	for _, path := range os.Args[2:] {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			if !grid.InDenmark(p) {
				return
			}
			m := p.Time.Unix() / 60
			heard[key{p.MMSI, m}] = true
			alive[m] = true
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// 2) DMA: which ships were present in which cell, during minutes the feed was alive
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	rd, err := dma.NewReader(f)
	if err != nil {
		log.Fatal(err)
	}
	present := map[key]h3.Cell{}
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
		m := p.Time.Unix() / 60
		if !alive[m] {
			continue // feed wasn't recording: silence here says nothing
		}
		k := key{p.MMSI, m}
		if _, seen := present[k]; !seen {
			present[k] = grid.CellOf(p) // first position in that minute
		}
	}

	// 3) Fit on even MMSIs; keep counts for all ships for the saved model
	fitCounts := map[h3.Cell]reception.Counts{}
	allCounts := map[h3.Cell]reception.Counts{}
	evalShips := map[int64][]int64{} // odd MMSIs -> minutes present
	fitMinutes := 0
	for k, c := range present {
		h := 0
		if heard[k] {
			h = 1
		}
		a := allCounts[c]
		a.Present++
		a.Heard += h
		allCounts[c] = a

		if k.mmsi%2 == 0 {
			fc := fitCounts[c]
			fc.Present++
			fc.Heard += h
			fitCounts[c] = fc
			fitMinutes++
		} else {
			evalShips[k.mmsi] = append(evalShips[k.mmsi], k.minute)
		}
	}
	model := reception.Fit(fitCounts, priorStrength)

	// 4) Find feed gaps on odd-MMSI ships, while DMA confirms they kept transmitting
	var gaps []gap
	evalMinutes := 0
	for mmsi, minutes := range evalShips {
		sort.Slice(minutes, func(i, j int) bool { return minutes[i] < minutes[j] })
		evalMinutes += len(minutes)

		afterHeard := false // only count gaps that start after a heard minute
		var silent []h3.Cell
		var prev int64 = -1
		for _, m := range minutes {
			if prev >= 0 && m != prev+1 {
				afterHeard, silent = false, nil // DMA lost the ship: can't judge this stretch
			}
			k := key{mmsi, m}
			if heard[k] {
				if afterHeard && len(silent) > 0 {
					gaps = append(gaps, gap{len(silent), model.SilenceLogProb(silent)})
				}
				afterHeard, silent = true, nil
			} else if afterHeard {
				silent = append(silent, present[k])
			}
			prev = m
		}
	}

	// 5) Report
	fmt.Printf("feed alive: %d minutes\n", len(alive))
	fmt.Printf("ship-minutes in DMA during those minutes: %d (fit %d, eval %d)\n",
		len(present), fitMinutes, evalMinutes)
	fmt.Printf("overall chance the feed hears a present ship in a given minute: %.1f%% (≈ 1 message every %.1f min)\n",
		100*model.Global, 1/model.Global)

	var probs []float64
	for c, cnt := range fitCounts {
		if cnt.Present >= minCellData {
			probs = append(probs, model.Prob(c))
		}
	}
	fmt.Printf("\nper-cell hearing chance (%d cells with ≥ %d ship-minutes):\n", len(probs), minCellData)
	for _, q := range []struct {
		label string
		p     float64
	}{{"poor (10th pct)", 0.1}, {"typical (median)", 0.5}, {"good (90th pct)", 0.9}} {
		p := percentile(probs, q.p)
		need := math.Ceil(math.Log(0.01) / math.Log(1-p))
		fmt.Printf("  %-17s %5.1f%%  → needs %4.0f silent minutes to flag at α=0.01\n", q.label, 100*p, need)
	}

	hours := float64(evalMinutes) / 60
	fmt.Printf("\nFALSE ALARMS on eval ships (DMA confirms all were transmitting)\n")
	fmt.Printf("%d feed gaps over %.0f ship-hours\n\n", len(gaps), hours)
	fmt.Printf("  %-34s %8s %12s\n", "rule", "alarms", "per 100 h")
	for _, n := range naiveMinutes {
		c := 0
		for _, g := range gaps {
			if g.minutes >= n {
				c++
			}
		}
		fmt.Printf("  %-34s %8d %12.1f\n", fmt.Sprintf("naive: silent ≥ %d min", n), c, 100*float64(c)/hours)
	}
	for _, a := range alphas {
		c := 0
		for _, g := range gaps {
			if g.logP < math.Log(a) {
				c++
			}
		}
		fmt.Printf("  %-34s %8d %12.1f\n", fmt.Sprintf("reception model: α = %g", a), c, 100*float64(c)/hours)
	}

	// 6) Save the model fitted on ALL ships, for the live detector
	if err := reception.Fit(allCounts, priorStrength).Save(outPath); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nsaved model (all ships) → %s\n", outPath)
}
