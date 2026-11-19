package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/presence"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/replay"
)

const (
	priorStrength = 20.0
	minCellData   = 30 // ship-minutes before a cell counts in the spread stats
	maxInterpMin  = presence.MaxInterpMin
	outPath       = "data/grid/reception-res6.csv"
)

var naiveMinutes = []int{5, 10, 15, 20, 30}
var alphas = []float64{0.05, 0.01, 0.001}

type key = presence.Key

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

// minutesToFlag: how long a ship must be silent in a cell with hearing
// chance p before the model flags it at level alpha.
func minutesToFlag(p, alpha float64) float64 {
	return math.Ceil(math.Log(alpha) / math.Log(1-p))
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/reception <dma.csv> <recording.jsonl>...")
		os.Exit(2)
	}

	// 1) Feed: which ships it heard in which minute, and which minutes it was alive
	heard := map[key]bool{}
	alive := map[int64]bool{}
	var firstAlive, lastAlive int64 = math.MaxInt64, 0
	for _, path := range os.Args[2:] {
		_, err := replay.ReadFile(path, func(p ais.Position) {
			if !grid.InDenmark(p) {
				return
			}
			m := p.Time.Unix() / 60
			heard[key{MMSI: p.MMSI, Minute: m}] = true
			alive[m] = true
			firstAlive = min(firstAlive, m)
			lastAlive = max(lastAlive, m)
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// 2) DMA: one fix per ship per minute, around the feed's alive period
	fixes, err := presence.LoadDMA(os.Args[1], firstAlive-maxInterpMin, lastAlive+maxInterpMin)
	if err != nil {
		log.Fatal(err)
	}

	// 3) Presence: DMA's own fixes, plus interpolated minutes inside short DMA gaps
	present := map[key]h3.Cell{}
	observed, interpolated := 0, 0
	add := func(mmsi, m int64, lat, lon float64, isFix bool) {
		if !alive[m] {
			return // feed wasn't recording: silence here says nothing
		}
		present[key{MMSI: mmsi, Minute: m}] = grid.CellOf(ais.Position{Lat: lat, Lon: lon})
		if isFix {
			observed++
		} else {
			interpolated++
		}
	}
	presence.Walk(fixes, func(mmsi int64, f presence.Fix, observed bool) {
		add(mmsi, f.Minute, f.Lat, f.Lon, observed)
	})

	// 4) Fit on one random half of ships; keep counts for all ships for the saved model
	fitCounts := map[h3.Cell]reception.Counts{}
	allCounts := map[h3.Cell]reception.Counts{}
	evalShips := map[int64][]int64{} // other half -> minutes present
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

		if presence.InFitSet(k.MMSI) {
			fc := fitCounts[c]
			fc.Present++
			fc.Heard += h
			fitCounts[c] = fc
			fitMinutes++
		} else {
			evalShips[k.MMSI] = append(evalShips[k.MMSI], k.Minute)
		}
	}
	model := reception.Fit(fitCounts, priorStrength)

	// 5) Find feed gaps on eval ships, while DMA confirms they were still there
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
				afterHeard, silent = false, nil // lost the ship: can't judge this stretch
			}
			k := key{MMSI: mmsi, Minute: m}
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

	// 6) Report
	fmt.Printf("feed alive: %d minutes\n", len(alive))
	fmt.Printf("ship-minutes present: %d (%d observed by DMA, %d interpolated across DMA gaps ≤ %d min)\n",
		len(present), observed, interpolated, maxInterpMin)
	fmt.Printf("split: fit %d, eval %d\n", fitMinutes, evalMinutes)
	fmt.Printf("overall chance the feed hears a present ship in a given minute: %.1f%% (≈ 1 message every %.1f min)\n",
		100*model.Global, 1/model.Global)

	var probs []float64
	for c, cnt := range fitCounts {
		if cnt.Present >= minCellData {
			probs = append(probs, model.Prob(c))
		}
	}
	fmt.Printf("\nsilent minutes needed before the model flags (%d cells with ≥ %d ship-minutes):\n", len(probs), minCellData)
	fmt.Printf("  %-17s %8s", "coverage", "hearing")
	for _, a := range alphas {
		fmt.Printf("  α=%-6g", a)
	}
	fmt.Println()
	for _, q := range []struct {
		label string
		p     float64
	}{{"poor (10th pct)", 0.1}, {"typical (median)", 0.5}, {"good (90th pct)", 0.9}} {
		p := percentile(probs, q.p)
		fmt.Printf("  %-17s %7.1f%%", q.label, 100*p)
		for _, a := range alphas {
			fmt.Printf("  %5.0f min", minutesToFlag(p, a))
		}
		fmt.Println()
	}

	hours := float64(evalMinutes) / 60
	fmt.Printf("\nFALSE ALARMS on eval ships (DMA confirms all were still present)\n")
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

	// 7) Save the model fitted on ALL ships, for the live detector
	if err := reception.Fit(allCounts, priorStrength).Save(outPath); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nsaved model (all ships) → %s\n", outPath)
}
