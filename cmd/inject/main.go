// Command inject measures how well the dark-ship gap detector catches ships
// that switch off AIS: it takes real held-out ships, deletes their feed fixes
// after a random moment (synthetic switch-off), and watches the detector
// minute by minute. Real, uninjected feed gaps of the same ships are the
// false-alarm controls. The naive "silent >= N minutes" rule is the baseline.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"github.com/gulsherassad/darkwater/internal/ais"
	"github.com/gulsherassad/darkwater/internal/detect"
	"github.com/gulsherassad/darkwater/internal/grid"
	"github.com/gulsherassad/darkwater/internal/liveness"
	"github.com/gulsherassad/darkwater/internal/presence"
	"github.com/gulsherassad/darkwater/internal/reception"
	"github.com/gulsherassad/darkwater/internal/replay"
	"github.com/gulsherassad/darkwater/internal/tracker"
)

const priorStrength = 20.0 // same as the live server

var (
	alphas      = []float64{0.05, 0.01, 0.001, 0.0001}
	naive       = []int{15, 30, 60, 90, 120}
	withinMin   = []int{30, 60, 120}
	stratAlpha  = 2 // index of α = 0.001
	stratWithin = 60
)

// One synthetic switch-off and what the detector did about it.
type injection struct {
	mmsi, seed int64
	t          time.Time
	prob       float64 // reception: chance per minute the feed hears a ship in the cell at T
	known      bool    // the cell has enough data for the detector to judge it
	out        Outcome
}

func main() {
	seeds := flag.Int("seeds", 5, "switch-off times per ship (seeds 1..N)")
	outPath := flag.String("out", "data/ml/injection.csv", "per-injection results")
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/inject [-seeds 5] [-out path] <dma.csv> <recording.jsonl>...")
		os.Exit(2)
	}

	// 1) Feed, replayed like live: mark liveness on every position report, then
	// Validate, then store.Update. Also the reception-fit inputs, exactly as cmd/reception.
	live := liveness.New(48 * time.Hour)
	store := tracker.New()
	feed := map[int64][]ais.Position{} // accepted fixes per ship, oldest first
	heard := map[presence.Key]bool{}   // cmd/reception: valid, in Denmark
	fitAlive := map[int64]bool{}
	var firstMin, endMinute int64 = math.MaxInt64, 0
	for _, path := range flag.Args()[1:] {
		_, err := replay.ReadAll(path, func(p ais.Position, valid bool) {
			live.Mark(p.Time)
			m := p.Time.Unix() / 60
			firstMin, endMinute = min(firstMin, m), max(endMinute, m)
			if !valid {
				return
			}
			if _, ok := store.Update(p); ok {
				feed[p.MMSI] = append(feed[p.MMSI], p)
			}
			if grid.InDenmark(p) {
				heard[presence.Key{MMSI: p.MMSI, Minute: m}] = true
				fitAlive[m] = true
			}
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	alive := detect.FeedAlive(live.Alive)
	aliveMinutes := 0
	for m := firstMin; m <= endMinute; m++ {
		if alive(m) {
			aliveMinutes++
		}
	}

	// 2) DMA truth, from the start of the feed to 2 h past its end
	fixes, err := presence.LoadDMA(flag.Arg(0), firstMin-presence.MaxInterpMin, endMinute+stayAfterMin+presence.MaxInterpMin)
	if err != nil {
		log.Fatal(err)
	}

	// 3) Reception model fitted on the fit-set ships only (same rules as cmd/reception);
	// truth for the held-out ships
	counts := map[h3.Cell]reception.Counts{}
	truth := map[int64]map[int64]presence.Fix{} // held-out ship -> minute -> where it was
	presence.Walk(fixes, func(mmsi int64, f presence.Fix, _ bool) {
		if !presence.InFitSet(mmsi) {
			if truth[mmsi] == nil {
				truth[mmsi] = map[int64]presence.Fix{}
			}
			truth[mmsi][f.Minute] = f
			return
		}
		if !fitAlive[f.Minute] {
			return // feed wasn't recording: silence here says nothing
		}
		c := counts[grid.CellOf(ais.Position{Lat: f.Lat, Lon: f.Lon})]
		c.Present++
		if heard[presence.Key{MMSI: mmsi, Minute: f.Minute}] {
			c.Heard++
		}
		counts[grid.CellOf(ais.Position{Lat: f.Lat, Lon: f.Lon})] = c
	})
	model := reception.Fit(counts, priorStrength)

	ships := make([]int64, 0, len(truth))
	for mmsi := range truth {
		ships = append(ships, mmsi)
	}
	sort.Slice(ships, func(i, j int) bool { return ships[i] < ships[j] })

	// 4) Injections
	var injs []injection
	eligible := 0
	for _, mmsi := range ships {
		cands := candidates(truth[mmsi], feed[mmsi], alive, firstMin, endMinute)
		if len(cands) == 0 {
			continue
		}
		eligible++
		for seed := int64(1); seed <= int64(*seeds); seed++ {
			m := pick(cands, seed, mmsi)
			t := time.Unix(m*60, 0).UTC()
			last, _ := lastFixAtOrBefore(feed[mmsi], t) // candidates guarantee one exists
			f := truth[mmsi][m]
			cell := grid.CellOf(ais.Position{Lat: f.Lat, Lon: f.Lon})
			injs = append(injs, injection{
				mmsi: mmsi, seed: seed, t: t,
				prob: model.Prob(cell), known: model.Data(cell).Present >= detect.MinCellData,
				out: Watch(model, last, t, watchSteps, alive, endMinute, alphas, naive),
			})
		}
	}

	// 5) Controls: real gaps of the same held-out ships, DMA confirms present and moving
	ctlAlpha := make([]int, len(alphas))
	ctlNaive := make([]int, len(naive))
	ctlGaps, ctlShips, ctlMinutes := 0, 0, 0
	for _, mmsi := range ships {
		fs := feed[mmsi]
		used := false
		for i := 0; i+1 < len(fs); i++ {
			a, b := fs[i], fs[i+1]
			if !controlGap(truth[mmsi], a, b) {
				continue
			}
			o := Watch(model, a, a.Time.Add(time.Minute), controlSteps(a, b), alive, endMinute, alphas, naive)
			ctlGaps++
			ctlMinutes += o.Watched
			used = true
			for k, d := range o.Alpha {
				if d != notDetected {
					ctlAlpha[k]++
				}
			}
			for k, d := range o.Naive {
				if d != notDetected {
					ctlNaive[k]++
				}
			}
		}
		if used {
			ctlShips++
		}
	}
	ctlHours := float64(ctlMinutes) / 60
	perHundred := func(n int) float64 { return 100 * float64(n) / ctlHours }

	// 6) Report
	censored, outage, full := 0, 0, 0
	for _, in := range injs {
		switch {
		case in.out.Outage:
			outage++
		case in.out.Censored:
			censored++
		case in.out.Watched == watchSteps:
			full++
		}
	}
	fmt.Printf("feed: %s → %s UTC, %d alive minutes\n",
		time.Unix(firstMin*60, 0).UTC().Format("2006-01-02 15:04"), time.Unix(endMinute*60, 0).UTC().Format("15:04"), aliveMinutes)
	fmt.Printf("reception model: fit-set ships only, prior strength %.0f, overall hearing %.1f%%/min\n", priorStrength, 100*model.Global)
	fmt.Printf("held-out ships with DMA truth: %d, with a usable switch-off time: %d\n", len(ships), eligible)
	fmt.Printf("injections: %d (%d ships × %d seeds); watch ended at end of feed data: %d, at a feed outage: %d, watched full 6 h: %d\n",
		len(injs), eligible, *seeds, censored, outage, full)
	fmt.Printf("controls: %d real feed gaps on %d held-out ships, %.1f ship-hours watched\n\n", ctlGaps, ctlShips, ctlHours)

	type row struct {
		label  string
		alarms int
		surv   survival
		atT    int // already flagged at the switch-off time (silence before T)
	}
	var rows []row
	for k, a := range alphas {
		r := row{label: fmt.Sprintf("model α=%g", a), alarms: ctlAlpha[k]}
		for _, in := range injs {
			r.surv.add(in.out.Alpha[k], in.out.Watched)
			if in.out.Alpha[k] == 0 {
				r.atT++
			}
		}
		rows = append(rows, r)
	}
	for k, n := range naive {
		r := row{label: fmt.Sprintf("naive ≥ %d min", n), alarms: ctlNaive[k]}
		for _, in := range injs {
			r.surv.add(in.out.Naive[k], in.out.Watched)
			if in.out.Naive[k] == 0 {
				r.atT++
			}
		}
		rows = append(rows, r)
	}

	rate := func(s *survival, x int) string {
		r, n, ok := s.within(x)
		if !ok {
			return fmt.Sprintf("%13s", fmt.Sprintf("n/a (n=%d)", n))
		}
		return fmt.Sprintf("%13s", fmt.Sprintf("%.1f%% (%d)", 100*r, n))
	}
	median := func(s *survival) string {
		if m, ok := s.median(); ok {
			return fmt.Sprintf("%d min", m)
		}
		return "not reached"
	}

	fmt.Println("DETECTION (Kaplan–Meier, censored at end of feed data; (n) = injections that inform the rate:")
	fmt.Println("detected by then, or watched that long undetected) and FALSE ALARMS on controls")
	fmt.Printf("%-16s %8s %9s %13s %13s %13s %12s %10s\n", "rule", "alarms", "per 100h", "≤ 30 min", "≤ 1 h", "≤ 2 h", "median", "at T")
	for i := range rows {
		r := &rows[i]
		fmt.Printf("%-16s %8d %9.2f", r.label, r.alarms, perHundred(r.alarms))
		for _, x := range withinMin {
			fmt.Printf(" %s", rate(&r.surv, x))
		}
		fmt.Printf(" %12s %10d\n", median(&r.surv), r.atT)
	}
	fmt.Println(`"at T" = already flagged at the switch-off moment, from feed silence before it`)
	fmt.Printf("0 alarms in %.1f ship-hours ⇒ true rate < %.2f per 100 h (95%%, rule of three)\n", ctlHours, perHundred(3))

	// 7) Matched false alarms: for each α, the naive N with the closest alarm rate
	fmt.Printf("\nMATCHED FALSE ALARMS (naive N with the closest rate to each α; ties → shortest N)\n")
	fmt.Printf("%-14s %9s %13s %12s   %-14s %9s %13s %12s\n", "model", "per 100h", "≤ 1 h", "median", "naive", "per 100h", "≤ 1 h", "median")
	for k := range alphas {
		best := len(alphas)
		for j := len(alphas); j < len(rows); j++ {
			if math.Abs(float64(rows[j].alarms-rows[k].alarms)) < math.Abs(float64(rows[best].alarms-rows[k].alarms)) {
				best = j
			}
		}
		m, n := &rows[k], &rows[best]
		fmt.Printf("%-14s %9.2f %s %12s   %-14s %9.2f %s %12s\n",
			m.label, perHundred(m.alarms), rate(&m.surv, 60), median(&m.surv),
			n.label, perHundred(n.alarms), rate(&n.surv, 60), median(&n.surv))
	}

	// 8) By reception quality of the cell at T (terciles of hearing chance among judgeable cells)
	var probs []float64
	for _, in := range injs {
		if in.known {
			probs = append(probs, in.prob)
		}
	}
	sort.Float64s(probs)
	if len(probs) >= 3 {
		lo, hi := probs[len(probs)/3], probs[2*len(probs)/3]
		groups := []struct {
			label string
			keep  func(injection) bool
		}{
			{fmt.Sprintf("poor (< %.1f%%)", 100*lo), func(in injection) bool { return in.known && in.prob < lo }},
			{fmt.Sprintf("median (%.1f–%.1f%%)", 100*lo, 100*hi), func(in injection) bool { return in.known && in.prob >= lo && in.prob < hi }},
			{fmt.Sprintf("good (≥ %.1f%%)", 100*hi), func(in injection) bool { return in.known && in.prob >= hi }},
			{fmt.Sprintf("cell < %d min data", detect.MinCellData), func(in injection) bool { return !in.known }},
		}
		fmt.Printf("\nBY RECEPTION AT T (model α=%g, detected ≤ %d min; bands = terciles of cell hearing chance/min)\n", alphas[stratAlpha], stratWithin)
		fmt.Printf("%-24s %6s %13s %12s\n", "cell reception", "inj.", "≤ 1 h", "median")
		for _, g := range groups {
			var s survival
			n := 0
			for _, in := range injs {
				if g.keep(in) {
					s.add(in.out.Alpha[stratAlpha], in.out.Watched)
					n++
				}
			}
			fmt.Printf("%-24s %6d %s %12s\n", g.label, n, rate(&s, stratWithin), median(&s))
		}
	}

	// 9) Per-injection CSV for plotting
	if err := writeCSV(*outPath, injs); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nsaved %d injections → %s\n", len(injs), *outPath)
}

func writeCSV(path string, injs []injection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	head := []string{"mmsi", "seed", "t_utc", "cell_prob", "cell_known", "watched_min", "censored"}
	for _, a := range alphas {
		head = append(head, fmt.Sprintf("detect_min_alpha_%g", a))
	}
	for _, n := range naive {
		head = append(head, fmt.Sprintf("detect_min_naive_%d", n))
	}
	fmt.Fprintln(w, strings.Join(head, ","))
	cell := func(d int) string {
		if d == notDetected {
			return ""
		}
		return fmt.Sprint(d)
	}
	for _, in := range injs {
		row := []string{fmt.Sprint(in.mmsi), fmt.Sprint(in.seed), in.t.Format(time.RFC3339),
			fmt.Sprintf("%.4f", in.prob), fmt.Sprint(in.known), fmt.Sprint(in.out.Watched), fmt.Sprint(in.out.Censored)}
		for _, d := range in.out.Alpha {
			row = append(row, cell(d))
		}
		for _, d := range in.out.Naive {
			row = append(row, cell(d))
		}
		fmt.Fprintln(w, strings.Join(row, ","))
	}
	return w.Flush()
}
